package cli

import (
	"bufio"
	"fmt"
	"strings"

	"github.com/charlesonunze/fw/internal/generator"

	"github.com/spf13/cobra"
)

func newProjectCommand() *cobra.Command {
	var modulePath, router, localFW string
	cmd := &cobra.Command{
		Use:   "new <project-name>",
		Short: "Scaffold a new project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			projectName := args[0]
			if modulePath == "" {
				fmt.Fprintf(cmd.OutOrStdout(), "Go module path (e.g. github.com/you/%s): ", projectName)
				reader := bufio.NewReader(cmd.InOrStdin())
				input, err := reader.ReadString('\n')
				if err != nil {
					return fmt.Errorf("failed to read input: %w", err)
				}
				modulePath = strings.TrimSpace(input)
			}

			if modulePath == "" {
				return fmt.Errorf("module path is required")
			}

			return generator.NewProject(projectName, modulePath, router, localFW)
		},
	}
	cmd.Flags().StringVar(&modulePath, "module", "", "Go module path (e.g. github.com/you/myapp)")
	cmd.Flags().StringVar(&router, "router", generator.DefaultRouter, "HTTP router adapter: chi or gin")
	cmd.Flags().StringVar(&localFW, "local", "", "Path to local fw framework (adds replace directive in go.mod)")
	return cmd
}
