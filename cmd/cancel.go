package cmd

import (
	"github.com/spf13/cobra"

	"github.com/dollarshaveclub/furan/pkg/generated/furanrpc"
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
}
