// +build linux darwin freebsd netbsd openbsd

package cmd

import (
	// Import pprof handlers into http.DefaultServeMux
	_ "net/http/pprof"

	"github.com/spf13/cobra"

	"github.com/dollarshaveclub/furan/pkg/config"
)

var serverConfig config.ServerConfig

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "Run Furan server",
	Long:  `Furan API server (see docs)`,
	PreRun: func(cmd *cobra.Command, args []string) {
	},
	Run: server,
}

func init() {
	serverCmd.PersistentFlags().UintVar(&serverConfig.HTTPSPort, "https-port", 4000, "REST HTTPS TCP port")
	serverCmd.PersistentFlags().UintVar(&serverConfig.GRPCPort, "grpc-port", 4001, "gRPC TCP port")
	serverCmd.PersistentFlags().StringVar(&serverConfig.HealthcheckAddr, "healthcheck-addr", "0.0.0.0", "HTTP healthcheck listen address")
	serverCmd.PersistentFlags().UintVar(&serverConfig.PPROFPort, "pprof-port", 4003, "Port for serving pprof profiles")
	serverCmd.PersistentFlags().StringVar(&serverConfig.HTTPSAddr, "https-addr", "0.0.0.0", "REST HTTPS listen address")
	serverCmd.PersistentFlags().StringVar(&serverConfig.GRPCAddr, "grpc-addr", "0.0.0.0", "gRPC listen address")
	serverCmd.PersistentFlags().BoolVar(&awsConfig.EnableECR, "ecr", false, "Enable AWS ECR support")
	serverCmd.PersistentFlags().StringSliceVar(&awsConfig.ECRRegistryHosts, "ecr-registry-hosts", []string{}, "ECR registry hosts (ex: 123456789.dkr.ecr.us-west-2.amazonaws.com) to authorize for base images")
	RootCmd.AddCommand(serverCmd)
}

func server(cmd *cobra.Command, args []string) {
}
