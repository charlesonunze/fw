package generator

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestDetectDepsSupportsFlatModules(t *testing.T) {
	t.Chdir(t.TempDir())

	writeFixture(t, filepath.Join("internal", "modules", "order", "order_service.go"), `package order

import (
	"example.com/app/internal/modules/inventory"
	"example.com/app/internal/modules/user"
)
`)

	got, err := detectDeps("order", "example.com/app")
	if err != nil {
		t.Fatalf("detectDeps() error = %v", err)
	}
	want := []string{"inventory", "user"}
	if !slices.Equal(got, want) {
		t.Errorf("detectDeps() = %v, want %v", got, want)
	}
}

func TestRestructureModuleSupportsFlatLayout(t *testing.T) {
	t.Chdir(t.TempDir())

	source := filepath.Join("internal", "modules", "order", "order_grpc.go")
	writeFixture(t, source, `package order

import "example.com/app/internal/modules/order/pb"
`)

	output := filepath.Join("microservices", "order")
	if err := restructureModule("order", "example.com/app", "order-service", output); err != nil {
		t.Fatalf("restructureModule() error = %v", err)
	}

	generated := filepath.Join(output, "internal", "order_grpc.go")
	content, err := os.ReadFile(generated)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", generated, err)
	}
	if !strings.Contains(string(content), `"order-service/internal/pb"`) {
		t.Errorf("generated import was not rewritten:\\n%s", content)
	}
}

func TestExtractMethodsExcludesServiceLifecycle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "user_service.go")
	writeFixture(t, path, `package user

type Service struct{}

func (s *Service) Name() string { return "user.service" }
func (s *Service) Close() error { return nil }
func (s *Service) GetByID() {}
func (s *Service) create() {}
`)

	got, err := extractMethods(path)
	if err != nil {
		t.Fatalf("extractMethods() error = %v", err)
	}
	want := []string{"GetByID"}
	if !slices.Equal(got, want) {
		t.Errorf("extractMethods() = %v, want %v", got, want)
	}
}

func TestDecoupledFlatModuleCompiles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping decoupled project compilation in short mode")
	}
	t.Setenv("GOWORK", "off")
	t.Chdir(t.TempDir())

	writeFixture(t, "go.mod", "module example.com/app\n\ngo 1.25.2\n")
	if err := NewModule("user", "example.com/app"); err != nil {
		t.Fatalf("NewModule() error = %v", err)
	}

	output := filepath.Join("microservices", "user")
	if err := DecoupleModule(
		"user",
		"example.com/app",
		output,
		":8081",
		"http",
		routerChi,
		frameworkRoot(t),
	); err != nil {
		t.Fatalf("DecoupleModule() error = %v", err)
	}
	if err := runGo(output, "mod", "tidy"); err != nil {
		t.Fatalf("tidy decoupled module: %v", err)
	}
	if err := runGo(output, "test", "./..."); err != nil {
		t.Fatalf("decoupled module does not compile: %v", err)
	}
}

func writeFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}
