package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/dollarshaveclub/furan/pkg/config"
	"github.com/dollarshaveclub/furan/pkg/generated/furanrpc"
)

var awsConfig config.AWSConfig

// used by build and trigger commands
var cliBuildRequest = furanrpc.BuildRequest{
	Build: &furanrpc.BuildDefinition{},
	Push: &furanrpc.PushDefinition{
		Registry: &furanrpc.PushRegistryDefinition{},
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

func init() {
}

func clierr(msg string, params ...interface{}) {
	fmt.Fprintf(os.Stderr, msg+"\n", params...)
	os.Exit(1)
}
