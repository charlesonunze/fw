package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charlesonunze/fw/internal/hotreload"
)

func assertDevelopmentFiles(t *testing.T, output string) {
	t.Helper()

	airPath := filepath.Join(output, hotreload.ConfigFile)
	airConfig, err := os.ReadFile(airPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", airPath, err)
	}
	if !strings.Contains(string(airConfig), `entrypoint = ["./tmp/main"]`) {
		t.Errorf("generated Air configuration has no application entrypoint:\n%s", airConfig)
	}

	gitignorePath := filepath.Join(output, ".gitignore")
	gitignore, err := os.ReadFile(gitignorePath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", gitignorePath, err)
	}
	if !strings.Contains(string(gitignore), "/tmp/") {
		t.Errorf("generated .gitignore does not exclude Air output:\n%s", gitignore)
	}
}
