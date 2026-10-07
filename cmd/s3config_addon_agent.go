package main

import (
	"github.com/red-hat-storage/odf-multicluster-orchestrator/addons/s3config"
)

func init() {
	rootCmd.AddCommand(s3config.NewS3ConfigAddonAgentCommand())
}
