package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateModuleName(t *testing.T) {
	for _, name := range []string{"user", "order_item", "user2"} {
		if err := validateModuleName(name); err != nil {
			t.Errorf("validateModuleName(%q) error = %v", name, err)
		}
	}

	for _, name := range []string{"", "User", "../user", "user-name", "user_", "for", "user/service", "usér"} {
		if err := validateModuleName(name); err == nil {
			t.Errorf("validateModuleName(%q) error = nil, want validation error", name)
		}
	}
}

func TestValidateProjectName(t *testing.T) {
	for _, name := range []string{"todo", "todo-api", "todo_api", "todo.api", "app2"} {
		if err := validateProjectName(name); err != nil {
			t.Errorf("validateProjectName(%q) error = %v", name, err)
		}
	}

	for _, name := range []string{"", "Todo", "../todo", "/tmp/todo", "todo/app", ".todo", "todo.", "todo app"} {
		if err := validateProjectName(name); err == nil {
			t.Errorf("validateProjectName(%q) error = nil, want validation error", name)
		}
	}
}

func TestValidateModulePath(t *testing.T) {
	if err := validateModulePath("example.com/acme/todo"); err != nil {
		t.Fatalf("validateModulePath() error = %v", err)
	}
	for _, path := range []string{"", "todo", "example.com/acme/../todo", "example.com/acme/todo\nreplace evil.invalid => /tmp"} {
		if err := validateModulePath(path); err == nil {
			t.Errorf("validateModulePath(%q) error = nil, want validation error", path)
		}
	}
}

func TestValidatePort(t *testing.T) {
	for _, address := range []string{":1", ":8080", ":65535"} {
		if _, err := validatePort(address); err != nil {
			t.Errorf("validatePort(%q) error = %v", address, err)
		}
	}
	for _, address := range []string{"", "8080", ":0", ":65536", ":8080/path", "localhost:8080", ":8080\nEXPOSE 22"} {
		if _, err := validatePort(address); err == nil {
			t.Errorf("validatePort(%q) error = nil, want validation error", address)
		}
	}
}

func TestValidateOutputPathResolvesSymlinkedParents(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.Mkdir(source, 0o755); err != nil {
		t.Fatalf("Mkdir(source) error = %v", err)
	}
	link := filepath.Join(root, "source-link")
	if err := os.Symlink(source, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	err := validateOutputPath(filepath.Join(link, "output"), source)
	if err == nil || !strings.Contains(err.Error(), "inside source") {
		t.Fatalf("validateOutputPath() error = %v, want source containment error", err)
	}
}

func TestValidateLocalDirectoryPathRejectsSymlinkedComponents(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	target := t.TempDir()
	if err := os.Symlink(target, "internal"); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	err := validateLocalDirectoryPath(filepath.Join("internal", "modules"))
	if err == nil || !strings.Contains(err.Error(), "contains a symlink") {
		t.Fatalf("validateLocalDirectoryPath() error = %v, want symlink error", err)
	}
}
