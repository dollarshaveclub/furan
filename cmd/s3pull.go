package cmd

import (
	"bytes"
	"io/ioutil"
	"os"

	"golang.org/x/net/context"

	"log"

	"github.com/aws/aws-sdk-go/aws"
	docker "github.com/docker/engine-api/client"
	"github.com/dollarshaveclub/furan/pkg/s3"
	"github.com/dollarshaveclub/furan/pkg/vault"
	humanize "github.com/dustin/go-humanize"
	"github.com/spf13/cobra"
)

type s3pullopts struct {
	region     string
	bucket     string
	keyprefix  string
	githubRepo string
	commitsha  string
	maxbufsize uint64
}

var s3PullOpts s3pullopts

// s3_pullCmd represents the s3_pull command
var s3pullCmd = &cobra.Command{
	Use:   "s3pull",
	Short: "Pull a container image from S3",
	Long: `Multipart download a container image from S3 and load into the local Docker daemon.
Set the following environment variables to allow access to your local Docker engine/daemon:

DOCKER_HOST
DOCKER_API_VERSION (optional)
DOCKER_TLS_VERIFY
DOCKER_CERT_PATH`,
	PreRun: func(cmd *cobra.Command, arts []string) {
		if s3PullOpts.bucket == "" {
			clierr("S3 bucket is required")
		}
		if s3PullOpts.githubRepo == "" {
			clierr("GitHub repo is required")
		}
		if s3PullOpts.commitsha == "" {
			clierr("Commit SHA is required")
		}
		vault.SetupVault(&vaultConfig, &awsConfig, &dockerConfig, &gitConfig, &serverConfig, awscredsprefix)
	},
	Run: s3pull,
}

func init() {
	s3pullCmd.PersistentFlags().StringVar(&s3PullOpts.region, "region", "us-west-2", "AWS region")
	s3pullCmd.PersistentFlags().StringVar(&s3PullOpts.bucket, "bucket", "", "S3 bucket")
	s3pullCmd.PersistentFlags().StringVar(&s3PullOpts.keyprefix, "keyprefix", "", "S3 key prefix")
	s3pullCmd.PersistentFlags().StringVar(&s3PullOpts.githubRepo, "github-repo", "", "GitHub repo")
	s3pullCmd.PersistentFlags().StringVar(&s3PullOpts.commitsha, "commit-sha", "", "Commit SHA")
	s3pullCmd.PersistentFlags().Uint64Var(&s3PullOpts.maxbufsize, "max-buf-size", 2147000000, "Max buffer size in bytes")
	RootCmd.AddCommand(s3pullCmd)
}

func s3pull(cmd *cobra.Command, args []string) {
	logger = log.New(os.Stderr, "", log.LstdFlags)
	mc, err := newDatadogCollector()
	if err != nil {
		clierr("error creating Datadog collector: %v", err)
	}
	osm := s3.NewS3StorageManager(awsConfig, mc, logger)

	dc, err := docker.NewEnvClient()
	if err != nil {
		clierr("error creating Docker client: %v", err)
	}

	opts := &s3.S3Options{
		Region:    s3PullOpts.region,
		Bucket:    s3PullOpts.bucket,
		KeyPrefix: s3PullOpts.keyprefix,
	}

	desc := s3.ImageDescription{
		GitHubRepo: s3PullOpts.githubRepo,
		CommitSHA:  s3PullOpts.commitsha,
	}

	// Get S3 object info so we can size buffer appropriately
	sz, err := osm.Size(desc, opts)
	if err != nil {
		clierr("error getting S3 object size: %v", err)
	}

	logger.Printf("S3 object size: %v", humanize.Bytes(uint64(sz)))

	if uint64(sz) > s3PullOpts.maxbufsize {
		clierr("object size exceeds maximum buffer size: %v", humanize.Bytes(s3PullOpts.maxbufsize))
	}

	bb := make([]byte, sz)
	buf := aws.NewWriteAtBuffer(bb)

	logger.Printf("downloading %v s3://%v/%v (%v threads)", s3PullOpts.region, s3PullOpts.bucket, s3.GenerateS3KeyName(s3PullOpts.keyprefix, s3PullOpts.githubRepo, s3PullOpts.commitsha), awsConfig.Concurrency)

	err = osm.Pull(desc, buf, opts)
	if err != nil {
		clierr("error pulling from S3: %v", err)
	}

	bufr := bytes.NewBuffer(buf.Bytes())

	logger.Printf("loading into Docker daemon")
	resp, err := dc.ImageLoad(context.Background(), bufr, true)
	if err != nil {
		clierr("error loading image: %v", err)
	}
	defer resp.Body.Close()
	_, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		clierr("error reading Docker daemon response: %v", err)
	}
	logger.Println("done")
}
