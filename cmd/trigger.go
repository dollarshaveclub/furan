package cmd

import (
	"github.com/spf13/cobra"
)

var discoverFuranHost bool
var consulFuranSvcName string
var remoteFuranHost string
var monitorBuild bool
var buildArgs []string

// triggerCmd represents the trigger command
var triggerCmd = &cobra.Command{
	Use:   "trigger",
	Short: "Start a build on a remote Furan server",
	Long:  `Trigger and then monitor a build on a remote Furan server`,
	Run:   trigger,
}

func init() {
	triggerCmd.PersistentFlags().StringVar(&remoteFuranHost, "remote-host", "", "Remote Furan server with gRPC port (eg: furan.me.com:4001)")
	triggerCmd.PersistentFlags().BoolVar(&discoverFuranHost, "consul-discovery", false, "Discover Furan hosts via Consul")
	triggerCmd.PersistentFlags().StringVar(&consulFuranSvcName, "svc-name", "furan", "Consul service name for Furan hosts")
	triggerCmd.PersistentFlags().StringVar(&cliBuildRequest.Build.GithubRepo, "github-repo", "", "source github repo")
	triggerCmd.PersistentFlags().StringVar(&cliBuildRequest.Build.Ref, "source-ref", "master", "source git ref")
	triggerCmd.PersistentFlags().StringVar(&cliBuildRequest.Build.DockerfilePath, "dockerfile-path", "Dockerfile", "Dockerfile path (optional)")
	triggerCmd.PersistentFlags().StringVar(&cliBuildRequest.Push.Registry.Repo, "image-repo", "", "push to image repo")
	triggerCmd.PersistentFlags().StringVar(&tags, "tags", "master", "image tags (optional, comma-delimited)")
	triggerCmd.PersistentFlags().BoolVar(&cliBuildRequest.Build.TagWithCommitSha, "tag-sha", false, "additionally tag with git commit SHA (optional)")
	triggerCmd.PersistentFlags().BoolVar(&cliBuildRequest.SkipIfExists, "skip-if-exists", false, "if build already exists at destination, skip build/push (registry: all tags exist, s3: object exists)")
	triggerCmd.PersistentFlags().StringSliceVar(&buildArgs, "build-arg", []string{}, "Build arg to use for build request")
	triggerCmd.PersistentFlags().BoolVar(&monitorBuild, "monitor", true, "Monitor build after triggering")
	RootCmd.AddCommand(triggerCmd)
}

func trigger(cmd *cobra.Command, args []string) {
}
