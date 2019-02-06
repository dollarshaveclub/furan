// +build linux darwin freebsd netbsd openbsd

package cmd

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	// Import pprof handlers into http.DefaultServeMux
	_ "net/http/pprof"

	docker "github.com/docker/engine-api/client"
	"github.com/dollarshaveclub/furan/lib/builder"
	"github.com/dollarshaveclub/furan/lib/config"
	"github.com/dollarshaveclub/furan/lib/consul"
	"github.com/dollarshaveclub/furan/lib/gc"
	githubfetch "github.com/dollarshaveclub/furan/lib/github_fetch"
	"github.com/dollarshaveclub/furan/lib/grpc"
	"github.com/dollarshaveclub/furan/lib/httphandlers"
	flogger "github.com/dollarshaveclub/furan/lib/logger"
	"github.com/dollarshaveclub/furan/lib/metrics"
	"github.com/dollarshaveclub/furan/lib/s3"
	"github.com/dollarshaveclub/furan/lib/squasher"
	"github.com/dollarshaveclub/furan/lib/tagcheck"
	"github.com/dollarshaveclub/furan/lib/vault"
	"github.com/gorilla/mux"
	"github.com/spf13/cobra"
	"gopkg.in/DataDog/dd-trace-go.v1/ddtrace/tracer"
)

var serverConfig config.Serverconfig

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "Run Furan server",
	Long:  `Furan API server (see docs)`,
	PreRun: func(cmd *cobra.Command, args []string) {
		if serverConfig.S3ErrorLogs {
			if serverConfig.S3ErrorLogBucket == "" {
				clierr("S3 error log bucket must be defined")
			}
			if serverConfig.S3ErrorLogRegion == "" {
				clierr("S3 error log region must be defined")
			}
		}
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
	serverCmd.PersistentFlags().StringVar(&serverConfig.DockerDiskPath, "docker-storage-path", "/var/lib/docker", "Path to Docker storage for monitoring free space (optional)")
	serverCmd.PersistentFlags().StringVar(&consulConfig.Addr, "consul-addr", "127.0.0.1:8500", "Consul address (IP:port)")
	serverCmd.PersistentFlags().StringVar(&consulConfig.KVPrefix, "consul-kv-prefix", "furan", "Consul KV prefix")
	RootCmd.AddCommand(serverCmd)
}

func setupServerLogger() {
	var url string
	if serverConfig.LogToSumo {
		url = serverConfig.SumoURL
	}
	hn, err := os.Hostname()
	if err != nil {
		log.Fatalf("error getting hostname: %v", err)
	}
	stdlog := flogger.NewStandardLogger(os.Stderr, url)
	logger = log.New(stdlog, fmt.Sprintf("%v: ", hn), log.LstdFlags)
}

// Separate server because it's HTTP on localhost only
// (simplifies Consul health check)
func healthcheck(ha *httphandlers.HTTPAdapter) {
	r := mux.NewRouter()
	r.HandleFunc("/health", ha.HealthHandler).Methods("GET")
	addr := fmt.Sprintf("%v:%v", serverConfig.HealthcheckAddr, serverConfig.HealthcheckHTTPport)
	server := &http.Server{Addr: addr, Handler: r}
	logger.Printf("HTTP healthcheck listening on: %v", addr)
	logger.Println(server.ListenAndServe())
}

func pprof() {
	// pprof installs handlers into http.DefaultServeMux
	logger.Println(http.ListenAndServe(fmt.Sprintf("127.0.0.1:%d", serverConfig.PPROFPort), nil))
	logger.Printf("pprof listening on port: %v", serverConfig.PPROFPort)
}

func startGC(dc builder.ImageBuildClient, mc metrics.MetricsCollector, log *log.Logger, interval uint) {
	igc := gc.NewDockerImageGC(log, dc, mc, serverConfig.DockerDiskPath)
	ticker := time.NewTicker(time.Duration(interval) * time.Second)
	go func() {
		for {
			select {
			case <-ticker.C:
				igc.GC()
			}
		}
	}()
}

