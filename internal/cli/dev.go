package cli

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/charlesonunze/fw/internal/hotreload"

	"github.com/spf13/cobra"
)

var devCmd = &cobra.Command{
	Use:   "dev [-- application arguments]",
	Short: "Run the app with hot reload (requires air)",
	RunE: func(cmd *cobra.Command, args []string) error {
		if _, err := os.Stat("go.mod"); err != nil {
			return fmt.Errorf("fw dev must run from a project root containing go.mod: %w", err)
		}

		airPath, err := exec.LookPath("air")
		if err != nil {
			cmd.PrintErrln("air is not installed. Install it with:")
			cmd.PrintErrln()
			cmd.PrintErrln("  go install github.com/air-verse/air@latest")
			cmd.PrintErrln()
			return fmt.Errorf("air not found in PATH")
		}

		created, err := hotreload.EnsureConfig(".")
		if err != nil {
			return err
		}
		if created {
			cmd.Printf("  create %s\n", hotreload.ConfigFile)
		}

		airArgs := append([]string{"-c", hotreload.ConfigFile}, args...)
		airCmd := exec.CommandContext(cmd.Context(), airPath, airArgs...)
		airCmd.Stdin = cmd.InOrStdin()
		airCmd.Stdout = cmd.OutOrStdout()
		airCmd.Stderr = cmd.ErrOrStderr()
		if err := airCmd.Run(); err != nil {
			return fmt.Errorf("run Air: %w", err)
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(devCmd)
}
