package generator

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestNewModuleCreatesFlatPrefixedPackage(t *testing.T) {
	t.Chdir(t.TempDir())

	if err := NewModule("user", "example.com/app"); err != nil {
		t.Fatalf("NewModule() error = %v", err)
	}

	base := filepath.Join("internal", "modules", "user")
	want := []string{
		"user_http.go",
		"user_model.go",
		"user_module.go",
		"user_repository.go",
		"user_repository_memory.go",
		"user_service.go",
	}

	entries, err := os.ReadDir(base)
	if err != nil {
		t.Fatalf("ReadDir(%q) error = %v", base, err)
	}

	got := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			t.Fatalf("generated unexpected subdirectory %q", entry.Name())
		}
		if !strings.HasPrefix(entry.Name(), "user_") {
			t.Errorf("generated file %q without module prefix", entry.Name())
		}
		got = append(got, entry.Name())

		path := filepath.Join(base, entry.Name())
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.PackageClauseOnly)
		if err != nil {
			t.Errorf("ParseFile(%q) error = %v", path, err)
			continue
		}
		if file.Name.Name != "user" {
			t.Errorf("package in %q = %q, want user", path, file.Name.Name)
		}
	}

	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Errorf("generated files = %v, want %v", got, want)
	}

	wiring, err := os.ReadFile(filepath.Join(base, "user_module.go"))
	if err != nil {
		t.Fatalf("ReadFile(user_module.go) error = %v", err)
	}
	for _, declaration := range []string{
		"func (m *Module) Register(deps *fw.Deps) error",
		"func (m *Module) Init(_ context.Context, _ *fw.Deps) error",
	} {
		if !strings.Contains(string(wiring), declaration) {
			t.Errorf("generated module missing %q:\n%s", declaration, wiring)
		}
	}
}

func TestNewModuleRejectsExistingModule(t *testing.T) {
	t.Chdir(t.TempDir())

	if err := NewModule("user", "example.com/app"); err != nil {
		t.Fatalf("first NewModule() error = %v", err)
	}
	if err := NewModule("user", "example.com/app"); err == nil {
		t.Fatal("second NewModule() error = nil, want existing module error")
	}
}