func server(cmd *cobra.Command, args []string) {
	var err error

	vault.SetupVault(&vaultConfig, &awsConfig, &dockerConfig, &gitConfig, &serverConfig, awscredsprefix)
	if serverConfig.LogToSumo {
		vault.GetSumoURL(&vaultConfig, &serverConfig)
	}

	setupServerLogger()
	setupDB(initializeDB)
	mc, err := newDatadogCollector()
	if err != nil {
		log.Fatalf("error creating Datadog collector: %v", err)
	}
	startDatadogTracer()
	setupKafka(mc)
	certPath, keyPath := vault.WriteTLSCert(&vaultConfig, &serverConfig)
	defer vault.RmTempFiles(certPath, keyPath)
	err = getDockercfg()
	if err != nil {
		logger.Fatalf("error reading dockercfg: %v", err)
	}

	dc, err := docker.NewEnvClient()
	if err != nil {
		log.Fatalf("error creating Docker client: %v", err)
	}

	gf := githubfetch.NewGitHubFetcher(gitConfig.Token)
	osm := s3.NewS3StorageManager(awsConfig, mc, logger)
	is := squasher.NewDockerImageSquasher(logger)
	itc := tagcheck.NewRegistryTagChecker(&dockerConfig, logger.Printf)
	s3errcfg := builder.S3ErrorLogConfig{
		PushToS3:          serverConfig.S3ErrorLogs,
		Region:            serverConfig.S3ErrorLogRegion,
		Bucket:            serverConfig.S3ErrorLogBucket,
		PresignTTLMinutes: serverConfig.S3PresignTTL,
	}
	imageBuilder, err := builder.NewImageBuilder(kafkaConfig.Manager, dbConfig.Datalayer, gf, dc, mc, osm, is, itc, dockerConfig.DockercfgContents, s3errcfg, logger)
	if err != nil {
		log.Fatalf("error creating image builder: %v", err)
	}

	kvo, err := consul.NewConsulKVOrchestrator(&consulConfig)
	if err != nil {
		log.Fatalf("error creating key value orchestrator: %v", err)
	}
	grpcServiceName := strings.Join([]string{datadogServiceName, "grpc"}, ".")
	grpcSvr := grpc.NewGRPCServer(imageBuilder, dbConfig.Datalayer, kafkaConfig.Manager, kafkaConfig.Manager, mc, kvo, serverConfig.Queuesize, serverConfig.Concurrency, logger, grpcServiceName)
	go grpcSvr.ListenRPC(serverConfig.GRPCAddr, serverConfig.GRPCPort)

	ha := httphandlers.NewHTTPAdapter(grpcSvr)

	stop := make(chan os.Signal, 10)
	signal.Notify(stop, syscall.SIGTERM) //non-portable outside of POSIX systems
	signal.Notify(stop, os.Interrupt)

	startGC(dc, mc, logger, serverConfig.GCIntervalSecs)
	go healthcheck(ha)
	go pprof()

	r := mux.NewRouter()
	r.HandleFunc("/", versionHandler).Methods("GET")
	r.HandleFunc("/build", ha.BuildRequestHandler).Methods("POST")
	r.HandleFunc("/build/{id}", ha.BuildStatusHandler).Methods("GET")
	r.HandleFunc("/build/{id}", ha.BuildCancelHandler).Methods("DELETE")

	tlsconfig := &tls.Config{MinVersion: tls.VersionTLS12}
	addr := fmt.Sprintf("%v:%v", serverConfig.HTTPSAddr, serverConfig.HTTPSPort)
	server := &http.Server{Addr: addr, Handler: r, TLSConfig: tlsconfig}

	go func() {
		_ = <-stop
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		server.Shutdown(ctx)
		cancel()
	}()

	logger.Printf("HTTPS REST listening on: %v", addr)
	logger.Println(server.ListenAndServeTLS(certPath, keyPath))
	logger.Printf("shutting down GRPC and aborting builds...")
	tracer.Stop()
	grpcSvr.Shutdown()
	close(stop)
	logger.Printf("done, exiting")
}

var version, description string

func setupVersion() {
	bv := make([]byte, 20)
	bd := make([]byte, 2048)
	fv, err := os.Open("VERSION.txt")
	if err != nil {
		return
	}
	defer fv.Close()
	sv, err := fv.Read(bv)
	if err != nil {
		return
	}
	fd, err := os.Open("DESCRIPTION.txt")
	if err != nil {
		return
	}
	defer fd.Close()
	sd, err := fd.Read(bd)
	if err != nil {
		return
	}
	version = string(bv[:sv])
	description = string(bd[:sd])
}

func versionHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Content-Type", "application/json")
	version := struct {
		Name        string `json:"name"`
		Version     string `json:"version"`
		Description string `json:"description"`
	}{
		Name:        "furan",
		Version:     version,
		Description: description,
	}
	vb, err := json.Marshal(version)
	if err != nil {
		w.Write([]byte(fmt.Sprintf(`{"error": "error marshalling version: %v"}`, err)))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Write(vb)
}
