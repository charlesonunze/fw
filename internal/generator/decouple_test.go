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
		{name: "http", transport: "http", want: "fw.WithTransport(fwhttp.New(fwhttp.Config{", doNotWant: "fwgrpc", assertion: "var _ fwhttp.Module = module"},
		{name: "grpc", transport: "grpc", want: "fw.WithTransport(fwgrpc.New(fwgrpc.Config{", doNotWant: "fwrouter", assertion: "var _ fwgrpc.Module = module"},
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
			name: "pointer constructor and pointer receiver",
			source: `package user
import "google.golang.org/grpc"
type Module struct{}
func New() *Module { return &Module{} }
func (m *Module) RegisterGRPC(*grpc.Server) {}
`,
			want: true,
		},
		{
			name: "pointer constructor and value receiver",
			source: `package user
import "google.golang.org/grpc"
type Module struct{}
func New() *Module { return &Module{} }
func (m Module) RegisterGRPC(*grpc.Server) {}
`,
			want: true,
		},
		{
			name: "value constructor and value receiver",
			source: `package user
import "google.golang.org/grpc"
type Module struct{}
func New() Module { return Module{} }
func (m Module) RegisterGRPC(*grpc.Server) {}
`,
			want: true,
		},
		{
			name: "value constructor and pointer receiver",
			source: `package user
import "google.golang.org/grpc"
type Module struct{}
func New() Module { return Module{} }
func (m *Module) RegisterGRPC(*grpc.Server) {}
`,
		},
		{
			name: "double pointer constructor has no module method set",
			source: `package user
import "google.golang.org/grpc"
type Module struct{}
func New() **Module { module := &Module{}; return &module }
func (m *Module) RegisterGRPC(*grpc.Server) {}
`,
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

func TestDecoupleModuleRejectsInvalidPortBeforeCreatingOutput(t *testing.T) {
	t.Chdir(t.TempDir())

	err := DecoupleModule("user", "example.com/app", "output", ":8080\nEXPOSE 22", "http", routerChi, "")
	if err == nil || !strings.Contains(err.Error(), "invalid listen address") {
		t.Fatalf("DecoupleModule() error = %v, want invalid address error", err)
	}
	if _, statErr := os.Lstat("output"); !os.IsNotExist(statErr) {
		t.Fatalf("output created for invalid port; stat error = %v", statErr)
	}
}

func TestDecoupleModuleRejectsTransportNotImplementedByModule(t *testing.T) {
	t.Chdir(t.TempDir())
	writeFixture(t, "go.mod", "module example.com/app\n\ngo 1.25.2\n")
	if err := NewModule("user", "example.com/app"); err != nil {
		t.Fatalf("NewModule() error = %v", err)
	}

	err := DecoupleModule("user", "example.com/app", "output", ":9090", "grpc", routerChi, "")
	if err == nil || !strings.Contains(err.Error(), "does not implement fwgrpc.Module") {
		t.Fatalf("DecoupleModule() error = %v, want missing fwgrpc.Module error", err)
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

func TestDetectDepsRejectsUnsafeModuleName(t *testing.T) {
	t.Chdir(t.TempDir())

	writeFixture(t, filepath.Join("internal", "modules", "order", "order_service.go"), `package order

import _ "example.com/app/internal/modules/../outside"
`)

	_, err := detectDeps("order", "example.com/app")
	if err == nil || !strings.Contains(err.Error(), "invalid module dependency") {
		t.Fatalf("detectDeps() error = %v, want invalid dependency error", err)
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

func TestRestructureModuleOnlyRewritesImports(t *testing.T) {
	t.Chdir(t.TempDir())

	source := filepath.Join("internal", "modules", "order", "order_service.go")
	writeFixture(t, source, `package order

import _ "example.com/app/internal/modules/order/pb"

const documentation = "example.com/app/internal/modules/order/pb"
`)

	output := filepath.Join("microservices", "order")
	if err := restructureModule("order", "example.com/app", "order-service", output); err != nil {
		t.Fatalf("restructureModule() error = %v", err)
	}
	content, err := os.ReadFile(filepath.Join(output, "internal", "order_service.go"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !strings.Contains(string(content), `import _ "order-service/internal/pb"`) {
		t.Errorf("generated import was not rewritten:\n%s", content)
	}
	if !strings.Contains(string(content), `const documentation = "example.com/app/internal/modules/order/pb"`) {
		t.Errorf("non-import string was unexpectedly rewritten:\n%s", content)
	}
}

func TestDecoupleModuleRejectsOutputInsideSource(t *testing.T) {
	t.Chdir(t.TempDir())
	writeFixture(t, "go.mod", "module example.com/app\n\ngo 1.25.2\n")
	if err := NewModule("user", "example.com/app"); err != nil {
		t.Fatalf("NewModule() error = %v", err)
	}

	output := filepath.Join("internal", "modules", "user", "standalone")
	err := DecoupleModule("user", "example.com/app", output, ":8080", "http", routerChi, "")
	if err == nil || !strings.Contains(err.Error(), "inside source") {
		t.Fatalf("DecoupleModule() error = %v, want source containment error", err)
	}
	if _, statErr := os.Lstat(output); !os.IsNotExist(statErr) {
		t.Fatalf("nested output was created; stat error = %v", statErr)
	}
}

func TestDecoupleModuleRollsBackFailedGeneration(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("PATH", t.TempDir())
	writeFixture(t, "go.mod", "module example.com/app\n\ngo 1.25.2\n")
	if err := NewModule("user", "example.com/app"); err != nil {
		t.Fatalf("NewModule() error = %v", err)
	}

	err := DecoupleModule("user", "example.com/app", "output", ":8080", "http", routerChi, frameworkRoot(t))
	if err == nil || !strings.Contains(err.Error(), "add local module replacements") {
		t.Fatalf("DecoupleModule() error = %v, want go mod edit error", err)
	}
	if _, statErr := os.Lstat("output"); !os.IsNotExist(statErr) {
		t.Fatalf("incomplete output was not removed; stat error = %v", statErr)
	}
}

func TestGRPCClientTemplateRequiresCredentialsAndClosesConnection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "client.go")
	data := clientData{DepName: "user", DepPascal: "User", Methods: []string{"GetByID"}}
	if err := writeTemplate(path, clientGRPCTmpl, data); err != nil {
		t.Fatalf("writeTemplate() error = %v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	for _, want := range []string{
		"transportCredentials credentials.TransportCredentials",
		"options ...grpc.DialOption",
		"func (c *GRPCClient) Close() error",
	} {
		if !strings.Contains(string(content), want) {
			t.Errorf("generated gRPC client missing %q:\n%s", want, content)
		}
	}
	if strings.Contains(string(content), "credentials/insecure") {
		t.Errorf("generated gRPC client hardcodes insecure credentials:\n%s", content)
	}
	if _, err := parser.ParseFile(token.NewFileSet(), path, content, parser.AllErrors); err != nil {
		t.Errorf("generated gRPC client is invalid Go: %v\n%s", err, content)
	}
}

func TestGRPCClientTemplateCompilesWithAndWithoutMethods(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping generated gRPC client compilation in short mode")
	}
	t.Setenv("GOWORK", "off")

	for _, methods := range [][]string{nil, {"GetByID"}} {
		workspace := t.TempDir()
		writeFixture(t, filepath.Join(workspace, "go.mod"), `module example.com/client

go 1.25.2

require google.golang.org/grpc v1.83.2
`)
		if err := writeTemplate(
			filepath.Join(workspace, "client.go"),
			clientGRPCTmpl,
			clientData{DepName: "user", DepPascal: "User", Methods: methods},
		); err != nil {
			t.Fatalf("writeTemplate() error = %v", err)
		}
		if err := runGo(workspace, "mod", "tidy"); err != nil {
			t.Fatalf("tidy generated gRPC client module: %v", err)
		}
		if err := runGo(workspace, "test", "./..."); err != nil {
			t.Fatalf("generated gRPC client with methods %v does not compile: %v", methods, err)
		}
	}
}

func TestWriteGoModDoesNotPinUnreleasedFrameworkVersion(t *testing.T) {
	output := t.TempDir()
	if err := writeGoMod(output, "user-service", "1.25.2"); err != nil {
		t.Fatalf("writeGoMod() error = %v", err)
	}
	content, err := os.ReadFile(filepath.Join(output, "go.mod"))
	if err != nil {
		t.Fatalf("ReadFile(go.mod) error = %v", err)
	}
	if strings.Contains(string(content), "v0.0.0") {
		t.Errorf("generated go.mod pins v0.0.0 without a replacement:\n%s", content)
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
