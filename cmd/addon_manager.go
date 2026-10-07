package main

import (
	"github.com/red-hat-storage/odf-multicluster-orchestrator/internal/controller"
)

func init() {
	rootCmd.AddCommand(controller.NewManagerCommand())
}
