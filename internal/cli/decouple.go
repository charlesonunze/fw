package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charlesonunze/fw/internal/generator"

	"github.com/spf13/cobra"
)

var (
	decoupleOutput    string
	decouplePort      string
	decoupleTransport string
	decoupleRouter    string
	decoupleLocalFW   string
)

var decoupleCmd = &cobra.Command{
	Use:   "decouple <module>",
	Short: "Scaffold a standalone service project from a module",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		moduleName := args[0]

		if !strings.HasPrefix(decouplePort, ":") {
			return fmt.Errorf("--port must start with ':', e.g. :8081")
		}
		if decoupleTransport != "http" && decoupleTransport != "grpc" {
			return fmt.Errorf("--transport must be 'http' or 'grpc'")
		}

		modPath, err := generator.DetectModulePath(".")
		if err != nil {
			return fmt.Errorf("could not detect Go module path: %w\nAre you in a project root with go.mod?", err)
		}

		output := decoupleOutput
		if output == "" {
			output = filepath.Join("microservices", moduleName)
		}

		return generator.DecoupleModule(
			moduleName,
			modPath,
			output,
			decouplePort,
			decoupleTransport,
			decoupleRouter,
			decoupleLocalFW,
		)
	},
}

func init() {
	decoupleCmd.Flags().StringVar(&decoupleOutput, "output", "", "Output path for the new service (default: ./microservices/<module>)")
	decoupleCmd.Flags().StringVar(&decouplePort, "port", ":8081", "Listen address for the standalone service")
	decoupleCmd.Flags().StringVar(&decoupleTransport, "transport", "http", "Standalone and dependency client transport: http or grpc")
	decoupleCmd.Flags().StringVar(&decoupleRouter, "router", generator.DefaultRouter, "HTTP router adapter: chi or gin")
	decoupleCmd.Flags().StringVar(&decoupleLocalFW, "local", "", "Path to local fw framework (adds replace directives in go.mod)")
	rootCmd.AddCommand(decoupleCmd)
}
