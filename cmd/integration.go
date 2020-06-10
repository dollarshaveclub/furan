package cmd

import (
	"github.com/dollarshaveclub/furan/pkg/generated/furanrpc"

	"github.com/spf13/cobra"
)

// integrationCmd represents the integration command
var integrationCmd = &cobra.Command{
	Use:   "integration",
	Short: "Run a set of integration tests",
	Long: `Run integration tests locally.

Uses secrets from either env vars or JSON file. Both will use --vault-path-prefix for naming.

Env var: <vault path prefix>_<secret name>   Example: SECRET_PRODUCTION_FURAN_AWS_ACCESS_KEY_ID
JSON file (object): { "secret/production/furan/aws/access_key_id": "asdf" }

Pass a JSON file containing options for the integration test (see testdata/integration.json).`,
	Run: integration,
}

var integrationOptionsFile string

type IntegrationOptions struct {
	GitHubRepo       string   `json:"github_repo"`
	Ref              string   `json:"ref"`
	ImageRepo        string   `json:"image_repo"`
	SkipIfExists     bool     `json:"skip"`
	ECRRegistryHosts []string `json:"ecr_registry_hosts"`
}

func (iops IntegrationOptions) BuildRequest() *furanrpc.BuildRequest {
	return &furanrpc.BuildRequest{
		Build: &furanrpc.BuildDefinition{
			GithubRepo:       iops.GitHubRepo,
			Ref:              iops.Ref,
			TagWithCommitSha: true,
		},
		Push: &furanrpc.PushDefinition{
			Registry: &furanrpc.PushRegistryDefinition{
				Repo: iops.ImageRepo,
			},
		},
		SkipIfExists: iops.SkipIfExists,
	}
}

func init() {
	integrationCmd.Flags().StringVar(&integrationOptionsFile, "integration-options-file", "testdata/integration.json", "JSON integration options file")
	RootCmd.AddCommand(integrationCmd)
}

func integration(cmd *cobra.Command, args []string) {
}
