package cmd

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"

	docker "github.com/docker/engine-api/client"
	"github.com/dollarshaveclub/furan/generated/lib"
	"github.com/dollarshaveclub/furan/lib/builder"
	"github.com/dollarshaveclub/furan/lib/consul"
	githubfetch "github.com/dollarshaveclub/furan/lib/github_fetch"
	"github.com/dollarshaveclub/furan/lib/grpc"
	"github.com/dollarshaveclub/furan/lib/s3"
	"github.com/dollarshaveclub/furan/lib/squasher"
	streamadapter "github.com/dollarshaveclub/furan/lib/stream_adapter"
	"github.com/dollarshaveclub/furan/lib/tagcheck"
	"github.com/dollarshaveclub/furan/lib/vault"
	"github.com/spf13/cobra"
	"golang.org/x/net/context"
)

var buildCmd = &cobra.Command{
	Use:   "build",
	Short: "Build and push a docker image from repo",
	Long: `Build a Docker image locally from the specified git repository and push
to the specified image repository or S3 target.

Set the following environment variables to allow access to your local Docker engine/daemon:

DOCKER_HOST
DOCKER_API_VERSION (optional)
DOCKER_TLS_VERIFY
DOCKER_CERT_PATH
`,
	PreRun: func(cmd *cobra.Command, args []string) {
		if buildS3ErrorLogs {
			if buildS3ErrorLogBucket == "" {
				clierr("S3 error log bucket must be defined")
			}
			if buildS3ErrorLogRegion == "" {
				clierr("S3 error log region must be defined")
			}
		}
	},
	Run: build,
}

var buildS3ErrorLogs bool
var buildS3ErrorLogRegion, buildS3ErrorLogBucket string
var buildS3ErrorLogsPresignTTL uint

func init() {
	buildCmd.PersistentFlags().StringVar(&cliBuildRequest.Build.GithubRepo, "github-repo", "", "source github repo")
	buildCmd.PersistentFlags().StringVar(&cliBuildRequest.Build.Ref, "source-ref", "master", "source git ref")
	buildCmd.PersistentFlags().StringVar(&cliBuildRequest.Build.DockerfilePath, "dockerfile-path", "Dockerfile", "Dockerfile path (optional)")
	buildCmd.PersistentFlags().StringVar(&cliBuildRequest.Push.Registry.Repo, "image-repo", "", "push to image repo")
	buildCmd.PersistentFlags().StringVar(&cliBuildRequest.Push.S3.Region, "s3-region", "", "S3 region")
	buildCmd.PersistentFlags().StringVar(&cliBuildRequest.Push.S3.Bucket, "s3-bucket", "", "S3 bucket")
	buildCmd.PersistentFlags().StringVar(&cliBuildRequest.Push.S3.KeyPrefix, "s3-key-prefix", "", "S3 key prefix")
	buildCmd.PersistentFlags().StringVar(&tags, "tags", "master", "image tags (optional, comma-delimited)")
	buildCmd.PersistentFlags().BoolVar(&cliBuildRequest.Build.TagWithCommitSha, "tag-sha", false, "additionally tag with git commit SHA (optional)")
	buildCmd.PersistentFlags().BoolVar(&buildS3ErrorLogs, "s3-error-logs", false, "Upload failed build logs to S3 (region and bucket must be specified)")
	buildCmd.PersistentFlags().StringVar(&buildS3ErrorLogRegion, "s3-error-log-region", "us-west-2", "Region for S3 error log upload")
	buildCmd.PersistentFlags().StringVar(&buildS3ErrorLogBucket, "s3-error-log-bucket", "", "Bucket for S3 error log upload")
	buildCmd.PersistentFlags().UintVar(&buildS3ErrorLogsPresignTTL, "s3-error-log-presign-ttl", 60*4, "Presigned error log URL TTL in minutes (0 to disable)")
	buildCmd.PersistentFlags().StringVar(&consulConfig.Addr, "consul-addr", "127.0.0.1:8500", "Consul address (IP:port)")
	buildCmd.PersistentFlags().StringVar(&consulConfig.KVPrefix, "consul-kv-prefix", "furan", "Consul KV prefix")
	buildCmd.PersistentFlags().StringSliceVar(&buildArgs, "build-arg", []string{}, "Build arg to use for build request")
	RootCmd.AddCommand(buildCmd)
}

