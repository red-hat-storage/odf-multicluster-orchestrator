package cmd

import "github.com/red-hat-storage/odf-multicluster-orchestrator/controllers"

func init() {
	rootCmd.AddCommand(controllers.NewManagerCommand())
}
