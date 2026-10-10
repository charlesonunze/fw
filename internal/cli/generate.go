package cli

import (
	"fmt"

	"github.com/charlesonunze/fw/internal/generator"

	"github.com/spf13/cobra"
)

func newGenerateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate code (modules, services, protos, etc.)",
	}
	cmd.AddCommand(newModuleCommand(), newServiceCommand(), newProtoCommand())
	return cmd
}

func newModuleCommand() *cobra.Command {
	var config generator.ModuleConfig
	cmd := &cobra.Command{
		Use:   "module <name>",
		Short: "Generate a flat, self-contained module with consistently prefixed files",
		Long: `Generate a flat, self-contained module with consistently prefixed files.

Supported transports are http, grpc, and none. HTTP is the default.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			modPath, err := generator.DetectModulePath(".")
			if err != nil {
				return fmt.Errorf("could not detect Go module path: %w\nAre you in a project root with go.mod?", err)
			}
			return generator.NewModule(args[0], modPath, config)
		},
	}
	cmd.Flags().StringVar(&config.Router, "router", "", "HTTP router: chi or gin (defaults to .fw.json)")
	cmd.Flags().StringVar(&config.Transport, "transport", generator.ModuleTransportHTTP, "Module transport: http, grpc, or none")
	return cmd
}

func newServiceCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "service <name>",
		Short: "Generate a standalone application service",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			modPath, err := generator.DetectModulePath(".")
			if err != nil {
				return fmt.Errorf("could not detect Go module path: %w\nAre you in a project root with go.mod?", err)
			}
			return generator.NewService(args[0], modPath)
		},
	}
}

func newProtoCommand() *cobra.Command {
	return &cobra.Command{
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
}
