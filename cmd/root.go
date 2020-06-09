package cmd

import (
	"fmt"
	"log"
	"os"

	"github.com/dollarshaveclub/furan/pkg/config"
	"github.com/dollarshaveclub/furan/pkg/generated/furanrpc"
	"github.com/spf13/cobra"
)

var vaultConfig config.Vaultconfig
var gitConfig config.Gitconfig
var dockerConfig config.Dockerconfig
var awsConfig config.AWSConfig
var dbConfig config.DBconfig
var consulConfig config.Consulconfig

var nodestr string
var datacenterstr string
var initializeDB bool
var kafkaBrokerStr string
var awscredsprefix string
var dogstatsdAddr string
var defaultMetricsTags string
var datadogServiceName string
var datadogTracingAgentAddr string

var logger *log.Logger

// used by build and trigger commands
var cliBuildRequest = furanrpc.BuildRequest{
	Build: &furanrpc.BuildDefinition{},
	Push: &furanrpc.PushDefinition{
		Registry: &furanrpc.PushRegistryDefinition{},
		S3:       &furanrpc.PushS3Definition{},
	},
}
var tags string

// RootCmd represents the base command when called without any subcommands
var RootCmd = &cobra.Command{
	Use:   "furan",
	Short: "Docker image builder",
	Long:  `API application to build Docker images on command`,
}

// Execute is the entry point for the app
func Execute() {
	if err := RootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(-1)
	}
}

// shorthands in use: ['a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j', 'k', 'l', 'm', 'n', 'o', 'p', 'q', 's', 't', 'u', 'v', 'w', 'x', 'y', 'z']
func init() {
	RootCmd.PersistentFlags().StringVarP(&vaultConfig.Addr, "vault-addr", "a", os.Getenv("VAULT_ADDR"), "Vault URL")
	RootCmd.PersistentFlags().StringVarP(&vaultConfig.Token, "vault-token", "t", os.Getenv("VAULT_TOKEN"), "Vault token (if using token auth)")
	RootCmd.PersistentFlags().StringVar(&vaultConfig.K8sJWTPath, "vault-k8s-jwt-path", os.Getenv("VAULT_K8s_JWT_PATH"), "Path to file containing K8s service account JWT, for k8s Vault authentication")
	RootCmd.PersistentFlags().StringVar(&vaultConfig.K8sRole, "vault-k8s-role", os.Getenv("VAULT_K8s_ROLE"), "Vault role name for use in k8s Vault authentication")
	RootCmd.PersistentFlags().StringVar(&vaultConfig.K8sAuthPath, "vault-k8s-auth-path", "kubernetes", "Auth path for use in k8s Vault authentication")
	RootCmd.PersistentFlags().BoolVarP(&vaultConfig.TokenAuth, "vault-token-auth", "k", false, "Use Vault token-based auth")
	RootCmd.PersistentFlags().StringVarP(&vaultConfig.AppID, "vault-app-id", "p", os.Getenv("APP_ID"), "Vault App-ID")
	RootCmd.PersistentFlags().StringVarP(&vaultConfig.UserIDPath, "vault-user-id-path", "u", os.Getenv("USER_ID_PATH"), "Path to file containing Vault User-ID")
	RootCmd.PersistentFlags().BoolVarP(&dbConfig.UseConsul, "consul-db-svc", "z", false, "Discover Cassandra nodes through Consul")
	RootCmd.PersistentFlags().StringVarP(&dbConfig.ConsulServiceName, "svc-name", "v", "cassandra", "Consul service name for Cassandra")
	RootCmd.PersistentFlags().StringVarP(&nodestr, "db-nodes", "n", "", "Comma-delimited list of Cassandra nodes (if not using Consul discovery)")
	RootCmd.PersistentFlags().StringVarP(&datacenterstr, "db-dc", "d", "us-west-2", "Comma-delimited list of Cassandra datacenters (if not using Consul discovery)")
	RootCmd.PersistentFlags().BoolVarP(&initializeDB, "db-init", "i", false, "Initialize DB UDTs and tables if missing (only necessary on first run)")
	RootCmd.PersistentFlags().StringVarP(&dbConfig.Keyspace, "db-keyspace", "b", "furan", "Cassandra keyspace")
	RootCmd.PersistentFlags().StringVarP(&vaultConfig.VaultPathPrefix, "vault-prefix", "x", "secret/production/furan", "Vault path prefix for secrets")
	RootCmd.PersistentFlags().StringVarP(&gitConfig.TokenVaultPath, "github-token-path", "g", "/github/token", "Vault path (appended to prefix) for GitHub token")
	RootCmd.PersistentFlags().StringVarP(&dockerConfig.DockercfgVaultPath, "vault-dockercfg-path", "e", "/dockercfg", "Vault path to .dockercfg contents")
	RootCmd.PersistentFlags().StringVarP(&awscredsprefix, "aws-creds-vault-prefix", "c", "/aws", "Vault path prefix for AWS credentials (paths: {vault prefix}/{aws creds prefix}/access_key_id|secret_access_key)")
	RootCmd.PersistentFlags().UintVarP(&awsConfig.Concurrency, "s3-concurrency", "o", 10, "Number of concurrent upload/download threads for S3 transfers")
	RootCmd.PersistentFlags().StringVarP(&dogstatsdAddr, "dogstatsd-addr", "q", "127.0.0.1:8125", "Address of dogstatsd for metrics")
	RootCmd.PersistentFlags().StringVarP(&defaultMetricsTags, "default-metrics-tags", "s", "env:qa", "Comma-delimited list of tag keys and values in the form key:value")
	RootCmd.PersistentFlags().StringVarP(&datadogServiceName, "datadog-service-name", "w", "furan", "Datadog APM service name")
	RootCmd.PersistentFlags().StringVarP(&datadogTracingAgentAddr, "datadog-tracing-agent-addr", "y", "127.0.0.1:8126", "Address of datadog tracing agent")
}

func clierr(msg string, params ...interface{}) {
	fmt.Fprintf(os.Stderr, msg+"\n", params...)
	os.Exit(1)
}
