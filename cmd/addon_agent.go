package cmd

import (
	"github.com/red-hat-storage/odf-multicluster-orchestrator/addons"
)

func init() {
	rootCmd.AddCommand(addons.NewAddonAgentCommand())
}
