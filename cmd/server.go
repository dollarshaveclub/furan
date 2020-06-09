// +build linux darwin freebsd netbsd openbsd

package cmd

import (
	// Import pprof handlers into http.DefaultServeMux
	_ "net/http/pprof"

	"github.com/spf13/cobra"

	"github.com/dollarshaveclub/furan/pkg/config"
)

var serverConfig config.Serverconfig

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
	serverCmd.PersistentFlags().UintVar(&serverConfig.HealthcheckHTTPport, "healthcheck-port", 4002, "Healthcheck HTTP port (listens on localhost only)")
	serverCmd.PersistentFlags().UintVar(&serverConfig.PPROFPort, "pprof-port", 4003, "Port for serving pprof profiles")
	serverCmd.PersistentFlags().StringVar(&serverConfig.HTTPSAddr, "https-addr", "0.0.0.0", "REST HTTPS listen address")
	serverCmd.PersistentFlags().StringVar(&serverConfig.GRPCAddr, "grpc-addr", "0.0.0.0", "gRPC listen address")
	serverCmd.PersistentFlags().UintVar(&serverConfig.Concurrency, "concurrency", 10, "Max concurrent builds")
	serverCmd.PersistentFlags().UintVar(&serverConfig.Queuesize, "queue", 100, "Max queue size for buffered build requests")
	serverCmd.PersistentFlags().StringVar(&serverConfig.VaultTLSCertPath, "tls-cert-path", "/tls/cert", "Vault path to TLS certificate")
	serverCmd.PersistentFlags().StringVar(&serverConfig.VaultTLSKeyPath, "tls-key-path", "/tls/key", "Vault path to TLS private key")
	serverCmd.PersistentFlags().BoolVar(&serverConfig.LogToSumo, "log-to-sumo", true, "Send log entries to SumoLogic HTTPS collector")
	serverCmd.PersistentFlags().StringVar(&serverConfig.VaultSumoURLPath, "sumo-collector-path", "/sumologic/url", "Vault path SumoLogic collector URL")
	serverCmd.PersistentFlags().BoolVar(&serverConfig.S3ErrorLogs, "s3-error-logs", false, "Upload failed build logs to S3 (region and bucket must be specified)")
	serverCmd.PersistentFlags().StringVar(&serverConfig.S3ErrorLogRegion, "s3-error-log-region", "us-west-2", "Region for S3 error log upload")
	serverCmd.PersistentFlags().StringVar(&serverConfig.S3ErrorLogBucket, "s3-error-log-bucket", "", "Bucket for S3 error log upload")
	serverCmd.PersistentFlags().UintVar(&serverConfig.S3PresignTTL, "s3-error-log-presign-ttl", 60*24, "Presigned error log URL TTL in minutes (0 to disable)")
	serverCmd.PersistentFlags().UintVar(&serverConfig.GCIntervalSecs, "gc-interval", 3600, "GC (garbage collection) interval in seconds")
	serverCmd.PersistentFlags().StringVar(&serverConfig.DockerDiskPath, "docker-storage-path", "/var/pkg/docker", "Path to Docker storage for monitoring free space (optional)")
	serverCmd.PersistentFlags().StringVar(&consulConfig.Addr, "consul-addr", "127.0.0.1:8500", "Consul address (IP:port)")
	serverCmd.PersistentFlags().StringVar(&consulConfig.KVPrefix, "consul-kv-prefix", "furan", "Consul KV prefix")
	serverCmd.PersistentFlags().BoolVar(&serverConfig.DisableMetrics, "disable-metrics", false, "Disable Datadog metrics collection")
	serverCmd.PersistentFlags().BoolVar(&awsConfig.EnableECR, "ecr", false, "Enable AWS ECR support")
	serverCmd.PersistentFlags().StringSliceVar(&awsConfig.ECRRegistryHosts, "ecr-registry-hosts", []string{}, "ECR registry hosts (ex: 123456789.dkr.ecr.us-west-2.amazonaws.com) to authorize for base images")
	RootCmd.AddCommand(serverCmd)
}

func server(cmd *cobra.Command, args []string) {
}
