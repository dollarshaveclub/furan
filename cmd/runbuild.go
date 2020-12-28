package cmd

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/gofrs/uuid"
	"github.com/moby/buildkit/session"
	"github.com/spf13/cobra"

	"github.com/dollarshaveclub/furan/pkg/auth"
	"github.com/dollarshaveclub/furan/pkg/builder"
	"github.com/dollarshaveclub/furan/pkg/buildkit"
	"github.com/dollarshaveclub/furan/pkg/datalayer"
	"github.com/dollarshaveclub/furan/pkg/github"
	"github.com/dollarshaveclub/furan/pkg/models"
	"github.com/dollarshaveclub/furan/pkg/s3"
	"github.com/dollarshaveclub/furan/pkg/tagcheck"
)

var runbuildCmd = &cobra.Command{
	Use:   "runbuild",
	Short: "Run a build job",
	Long: `This executes a build job with a running instance of BuildKit.

This is intended to be used in a Kubernetes pod with BuildKit as part of an automated build job
created by pkg/jobrunner.`,
	PreRun: func(cmd *cobra.Command, args []string) {
		gitHubSecrets()
		awsSecrets()
		quaySecrets()
		dbSecrets()
	},
	RunE: runbuild,
}

var buildid, bkaddr string
var runbuildtimeout time.Duration

func init() {
	serverAndRunnerFlags(runbuildCmd)
	runbuildCmd.PersistentFlags().StringVar(&buildid, "build-id", "", "Build ID")
	runbuildCmd.PersistentFlags().StringVar(&bkaddr, "buildkit-addr", "", "BuildKit UNIX socket address (unix:///path/to/socket)")
	runbuildCmd.PersistentFlags().DurationVar(&runbuildtimeout, "timeout", 30*time.Minute, "max build duration/timeout")
	RootCmd.AddCommand(runbuildCmd)
}

func runbuild(cmd *cobra.Command, args []string) error {
	dl, err := datalayer.NewPostgresDBLayer(dbConfig.PostgresURI)
	if err != nil {
		return fmt.Errorf("error configuring database: %w", err)
	}

	cm := &s3.CacheManager{
		AccessKeyID:     awsConfig.AccessKeyID,
		SecretAccessKey: awsConfig.SecretAccessKey,
		Region:          awsConfig.Region,
		Bucket:          awsConfig.CacheBucket,
		Keypfx:          awsConfig.CacheKeyPrefix,
		DL:              dl,
	}

	tc := &tagcheck.Checker{
		Quay: &tagcheck.QuayChecker{APIToken: quayConfig.Token},
		ECR: &tagcheck.ECRChecker{
			AccessKeyID:     awsConfig.AccessKeyID,
			SecretAccessKey: awsConfig.SecretAccessKey,
		},
	}

	bks, err := buildkit.NewBuildSolver(bkaddr, cm, dl)
	if err != nil {
		return fmt.Errorf("error initializing build solver: %w", err)
	}
	bks.LogF = log.Printf
	bks.AuthProviderFunc = func() []session.Attachable {
		return []session.Attachable{
			auth.New(quayConfig.Token, awsConfig.AccessKeyID, awsConfig.SecretAccessKey),
		}
	}

	bm := &builder.Manager{
		DL:             dl,
		BRunner:        bks,
		TCheck:         tc,
		FetcherFactory: func(tkn string) models.CodeFetcher { return github.NewGitHubFetcher(tkn) },
		GitHubTokenKey: dbConfig.CredEncKeyArray,
	}

	bid, err := uuid.FromString(buildid)
	if err != nil {
		return fmt.Errorf("invalid build id: %w", err)
	}

	// verify that buildkitd is up and running
	if err := bks.VerifyAddr(); err != nil {
		return fmt.Errorf("error connecting to buildkitd: %v", err)
	}

	ctx, cf := context.WithTimeout(context.Background(), runbuildtimeout)
	defer cf()

	return bm.Run(ctx, bid)
}
