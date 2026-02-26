package cli

import (
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "fw",
	Short: "A modular Go framework for monoliths and microservices",
}

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
}
