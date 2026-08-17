package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/charlesonunze/fw/internal/generator"

	"github.com/spf13/cobra"
)

var (
	modulePath    string
	localFw       string
	projectRouter string
)

var newCmd = &cobra.Command{
	Use:   "new <project-name>",
	Short: "Scaffold a new project",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		projectName := args[0]

		// If --module flag not provided, prompt interactively
		if modulePath == "" {
			fmt.Printf("Go module path (e.g. github.com/you/%s): ", projectName)
			reader := bufio.NewReader(os.Stdin)
			input, err := reader.ReadString('\n')
			if err != nil {
				return fmt.Errorf("failed to read input: %w", err)
			}
			modulePath = strings.TrimSpace(input)
		}

		if modulePath == "" {
			return fmt.Errorf("module path is required")
		}

		return generator.NewProject(projectName, modulePath, projectRouter, localFw)
	},
}

func init() {
	newCmd.Flags().StringVar(&modulePath, "module", "", "Go module path (e.g. github.com/you/myapp)")
	newCmd.Flags().StringVar(&projectRouter, "router", generator.DefaultRouter, "HTTP router adapter: chi or gin")
	newCmd.Flags().StringVar(&localFw, "local", "", "Path to local fw framework (adds replace directive in go.mod)")
	rootCmd.AddCommand(newCmd)
}
