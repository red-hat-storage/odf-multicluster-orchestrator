package cmd

import (
	"bytes"
	"context"
	"fmt"
	"os"

	"github.com/red-hat-storage/odf-multicluster-orchestrator/version"
	"github.com/spf13/cobra"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
)

var rootCmd = &cobra.Command{
	Use:   "odfx",
	Short: "Multicluster Orchestrator for ODF",
	Run: func(cmd *cobra.Command, args []string) {
		if err := cmd.Usage(); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(1)
	},
	Version: version.Version,
}

func Execute() {
	ctx := signals.SetupSignalHandler()
	if err := rootCmd.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func ExecuteCommandForTest(ctx context.Context, args []string) (string, error) {
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs(args)
	err := rootCmd.ExecuteContext(ctx)
	return buf.String(), err
}
