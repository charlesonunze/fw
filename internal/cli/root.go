package cli

import "github.com/spf13/cobra"

func newRootCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fw",
		Short: "A modular Go framework for monoliths and microservices",
	}
	cmd.AddCommand(newProjectCommand(), newGenerateCommand(), newDecoupleCommand(), newDevCommand())
	return cmd
}

// Execute runs the root command.
func Execute() error {
	return newRootCommand().Execute()
}
