package cli

import (
	"fmt"

	"github.com/charlesonunze/fw/internal/generator"

	"github.com/spf13/cobra"
)

var generateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate code (modules, etc.)",
}

var generateModuleCmd = &cobra.Command{
	Use:   "module <name>",
	Short: "Generate a new module with domain, service, repo, api, transform, and module.go",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		moduleName := args[0]

		// Detect module path from go.mod in current directory
		modPath, err := generator.DetectModulePath(".")
		if err != nil {
			return fmt.Errorf("could not detect Go module path: %w\nAre you in a project root with go.mod?", err)
		}

		return generator.NewModule(moduleName, modPath)
	},
}

func init() {
	generateCmd.AddCommand(generateModuleCmd)
	rootCmd.AddCommand(generateCmd)
}
