package cli

import (
	"fmt"

	"github.com/charlesonunze/fw/internal/generator"

	"github.com/spf13/cobra"
)

var generateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate code (modules, protos, etc.)",
}

var generateModuleCmd = &cobra.Command{
	Use:   "module <name>",
	Short: "Generate a flat, self-contained module with consistently prefixed files",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		modPath, err := generator.DetectModulePath(".")
		if err != nil {
			return fmt.Errorf("could not detect Go module path: %w\nAre you in a project root with go.mod?", err)
		}
		return generator.NewModule(args[0], modPath)
	},
}

var generateProtoCmd = &cobra.Command{
	Use:   "proto [module]",
	Short: "Scaffold a .proto file for a module and generate Go code via protoc",
	Long: `With a module name: creates proto/<module>.proto and generates pb.go files.
Without arguments: regenerates Go code for all .proto files under proto/.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return generator.GenerateProto()
		}

		modPath, err := generator.DetectModulePath(".")
		if err != nil {
			return fmt.Errorf("could not detect Go module path: %w\nAre you in a project root with go.mod?", err)
		}
		return generator.NewProto(args[0], modPath)
	},
}

func init() {
	generateCmd.AddCommand(generateModuleCmd)
	generateCmd.AddCommand(generateProtoCmd)
	rootCmd.AddCommand(generateCmd)
}
