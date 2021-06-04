/*
This package implements a Furan RPC client that can be directly imported by other Go programs.
It uses Consul service discovery to pick a random node.
*/

package rpcclient

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/DataDog/dd-trace-go.v1/ddtrace/tracer"

	"google.golang.org/grpc/codes"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
	grpctrace "gopkg.in/DataDog/dd-trace-go.v1/contrib/google.golang.org/grpc"
)

const (
	connTimeoutSecs = 30
)

// ErrCanceled is an error returned by Build() when the build has been canceled
var ErrCanceled = errors.New("build canceled")

//go:generate stringer -type=NodeSelectionStrategy

//NodeSelectionStrategy enumerates the ways that rpcclient will use to pick a node
type NodeSelectionStrategy int

const (
	RandomNodeSelection NodeSelectionStrategy = iota // Choose a random node
	NetworkProximity                                 // Pick the closest node as determined by Consul
)

// ImageBuildPusher describes an object capable of building and pushing container images
type ImageBuildPusher interface {
	Build(context.Context, chan *BuildEvent, *BuildRequest) (string, error)
}

// FuranClient is an object which issues remote RPC calls to a Furan server
type FuranClient struct {
	n      furanNode
	logger *log.Logger
	ddName string
}

// DiscoveryOptions describes the options for determining the Furan node to use
// for the client.
type DiscoveryOptions struct {
	UseConsul         bool                  // Has no effect (Consul discovery is no longer supported)
	ConsulAddr        string                // No effect
	SelectionStrategy NodeSelectionStrategy // No effect
	ServiceName       string                // No effect
	NodeList          []string              // Required. Nodes in the format "{host}:{port}". A random host will be used if len(NodeList) > 1
}

type furanNode struct {
	addr string
	port int
}

// NewFuranClient takes a Consul service name and returns a client which connects
// to a randomly chosen Furan host and uses the optional logger
func NewFuranClient(opts *DiscoveryOptions, logger *log.Logger, datadogServiceNamePrefix string) (*FuranClient, error) {
	fc := &FuranClient{}
	fc.ddName = datadogServiceNamePrefix + ".furan-client"
	if logger == nil {
		fc.logger = log.New(os.Stderr, "", log.LstdFlags)
	} else {
		fc.logger = logger
	}
	if opts.UseConsul {
		if opts.ServiceName == "" {
			return nil, fmt.Errorf("ConsulService is required if UseConsul is true")
		}
	} else {
		if len(opts.NodeList) == 0 {
			return nil, fmt.Errorf("non-empty NodeList is required if UseConsul is false")
		}
		opts.SelectionStrategy = RandomNodeSelection
	}
	err := fc.init(opts)
	return fc, err
}

func (fc *FuranClient) init(opts *DiscoveryOptions) error {
	nodes := []furanNode{}
	if opts.UseConsul {
		panic("consul discovery is not supported!")
	} else {
		for _, s := range opts.NodeList {
			ns := strings.Split(s, ":")
			if len(ns) != 2 {
				return fmt.Errorf("malformed node: %v", s)
			}
			p, err := strconv.Atoi(ns[1])
			if err != nil {
				return fmt.Errorf("malformed port: %v", s)
			}
			n := furanNode{
				addr: ns[0],
				port: p,
			}
			nodes = append(nodes, n)
		}
	}
	if opts.SelectionStrategy == RandomNodeSelection && len(nodes) > 1 {
		i, err := randomRange(len(nodes)) // Random node
		if err != nil {
			return err
		}
		fc.n = nodes[i]
	} else {
		fc.n = nodes[0]
	}
	fc.logger.Printf("using node %v", fc.n.addr)
	return nil
}

func (fc FuranClient) validateBuildRequest(req *BuildRequest) error {
	if req.Build.GithubRepo == "" {
		return fmt.Errorf("Build.GithubRepo is required")
	}
	if req.Build.Ref == "" {
		return fmt.Errorf("Build.Ref is required")
	}
	if len(req.Build.Tags) == 0 {
		return fmt.Errorf("at least one tag is required") // no tags causes datalayer failure
	}
	if req.Push.Registry.Repo == "" &&
		req.Push.S3.Region == "" &&
		req.Push.S3.Bucket == "" &&
		req.Push.S3.KeyPrefix == "" {
		return fmt.Errorf("you must specify either a Docker registry or S3 region/bucket/key-prefix as a push target")
	}
	return nil
}

func (fc FuranClient) rpcerr(err error, msg string, params ...interface{}) error {
	code := grpc.Code(err)
	msg = fmt.Sprintf(msg, params...)
	return fmt.Errorf("rpc error: %v: %v: %v", msg, code.String(), err)
}

// Build starts and monitors a build synchronously, sending BuildEvents to out and returning the build ID when completed, or error.
// Returns an error if there was an RPC error or if the build/push fails
// You must read from out (or provide a sufficiently buffered channel) to prevent Build from blocking forever
func (fc FuranClient) Build(ctx context.Context, out chan *BuildEvent, req *BuildRequest) (_ string, err error) {
	parentSpan, _ := tracer.StartSpanFromContext(ctx, "furan_client.build")
	defer func() {
		parentSpan.Finish(tracer.WithError(err))
	}()
	err = fc.validateBuildRequest(req)
	if err != nil {
		return "", err
	}

	remoteHost := fmt.Sprintf("%v:%v", fc.n.addr, fc.n.port)

	fc.logger.Printf("connecting to %v", remoteHost)
	i := grpctrace.UnaryClientInterceptor(grpctrace.WithServiceName(fc.ddName))

	conn, err := grpc.Dial(remoteHost, grpc.WithInsecure(), grpc.WithBlock(), grpc.WithTimeout(connTimeoutSecs*time.Second), grpc.WithUnaryInterceptor(i))
	if err != nil {
		return "", fmt.Errorf("error connecting to remote host: %v", err)
	}
	defer conn.Close()

	c := NewFuranExecutorClient(conn)

	if ctx.Err() == context.Canceled {
		return "", ErrCanceled
	}

	fc.logger.Printf("triggering build")
	// use a new context so StartBuild won't get cancelled if
	// ctx is cancelled
	buildContext := tracer.ContextWithSpan(context.Background(), parentSpan)
	resp, err := c.StartBuild(buildContext, req)
	if err != nil {
		return "", fc.rpcerr(err, "StartBuild")
	}

	mreq := BuildStatusRequest{
		BuildId: resp.BuildId,
	}

	fc.logger.Printf("monitoring build: %v", resp.BuildId)
	stream, err := c.MonitorBuild(ctx, &mreq)
	if err != nil {

		if grpc.Code(err) == codes.Canceled || err == context.Canceled {
			creq := BuildCancelRequest{
				BuildId: resp.BuildId,
			}

			c.CancelBuild(context.Background(), &creq) // best effort but doesn't matter if it fails
			return resp.BuildId, ErrCanceled
		}

		return resp.BuildId, fc.rpcerr(err, "MonitorBuild")
	}

	for {
		event, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				break
			}

			if grpc.Code(err) == codes.Canceled || err == context.Canceled {
				creq := BuildCancelRequest{
					BuildId: resp.BuildId,
				}

				c.CancelBuild(context.Background(), &creq) // best effort but doesn't matter if it fails
				return resp.BuildId, ErrCanceled
			}

			return resp.BuildId, fc.rpcerr(err, "stream.Recv")
		}
		out <- event
		if event.EventError.IsError {
			return resp.BuildId, fmt.Errorf("build error: %v", event.Message)
		}
	}

	return resp.BuildId, nil
}
