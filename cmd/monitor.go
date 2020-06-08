package cmd

import (
	"context"
	"fmt"
	"io"
	"log"
	"time"

	"github.com/dollarshaveclub/furan/pkg/generated/furanrpc"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
)

// monitorCmd represents the monitor command
var monitorCmd = &cobra.Command{
	Use:   "monitor",
	Short: "Monitor a running build",
	Long:  `Monitor an already triggered, running build.`,
	Run:   monitor,
}

var monitorRequest furanrpc.BuildStatusRequest

func init() {
	monitorCmd.PersistentFlags().StringVar(&remoteFuranHost, "remote-host", "", "Remote Furan server with gRPC port (eg: furan.me.com:4001)")
	monitorCmd.PersistentFlags().BoolVar(&discoverFuranHost, "consul-discovery", false, "Discover Furan hosts via Consul")
	monitorCmd.PersistentFlags().StringVar(&consulFuranSvcName, "svc-name", "furan", "Consul service name for Furan hosts")
	monitorCmd.PersistentFlags().StringVar(&monitorRequest.BuildId, "build-id", "", "Build ID")
	RootCmd.AddCommand(monitorCmd)
}

func monitor(cmd *cobra.Command, args []string) {
	if remoteFuranHost == "" {
		if !discoverFuranHost || consulFuranSvcName == "" {
			clierr("remote host or consul discovery is required")
		}
	}

	if monitorRequest.BuildId == "" {
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
	stream, err := c.MonitorBuild(context.Background(), &monitorRequest)
	if err != nil {
		clierr("error monitoring build: %v", err)
	}
	for {
		event, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				break
			}
			clierr("error receiving from stream: %v", err)
		}
		if event.EventError.IsError {
			clierr("build error: %v: %v", event.EventError.ErrorType.String(), event.Message)
		}
		fmt.Println(event.Message)
	}
}
