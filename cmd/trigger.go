package cmd

import (
	"crypto/rand"
	"fmt"
	"io"
	"log"
	"math/big"
	"os"
	"time"

	"github.com/dollarshaveclub/furan/pkg/generated/furanrpc"
	"github.com/dollarshaveclub/furan/pkg/metrics"

	consul "github.com/hashicorp/consul/api"
	"github.com/spf13/cobra"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
)

const (
	pollStatusIntervalSecs = 5
	connTimeoutSecs        = 30
)

var discoverFuranHost bool
var consulFuranSvcName string
var remoteFuranHost string
var monitorBuild bool
var buildArgs []string

// triggerCmd represents the trigger command
var triggerCmd = &cobra.Command{
	Use:   "trigger",
	Short: "Start a build on a remote Furan server",
	Long:  `Trigger and then monitor a build on a remote Furan server`,
	Run:   trigger,
}

func init() {
	triggerCmd.PersistentFlags().StringVar(&remoteFuranHost, "remote-host", "", "Remote Furan server with gRPC port (eg: furan.me.com:4001)")
	triggerCmd.PersistentFlags().BoolVar(&discoverFuranHost, "consul-discovery", false, "Discover Furan hosts via Consul")
	triggerCmd.PersistentFlags().StringVar(&consulFuranSvcName, "svc-name", "furan", "Consul service name for Furan hosts")
	triggerCmd.PersistentFlags().StringVar(&cliBuildRequest.Build.GithubRepo, "github-repo", "", "source github repo")
	triggerCmd.PersistentFlags().StringVar(&cliBuildRequest.Build.Ref, "source-ref", "master", "source git ref")
	triggerCmd.PersistentFlags().StringVar(&cliBuildRequest.Build.DockerfilePath, "dockerfile-path", "Dockerfile", "Dockerfile path (optional)")
	triggerCmd.PersistentFlags().StringVar(&cliBuildRequest.Push.Registry.Repo, "image-repo", "", "push to image repo")
	triggerCmd.PersistentFlags().StringVar(&cliBuildRequest.Push.S3.Region, "s3-region", "", "S3 region")
	triggerCmd.PersistentFlags().StringVar(&cliBuildRequest.Push.S3.Bucket, "s3-bucket", "", "S3 bucket")
	triggerCmd.PersistentFlags().StringVar(&cliBuildRequest.Push.S3.KeyPrefix, "s3-key-prefix", "", "S3 key prefix")
	triggerCmd.PersistentFlags().StringVar(&tags, "tags", "master", "image tags (optional, comma-delimited)")
	triggerCmd.PersistentFlags().BoolVar(&cliBuildRequest.Build.TagWithCommitSha, "tag-sha", false, "additionally tag with git commit SHA (optional)")
	triggerCmd.PersistentFlags().BoolVar(&cliBuildRequest.SkipIfExists, "skip-if-exists", false, "if build already exists at destination, skip build/push (registry: all tags exist, s3: object exists)")
	triggerCmd.PersistentFlags().StringSliceVar(&buildArgs, "build-arg", []string{}, "Build arg to use for build request")
	triggerCmd.PersistentFlags().BoolVar(&monitorBuild, "monitor", true, "Monitor build after triggering")
	RootCmd.AddCommand(triggerCmd)
}

func rpcerr(err error, msg string, mc metrics.MetricsCollector) {
	code := grpc.Code(err)
	err = fmt.Errorf("rpc error: %v: %v: %v", msg, code.String(), err)
	triggerFailed(err, mc)
}

type furanNode struct {
	addr string
	port int
}

func randomRange(max int) (int64, error) {
	maxBig := *big.NewInt(int64(max))
	n, err := rand.Int(rand.Reader, &maxBig)
	if err != nil {
		return 0, err
	}
	return n.Int64(), nil
}

