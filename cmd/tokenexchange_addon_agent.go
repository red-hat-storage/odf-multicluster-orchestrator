package main

import (
	"github.com/red-hat-storage/odf-multicluster-orchestrator/addons/tokenexchange"
)

func init() {
	rootCmd.AddCommand(tokenexchange.NewAddonAgentCommand())
}
