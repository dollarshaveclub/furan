package cmd

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/dollarshaveclub/furan/generated/lib"
	"github.com/dollarshaveclub/furan/lib/config"
	"github.com/dollarshaveclub/furan/lib/datalayer"
	"github.com/dollarshaveclub/furan/lib/db"
	"github.com/dollarshaveclub/furan/lib/kafka"
	"github.com/dollarshaveclub/furan/lib/metrics"
	"github.com/dollarshaveclub/go-lib/cassandra"
	"github.com/gocql/gocql"
	consul "github.com/hashicorp/consul/api"
	"github.com/spf13/cobra"
	"gopkg.in/DataDog/dd-trace-go.v1/ddtrace/tracer"
)

var vaultConfig config.Vaultconfig
var gitConfig config.Gitconfig
var dockerConfig config.Dockerconfig
var awsConfig config.AWSConfig
var dbConfig config.DBconfig
var kafkaConfig config.Kafkaconfig
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
var cliBuildRequest = lib.BuildRequest{
	Build: &lib.BuildDefinition{},
	Push: &lib.PushDefinition{
		Registry: &lib.PushRegistryDefinition{},
		S3:       &lib.PushS3Definition{},
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

// shorthands in use: ['a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j', 'k', 'l', 'm', 'n', 'o', 'p', 'q', 's', t', 'u', 'v', 'w', 'x', 'z']
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
	RootCmd.PersistentFlags().StringVarP(&kafkaBrokerStr, "kafka-brokers", "f", "localhost:9092", "Comma-delimited list of Kafka brokers")
	RootCmd.PersistentFlags().StringVarP(&kafkaConfig.Topic, "kafka-topic", "m", "furan-events", "Kafka topic to publish build events (required for build monitoring)")
	RootCmd.PersistentFlags().UintVarP(&kafkaConfig.MaxOpenSends, "kafka-max-open-sends", "j", 1000, "Max number of simultaneous in-flight Kafka message sends")
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

func getDockercfg() error {
	dockerConfig.Setup()
	err := json.Unmarshal([]byte(dockerConfig.DockercfgRaw), &dockerConfig.DockercfgContents)
	if err != nil {
		return err
	}
	for k, v := range dockerConfig.DockercfgContents {
		if v.Auth != "" && v.Username == "" && v.Password == "" {
			// Auth is a base64-encoded string of the form USERNAME:PASSWORD
			ab, err := base64.StdEncoding.DecodeString(v.Auth)
			if err != nil {
				return fmt.Errorf("dockercfg: couldn't decode auth string: %v: %v", k, err)
			}
			as := strings.Split(string(ab), ":")
			if len(as) != 2 {
				return fmt.Errorf("dockercfg: malformed auth string: %v: %v: %v", k, v.Auth, string(ab))
			}
			v.Username = as[0]
			v.Password = as[1]
			v.Auth = ""
		}
		v.ServerAddress = k
		dockerConfig.DockercfgContents[k] = v
	}
	return nil
}

// GetNodesFromConsul queries the local Consul agent for the given service,
// returning the healthy nodes in ascending order of network distance/latency
func getNodesFromConsul(svc string) ([]string, error) {
	nodes := []string{}
	c, err := consul.NewClient(consul.DefaultConfig())
	if err != nil {
		return nodes, err
	}
	h := c.Health()
	opts := &consul.QueryOptions{
		Near: "_agent",
	}
	se, _, err := h.Service(svc, "", true, opts)
	if err != nil {
		return nodes, err
	}
	for _, s := range se {
		nodes = append(nodes, s.Node.Address)
	}
	return nodes, nil
}

func connectToDB() {
	if dbConfig.UseConsul {
		nodes, err := getNodesFromConsul(dbConfig.ConsulServiceName)
		if err != nil {
			log.Fatalf("error getting DB nodes: %v", err)
		}
		dbConfig.Nodes = nodes
	}
	dbConfig.Cluster = gocql.NewCluster(dbConfig.Nodes...)
	dbConfig.Cluster.Keyspace = dbConfig.Keyspace
	dbConfig.Cluster.ProtoVersion = 3
	dbConfig.Cluster.NumConns = 20
	dbConfig.Cluster.Timeout = 10 * time.Second
	dbConfig.Cluster.SocketKeepalive = 30 * time.Second
}

func setupDataLayer() {
	s, err := dbConfig.Cluster.CreateSession()
	if err != nil {
		log.Fatalf("error creating DB session: %v", err)
	}
	dbConfig.Datalayer = datalayer.NewDBLayer(s)
}

func initDB() {
	err := cassandra.CreateRequiredTypes(dbConfig.Cluster, db.RequiredUDTs)
	if err != nil {
		log.Fatalf("error creating UDTs: %v", err)
	}
	err = cassandra.CreateRequiredTables(dbConfig.Cluster, db.RequiredTables)
	if err != nil {
		log.Fatalf("error creating tables: %v", err)
	}
}

func setupDB(initdb bool) {
	dbConfig.Nodes = strings.Split(nodestr, ",")
	if !dbConfig.UseConsul {
		if len(dbConfig.Nodes) == 0 || dbConfig.Nodes[0] == "" {
			log.Fatalf("cannot setup DB: Consul is disabled and node list is empty")
		}
	}
	dbConfig.DataCenters = strings.Split(datacenterstr, ",")
	connectToDB()
	if initdb {
		initDB()
	}
	dbConfig.Cluster.Keyspace = dbConfig.Keyspace
	setupDataLayer()
}

func setupKafka(mc metrics.MetricsCollector) {
	kafkaConfig.Brokers = strings.Split(kafkaBrokerStr, ",")
	if len(kafkaConfig.Brokers) < 1 {
		log.Fatalf("At least one Kafka broker is required")
	}
	if kafkaConfig.Topic == "" {
		log.Fatalf("Kafka topic is required")
	}
	kp, err := kafka.NewKafkaManager(kafkaConfig.Brokers, kafkaConfig.Topic, kafkaConfig.MaxOpenSends, mc, logger)
	if err != nil {
		log.Fatalf("Error creating Kafka producer: %v", err)
	}
	kafkaConfig.Manager = kp
}

func newDatadogCollector() (*metrics.DatadogCollector, error) {
	defaultDogstatsdTags := strings.Split(defaultMetricsTags, ",")
	return metrics.NewDatadogCollector(dogstatsdAddr, defaultDogstatsdTags)
}

func startDatadogTracer() {
	opts := []tracer.StartOption{tracer.WithAgentAddr(datadogTracingAgentAddr)}
	opts = append(opts, tracer.WithServiceName(datadogServiceName))
	opts = append(opts, tracer.WithDebugMode(true))
	for _, tag := range strings.Split(defaultMetricsTags, ",") {
		keyValPair := strings.Split(tag, ":")
		if len(keyValPair) != 2 {
			log.Fatalf("invalid default metrics tags: %v", defaultMetricsTags)
		}
		key, val := keyValPair[0], keyValPair[1]
		opts = append(opts, tracer.WithGlobalTag(key, val))
	}
	tracer.Start(opts...)
}
