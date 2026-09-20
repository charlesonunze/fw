package generator

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewServiceCreatesPrefixedApplicationService(t *testing.T) {
	t.Chdir(t.TempDir())

	if err := NewService("mailer", "example.com/app"); err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	path := filepath.Join("internal", "services", "mailer", "mailer_service.go")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	for _, declaration := range []string{
		"package mailer",
		"type Service struct",
		"func New() *Service",
		`func (*Service) Name() string { return "mailer" }`,
		"func (*Service) Close() error",
		"var _ fw.Service = (*Service)(nil)",
	} {
		if !strings.Contains(string(content), declaration) {
			t.Errorf("generated service missing %q:\n%s", declaration, content)
		}
	}
	if _, err := parser.ParseFile(token.NewFileSet(), path, content, parser.AllErrors); err != nil {
		t.Fatalf("generated service is invalid Go: %v\n%s", err, content)
	}

	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "mailer_service.go" {
		t.Fatalf("generated service files = %v, want [mailer_service.go]", entries)
	}
}

func TestNewServiceRejectsExistingService(t *testing.T) {
	t.Chdir(t.TempDir())

	if err := NewService("mailer", "example.com/app"); err != nil {
		t.Fatalf("first NewService() error = %v", err)
	}
	if err := NewService("mailer", "example.com/app"); err == nil {
		t.Fatal("second NewService() error = nil, want existing service error")
	}
}

func TestNewServiceRejectsUnsafeInputBeforeCreatingFiles(t *testing.T) {
	workspace := t.TempDir()
	t.Chdir(workspace)

	if err := NewService("../mailer", "example.com/app"); err == nil {
		t.Fatal("NewService() error = nil, want invalid service name error")
	}
	if _, err := os.Lstat("internal"); !os.IsNotExist(err) {
		t.Fatalf("service directories created for invalid name; stat error = %v", err)
	}

	if err := NewService("mailer", "example.com/app\nreplace evil.invalid => /tmp"); err == nil {
		t.Fatal("NewService() error = nil, want invalid module path error")
	}
	if _, err := os.Lstat("internal"); !os.IsNotExist(err) {
		t.Fatalf("service directories created for invalid module path; stat error = %v", err)
	}
}