func getFuranServerFromConsul(svc string) (*furanNode, error) {
	nodes := []furanNode{}
	c, err := consul.NewClient(consul.DefaultConfig())
	if err != nil {
		return nil, err
	}
	se, _, err := c.Health().Service(svc, "", true, &consul.QueryOptions{})
	if err != nil {
		return nil, err
	}
	if len(se) == 0 {
		return nil, fmt.Errorf("no furan hosts found via Consul")
	}
	for _, s := range se {
		n := furanNode{
			addr: s.Node.Address,
			port: s.Service.Port,
		}
		nodes = append(nodes, n)
	}
	i, err := randomRange(len(nodes)) // Random node
	if err != nil {
		return nil, err
	}
	return &nodes[i], nil
}

func triggerFailed(err error, mc metrics.MetricsCollector) {
	if mc != nil {
		mc.TriggerCompleted(cliBuildRequest.Build.GithubRepo, cliBuildRequest.Build.Ref, true)
	}
	fmt.Fprintf(os.Stderr, "trigger failed: %v", err)
	os.Exit(1)
}

func triggerSucceeded(mc metrics.MetricsCollector) {
	if mc != nil {
		mc.TriggerCompleted(cliBuildRequest.Build.GithubRepo, cliBuildRequest.Build.Ref, false)
	}
	os.Exit(0)
}

func trigger(cmd *cobra.Command, args []string) {
	if remoteFuranHost == "" {
		if !discoverFuranHost || consulFuranSvcName == "" {
			clierr("remote host or consul discovery is required")
		}
	}
	validateCLIBuildRequest()

	var remoteHost string
	if discoverFuranHost {
		n, err := getFuranServerFromConsul(consulFuranSvcName)
		if err != nil {
			clierr("error discovering Furan hosts: %v", err)
		}
		remoteHost = fmt.Sprintf("%v:%v", n.addr, n.port)
	} else {
		remoteHost = remoteFuranHost
	}

	log.Printf("connecting to %v", remoteHost)
	conn, err := grpc.Dial(remoteHost, grpc.WithInsecure(), grpc.WithBlock(), grpc.WithTimeout(connTimeoutSecs*time.Second))
	if err != nil {
		clierr("error connecting to remote host: %v", err)
	}
	defer conn.Close()

	c := furanrpc.NewFuranExecutorClient(conn)
	mc, err := newDatadogCollector()
	if err != nil {
		log.Printf("unabled to create datadog collector: %v, continuing...", err)
	}

	log.Printf("triggering build")
	resp, err := c.StartBuild(context.Background(), &cliBuildRequest)
	if err != nil {
		rpcerr(err, "StartBuild", mc)
	}

	if !monitorBuild {
		log.Printf("build ID: %v\n", resp.BuildId)
		return
	}

	mreq := furanrpc.BuildStatusRequest{
		BuildId: resp.BuildId,
	}

	log.Printf("monitoring build: %v", resp.BuildId)
	stream, err := c.MonitorBuild(context.Background(), &mreq)
	if err != nil {
		rpcerr(err, "MonitorBuild", mc)
	}

	// In the event of a Kafka failure, instead of hanging indefinitely we concurrently
	// poll for build status so we know when a build finishes/fails
	ticker := time.NewTicker(pollStatusIntervalSecs * time.Second)
	go func() {
		sreq := furanrpc.BuildStatusRequest{
			BuildId: resp.BuildId,
		}
		for {
			select {
			case <-ticker.C:
				sresp, err := c.GetBuildStatus(context.Background(), &sreq)
				if err != nil {
					rpcerr(err, "GetBuildStatus", mc)
				}
				log.Printf("build status: %v", sresp.State.String())
				if sresp.Finished {
					if sresp.Failed {
						triggerFailed(fmt.Errorf("build failed: %v", err), mc)
					}
					triggerSucceeded(mc)
				}
			}
		}
	}()

	for {
		event, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				break
			}
			rpcerr(err, "stream.Recv", mc)
		}
		if event.EventError.IsError {
			triggerFailed(fmt.Errorf("build failed: %v", event.Message), mc)
		}
	}
}
