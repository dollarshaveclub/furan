package cmd

import (
	"io"
	"log"
	"os"

	"github.com/dollarshaveclub/furan/pkg/squasher"
	"github.com/dustin/go-humanize"
	"github.com/spf13/cobra"
	"golang.org/x/net/context"
)

var verboseSquash bool

// squashCmd represents the squash command
var squashCmd = &cobra.Command{
	Use:   "squash [input] [output]",
	Short: "Flatten a Docker image",
	Long: `Flatten a Docker image to its final layer and output the image as a
tar archive suitable for Docker import.

Specify "-" for input or output to use stdin or stdout, respectively.`,
	Run: squash,
}

func init() {
	squashCmd.PersistentFlags().BoolVar(&verboseSquash, "verbose", false, "verbose output")
	RootCmd.AddCommand(squashCmd)
}

func squash(cmd *cobra.Command, args []string) {
	var input io.ReadCloser
	var output io.WriteCloser
	var err error
	if len(args) != 2 {
		clierr("input and output required")
	}
	switch args[0] {
	case "-":
		input = os.Stdin
	default:
		input, err = os.Open(args[0])
		defer input.Close()
		if err != nil {
			clierr("error opening input: %v", err)
		}
	}
	switch args[1] {
	case "-":
		output = os.Stdout
	default:
		output, err = os.Open(args[0])
		defer output.Close()
		if err != nil {
			clierr("error opening input: %v", err)
		}
	}
	var sink io.Writer
	if verboseSquash {
		sink = os.Stderr
	} else {
		var dnull *os.File
		dnull, err = os.Open(os.DevNull)
		if err != nil {
			clierr("error opening %v: %v", os.DevNull, err)
		}
		defer dnull.Close()
		sink = dnull
	}
	logger = log.New(sink, "", log.LstdFlags)
	squasher := squasher.NewDockerImageSquasher(logger)

	si, err := squasher.Squash(context.Background(), input, output)
	if err != nil {
		clierr("error squashing: %v", err)
	}
	logger.Printf("***** squash stats *****\n")
	logger.Printf("input size: %v (%v bytes)\n", humanize.Bytes(si.InputBytes), si.InputBytes)
	logger.Printf("output size: %v (%v bytes)\n", humanize.Bytes(si.OutputBytes), si.OutputBytes)
	var numprefix string
	var diffnum uint64
	if si.SizeDifference < 0 {
		numprefix = "-"
		diffnum = uint64(-si.SizeDifference)
	} else {
		diffnum = uint64(si.SizeDifference)
	}
	pct := (float64(diffnum) / float64(si.InputBytes)) * 100
	logger.Printf("image size difference: %v%v (%v bytes) - %.3f%% reduction\n", numprefix, humanize.Bytes(diffnum), si.SizeDifference, pct)
	logger.Printf("layers removed: %v\n", si.LayersRemoved)
	logger.Printf("files removed: %v\n", si.FilesRemovedCount)
}
