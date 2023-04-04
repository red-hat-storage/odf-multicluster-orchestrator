package cmd

import (
	maintenance "github.com/red-hat-storage/odf-multicluster-orchestrator/addons/maintainence-agent"
)

func init() {
	rootCmd.AddCommand(maintenance.NewAgentCommand())
}
