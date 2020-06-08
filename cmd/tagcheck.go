package cmd

import (
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"strings"

	"github.com/dollarshaveclub/furan/pkg/tagcheck"
	"github.com/spf13/cobra"
)

var exitWithError, verboseLogging bool
var dockercfgPath string

// tagcheckCmd represents the tagcheck command
var tagcheckCmd = &cobra.Command{
	Use:   "tagcheck REPOSITORY tag1,tag2,...",
	Short: "Check if tag(s) exist for a registry repository",
	Long: `Check if one or more tags (comma-separated) exist for a given image repository.
quay.io is currently the only supported registry.`,
	Run: tagcheckcmd,
}

func init() {
	tagcheckCmd.Flags().BoolVar(&exitWithError, "exit-with-error", false, "Exit with non-zero error code if any tags are missing")
	tagcheckCmd.Flags().BoolVar(&verboseLogging, "verbose", false, "Verbose logging")
	tagcheckCmd.Flags().StringVar(&dockercfgPath, "dockercfg", "", "Path to .dockercfg, if needed for a private repository")
	RootCmd.AddCommand(tagcheckCmd)
}

func tagcheckcmd(cmd *cobra.Command, args []string) {
	if len(args) != 2 {
		clierr("repository and comma-separated list of tags are required")
	}
	repo := args[0]
	tags := strings.Split(args[1], ",")
	if len(tags) == 0 {
		clierr("at least one tag is required")
	}
	if strings.Split(repo, "/")[0] != "quay.io" {
		clierr("quay.io is the only supported registry")
	}
	if dockercfgPath != "" {
		d, err := ioutil.ReadFile(dockercfgPath)
		if err != nil {
			clierr("error reading dockercfg: %v", err)
		}
		dockerConfig.DockercfgRaw = string(d)
	} else {
		dockerConfig.DockercfgRaw = "{}"
	}
	if err := getDockercfg(); err != nil {
		clierr("error processing dockercfg: %v", err)
	}
	var out io.Writer
	if verboseLogging {
		out = os.Stderr
	} else {
		out = ioutil.Discard
	}
	logger = log.New(out, "", log.LstdFlags)
	itc := tagcheck.NewRegistryTagChecker(&dockerConfig, logger.Printf)
	ok, missing, err := itc.AllTagsExist(tags, repo)
	if err != nil {
		clierr("error checking tags: %v", err)
	}
	if ok {
		fmt.Printf("All tags exist.\n")
	} else {
		fmt.Printf("Missing tags: %v\n", strings.Join(missing, ", "))
		if exitWithError {
			os.Exit(1)
		}
	}
}
