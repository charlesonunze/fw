package generator

import (
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewServiceCreatesPrefixedApplicationService(t *testing.T) {
	t.Chdir(t.TempDir())

	var generateErr error
	output := captureStdout(t, func() {
		generateErr = NewService("mailer", "example.com/app")
	})
	if generateErr != nil {
		t.Fatalf("NewService() error = %v", generateErr)
	}
	if want := "app.RegisterService(mailerService, fw.As[mailer.Service]())"; !strings.Contains(output, want) {
		t.Fatalf("NewService() output missing %q:\n%s", want, output)
	}

	path := filepath.Join("internal", "services", "mailer", "mailer_service.go")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	for _, declaration := range []string{
		"package mailer",
		"type Service interface",
		"fw.Service",
		"type service struct",
		"func New() Service",
		`func (*service) Name() string { return "mailer" }`,
		"func (*service) Health(context.Context) error",
		"func (*service) Close() error",
		"var _ Service = (*service)(nil)",
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

func captureStdout(t *testing.T, run func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	original := os.Stdout
	os.Stdout = writer
	run()
	os.Stdout = original
	if err := writer.Close(); err != nil {
		t.Fatalf("Close(stdout writer) error = %v", err)
	}
	content, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll(stdout) error = %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("Close(stdout reader) error = %v", err)
	}
	return string(content)
}

func TestNewServiceRejectsExistingService(t *testing.T) {
	t.Chdir(t.TempDir())

	if err := NewService("mailer", "example.com/app"); err != nil {
		t.Fatalf("NewService() error = %v", err)
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
