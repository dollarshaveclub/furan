package cmd

import (
	"strings"

	"github.com/spf13/cobra"
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
	buildCmd.PersistentFlags().StringVar(&tags, "tags", "master", "image tags (optional, comma-delimited)")
	buildCmd.PersistentFlags().BoolVar(&cliBuildRequest.Build.TagWithCommitSha, "tag-sha", false, "additionally tag with git commit SHA (optional)")
	buildCmd.PersistentFlags().BoolVar(&buildS3ErrorLogs, "s3-error-logs", false, "Upload failed build logs to S3 (region and bucket must be specified)")
	buildCmd.PersistentFlags().StringVar(&buildS3ErrorLogRegion, "s3-error-log-region", "us-west-2", "Region for S3 error log upload")
	buildCmd.PersistentFlags().StringVar(&buildS3ErrorLogBucket, "s3-error-log-bucket", "", "Bucket for S3 error log upload")
	buildCmd.PersistentFlags().UintVar(&buildS3ErrorLogsPresignTTL, "s3-error-log-presign-ttl", 60*4, "Presigned error log URL TTL in minutes (0 to disable)")
	buildCmd.PersistentFlags().StringSliceVar(&buildArgs, "build-arg", []string{}, "Build arg to use for build request")
	buildCmd.PersistentFlags().BoolVar(&awsConfig.EnableECR, "ecr", false, "Enable AWS ECR support")
	buildCmd.PersistentFlags().StringSliceVar(&awsConfig.ECRRegistryHosts, "ecr-registry-urls", []string{}, "ECR registry urls (ex: 123456789.dkr.ecr.us-west-2.amazonaws.com) to authorize for base images")
	RootCmd.AddCommand(buildCmd)
}

func validateCLIBuildRequest() {
	cliBuildRequest.Build.Tags = strings.Split(tags, ",")
	cliBuildRequest.Build.Args = buildArgsFromSlice(buildArgs)

	if cliBuildRequest.Push.Registry.Repo == "" {
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
}
