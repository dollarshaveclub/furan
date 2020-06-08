package cmd

import (
	"fmt"
	"log"
	"time"

	"github.com/dollarshaveclub/furan/pkg/generated/furanrpc"
	"github.com/spf13/cobra"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
)

// triggerCmd represents the trigger command
var cancelCmd = &cobra.Command{
	Use:   "cancel",
	Short: "Cancel a running build",
	Long:  `Cancel a running build in a remote Furan cluster`,
	Run:   cancel,
}

var cancelReq = &furanrpc.BuildCancelRequest{}

func init() {
	cancelCmd.PersistentFlags().StringVar(&remoteFuranHost, "remote-host", "", "Remote Furan server with gRPC port (eg: furan.me.com:4001)")
	cancelCmd.PersistentFlags().BoolVar(&discoverFuranHost, "consul-discovery", false, "Discover Furan hosts via Consul")
	cancelCmd.PersistentFlags().StringVar(&consulFuranSvcName, "svc-name", "furan", "Consul service name for Furan hosts")
	cancelCmd.PersistentFlags().StringVar(&cancelReq.BuildId, "build-id", "", "Build ID")
	RootCmd.AddCommand(cancelCmd)
}

func cancel(cmd *cobra.Command, args []string) {
	if remoteFuranHost == "" {
		if !discoverFuranHost || consulFuranSvcName == "" {
			clierr("remote host or consul discovery is required")
		}
	}

	if cancelReq.BuildId == "" {
		clierr("build ID is required")
	}

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

	resp, err := c.CancelBuild(context.Background(), cancelReq)
	if err != nil {
		rpcerr(err, "CancelBuild", nil)
	}
	log.Printf("%v\n", *resp)
}
