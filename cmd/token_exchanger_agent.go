package cmd

import tokenexchange "github.com/red-hat-storage/odf-multicluster-orchestrator/addons/token-exchange"

func init() {
	rootCmd.AddCommand(tokenexchange.NewAgentCommand())
}
