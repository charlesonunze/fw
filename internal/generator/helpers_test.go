package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteTemplateRefusesToOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "generated.go")
	writeFixture(t, path, "original")

	err := writeTemplate(path, "replacement", nil)
	if err == nil {
		t.Fatal("writeTemplate() error = nil, want overwrite error")
	}
	content, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, readErr)
	}
	if string(content) != "original" {
		t.Fatalf("existing file content = %q, want original", content)
	}
}

func TestWriteTemplateDoesNotCreateFileOnTemplateError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "generated.go")
	if err := writeTemplate(path, "{{", nil); err == nil {
		t.Fatal("writeTemplate() error = nil, want parse error")
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("generated file exists after template failure; stat error = %v", err)
	}
}

func TestDetectModulePathUsesGoModSyntax(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, filepath.Join(dir, "go.mod"), "module example.com/acme/app\n\ngo 1.25\n")

	got, err := DetectModulePath(dir)
	if err != nil {
		t.Fatalf("DetectModulePath() error = %v", err)
	}
	if got != "example.com/acme/app" {
		t.Fatalf("DetectModulePath() = %q, want example.com/acme/app", got)
	}
}

func TestDetectModulePathRejectsUnsafePath(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, filepath.Join(dir, "go.mod"), "module example.com/acme/../app\n")

	_, err := DetectModulePath(dir)
	if err == nil || !strings.Contains(err.Error(), "invalid Go module path") {
		t.Fatalf("DetectModulePath() error = %v, want invalid module path error", err)
	}
}
