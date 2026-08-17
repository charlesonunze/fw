package generator

import (
	"fmt"
	"path/filepath"

	"github.com/charlesonunze/fw/internal/hotreload"
)

const projectGitignore = `# Air
/tmp/
`

func writeDevelopmentFiles(output string) error {
	fmt.Printf("  create %s\n", hotreload.ConfigFile)
	if _, err := hotreload.EnsureConfig(output); err != nil {
		return err
	}

	fmt.Printf("  create .gitignore\n")
	if err := writeTemplate(filepath.Join(output, ".gitignore"), projectGitignore, nil); err != nil {
		return err
	}

	return nil
}
