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

func TestDecoupleMainTemplateUsesSelectedTransport(t *testing.T) {
	tests := []struct {
		name      string
		transport string
		want      string
		doNotWant string
		assertion string
	}{
		{name: "http", transport: "http", want: "fw.WithHTTP(fw.HTTPConfig{", doNotWant: "fw.WithGRPC(", assertion: "var _ fw.HTTPModule = module"},
		{name: "grpc", transport: "grpc", want: "fw.WithGRPC(fw.GRPCConfig{", doNotWant: "fwrouter", assertion: "var _ fw.GRPCModule = module"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "main.go")
			data := decoupleData{
				Name: "user", ModuleName: "user-service", Port: ":8081",
				Router: routerChi, Transport: tt.transport,
			}
			if err := writeTemplate(path, decoupleCmdTmpl, data); err != nil {
				t.Fatalf("writeTemplate() error = %v", err)
			}
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile(%q) error = %v", path, err)
			}
			if !strings.Contains(string(content), tt.want) {
				t.Errorf("generated main missing %q:\n%s", tt.want, content)
			}
			if strings.Contains(string(content), tt.doNotWant) {
				t.Errorf("generated main unexpectedly contains %q:\n%s", tt.doNotWant, content)
			}
			if !strings.Contains(string(content), tt.assertion) {
				t.Errorf("generated main missing interface assertion %q:\n%s", tt.assertion, content)
			}
			if _, err := parser.ParseFile(token.NewFileSet(), path, content, parser.AllErrors); err != nil {
				t.Errorf("generated main is invalid Go: %v\n%s", err, content)
			}
		})
	}
}

func TestModuleSupportsTransportValidatesNewReturnTypeAndSignature(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   bool
	}{
		{
			name: "valid",
			source: `package user
import "google.golang.org/grpc"
type Module struct{}
func New() *Module { return &Module{} }
func (m *Module) RegisterGRPC(*grpc.Server) {}
`,
			want: true,
		},
		{
			name: "wrong receiver",
			source: `package user
import "google.golang.org/grpc"
type Module struct{}
type helper struct{}
func New() *Module { return &Module{} }
func (h *helper) RegisterGRPC(*grpc.Server) {}
`,
		},
		{
			name: "wrong signature",
			source: `package user
type Module struct{}
func New() *Module { return &Module{} }
func (m *Module) RegisterGRPC(string) {}
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			writeFixture(t, filepath.Join("internal", "modules", "user", "user_module.go"), tt.source)
			got, err := moduleSupportsTransport("user", "grpc")
			if err != nil {
				t.Fatalf("moduleSupportsTransport() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("moduleSupportsTransport() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDecoupleModuleRejectsInvalidTransport(t *testing.T) {
	err := DecoupleModule("user", "example.com/app", "output", ":8080", "htttp", routerChi, "")
	if err == nil || !strings.Contains(err.Error(), "unsupported transport") {
		t.Fatalf("DecoupleModule() error = %v, want unsupported transport error", err)
	}
}

func TestDecoupleModuleRejectsTransportNotImplementedByModule(t *testing.T) {
	t.Chdir(t.TempDir())
	writeFixture(t, "go.mod", "module example.com/app\n\ngo 1.25.2\n")
	if err := NewModule("user", "example.com/app"); err != nil {
		t.Fatalf("NewModule() error = %v", err)
	}

	err := DecoupleModule("user", "example.com/app", "output", ":9090", "grpc", routerChi, "")
	if err == nil || !strings.Contains(err.Error(), "does not implement fw.GRPCModule") {
		t.Fatalf("DecoupleModule() error = %v, want missing GRPCModule error", err)
	}
}

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
	assertDevelopmentFiles(t, output)
	if err := runGo(output, "mod", "tidy"); err != nil {
		t.Fatalf("tidy decoupled module: %v", err)
	}
	if err := runGo(output, "test", "./..."); err != nil {
		t.Fatalf("decoupled module does not compile: %v", err)
	}
}

func TestDecoupledGRPCModuleNeedsNoRouterAdapter(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping decoupled project compilation in short mode")
	}
	t.Setenv("GOWORK", "off")
	t.Chdir(t.TempDir())

	writeFixture(t, "go.mod", "module example.com/app\n\ngo 1.25.2\n")
	if err := NewModule("user", "example.com/app"); err != nil {
		t.Fatalf("NewModule() error = %v", err)
	}
	writeFixture(t, filepath.Join("internal", "modules", "user", "user_grpc.go"), `package user

import "google.golang.org/grpc"

func (m *Module) RegisterGRPC(*grpc.Server) {}
`)

	output := filepath.Join("microservices", "user-grpc")
	if err := DecoupleModule(
		"user",
		"example.com/app",
		output,
		":9090",
		"grpc",
		"unused-router",
		frameworkRoot(t),
	); err != nil {
		t.Fatalf("DecoupleModule() error = %v", err)
	}
	if err := runGo(output, "mod", "tidy"); err != nil {
		t.Fatalf("tidy decoupled gRPC module: %v", err)
	}
	if err := runGo(output, "test", "./..."); err != nil {
		t.Fatalf("decoupled gRPC module does not compile: %v", err)
	}
	goMod, err := os.ReadFile(filepath.Join(output, "go.mod"))
	if err != nil {
		t.Fatalf("ReadFile(go.mod) error = %v", err)
	}
	if strings.Contains(string(goMod), "fw/adapters/") {
		t.Errorf("decoupled gRPC module retained router adapter:\n%s", goMod)
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