func validateCLIBuildRequest() {
	cliBuildRequest.Build.Tags = strings.Split(tags, ",")
	cliBuildRequest.Build.Args = buildArgsFromSlice(buildArgs)

	if cliBuildRequest.Push.Registry.Repo == "" &&
		cliBuildRequest.Push.S3.Region == "" &&
		cliBuildRequest.Push.S3.Bucket == "" &&
		cliBuildRequest.Push.S3.KeyPrefix == "" {
		clierr("you must specify either a Docker registry or S3 region/bucket/key-prefix as a push target")
	}
	if cliBuildRequest.Build.GithubRepo == "" {
		clierr("GitHub repo is required")
	}
	if cliBuildRequest.Build.Ref == "" {
		clierr("Source ref is required")
	}
}

func buildArgsFromSlice(args []string) map[string]string {
	buildArgs := make(map[string]string)
	for _, arg := range args {
		kv := strings.Split(arg, "=")
		if len(kv) != 2 {
			continue
		}

		buildArgs[kv[0]] = kv[1]
	}
	return buildArgs
}

func build(cmd *cobra.Command, args []string) {
	ctx, cancel := context.WithCancel(context.Background())
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	go func() {
		for _ = range c {
			cancel()
			os.Exit(1)
			return
		}
	}()

	validateCLIBuildRequest()
	vault.SetupVault(&vaultConfig, &awsConfig, &dockerConfig, &gitConfig, &serverConfig, awscredsprefix)
	setupDB(initializeDB)

	dnull, err := os.Open(os.DevNull)
	if err != nil {
		clierr("error opening %v: %v", os.DevNull, err)
	}
	defer dnull.Close()

	logger = log.New(dnull, "", log.LstdFlags)
	clogger := log.New(os.Stderr, "", log.LstdFlags)

	mc, err := newDatadogCollector()
	if err != nil {
		log.Fatalf("error creating Datadog collector: %v", err)
	}
	setupKafka(mc)
	err = getDockercfg()
	if err != nil {
		clierr("Error getting dockercfg: %v", err)
	}

	gf := githubfetch.NewGitHubFetcher(gitConfig.Token)
	dc, err := docker.NewEnvClient()
	if err != nil {
		clierr("error creating Docker client: %v", err)
	}

	osm := s3.NewS3StorageManager(awsConfig, mc, clogger)
	is := squasher.NewDockerImageSquasher(clogger)
	itc := tagcheck.NewRegistryTagChecker(&dockerConfig, logger.Printf)
	s3errcfg := builder.S3ErrorLogConfig{
		PushToS3:          buildS3ErrorLogs,
		Region:            buildS3ErrorLogRegion,
		Bucket:            buildS3ErrorLogBucket,
		PresignTTLMinutes: buildS3ErrorLogsPresignTTL,
	}
	ib, err := builder.NewImageBuilder(kafkaConfig.Manager, dbConfig.Datalayer, gf, dc, mc, osm, is, itc, dockerConfig.DockercfgContents, s3errcfg, logger)
	if err != nil {
		clierr("error creating image builder: %v", err)
	}

	kvo, err := consul.NewConsulKVOrchestrator(&consulConfig)
	if err != nil {
		clierr("error creating key value orchestrator: %v", err)
	}

	logger = log.New(dnull, "", log.LstdFlags)
	grpcServiceName := strings.Join([]string{datadogServiceName, "grpc"}, ".")
	gs := grpc.NewGRPCServer(ib, dbConfig.Datalayer, kafkaConfig.Manager, kafkaConfig.Manager, mc, kvo, 1, 1, logger, grpcServiceName)

	resp, err := gs.StartBuild(ctx, &cliBuildRequest)
	if err != nil {
		clierr("error running build: %v", err)
	}

	fmt.Fprintf(os.Stdout, "build id: %v\n", resp.BuildId)

	req := &lib.BuildStatusRequest{
		BuildId: resp.BuildId,
	}

	ls := streamadapter.NewLocalServerStream(ctx, os.Stdout)
	err = gs.MonitorBuild(req, ls)
	if err != nil {
		clierr("error monitoring build: %v", err)
	}
}
