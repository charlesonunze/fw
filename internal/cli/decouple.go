package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charlesonunze/fw/internal/generator"

	"github.com/spf13/cobra"
)

func newDecoupleCommand() *cobra.Command {
	var output, port, transport, router, localFW string
	cmd := &cobra.Command{
		Use:   "decouple <module>",
		Short: "Scaffold a standalone service project from a module",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			moduleName := args[0]

			if !strings.HasPrefix(port, ":") {
				return fmt.Errorf("--port must start with ':', e.g. :8081")
			}
			if transport != "http" && transport != "grpc" {
				return fmt.Errorf("--transport must be 'http' or 'grpc'")
			}

			modPath, err := generator.DetectModulePath(".")
			if err != nil {
				return fmt.Errorf("could not detect Go module path: %w\nAre you in a project root with go.mod?", err)
			}

			outputPath := output
			if outputPath == "" {
				outputPath = filepath.Join("microservices", moduleName)
			}

			return generator.DecoupleModule(moduleName, modPath, outputPath, port, transport, router, localFW)
		},
	}
	cmd.Flags().StringVar(&output, "output", "", "Output path for the new service (default: ./microservices/<module>)")
	cmd.Flags().StringVar(&port, "port", ":8081", "Listen address for the standalone service")
	cmd.Flags().StringVar(&transport, "transport", "http", "Standalone and dependency client transport: http or grpc")
	cmd.Flags().StringVar(&router, "router", "", "HTTP router: chi or gin (defaults to .fw.json; must match the source module)")
	cmd.Flags().StringVar(&localFW, "local", "", "Path to local fw framework (adds replace directives in go.mod)")
	return cmd
}
