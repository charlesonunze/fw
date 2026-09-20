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

	if err := NewModule("user", "example.com/app", ModuleConfig{}); err != nil {
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
		`const Name fw.ModuleName = "user"`,
		"func (m *Module) Imports() []fw.ModuleName",
		"service Service",
		"func (m *Module) Register(deps *fw.Deps) error",
		"fw.As[Service]()",
		"func (m *Module) Init(_ context.Context, _ *fw.Deps) error",
		"func (m *Module) RegisterRoutes(r fwhttp.Router)",
	} {
		if !strings.Contains(string(wiring), declaration) {
			t.Errorf("generated module missing %q:\n%s", declaration, wiring)
		}
	}

	service, err := os.ReadFile(filepath.Join(base, "user_service.go"))
	if err != nil {
		t.Fatalf("ReadFile(user_service.go) error = %v", err)
	}
	for _, declaration := range []string{
		"type Service interface",
		"fw.Service",
		"type service struct",
		"func NewService(repo Repository) *service",
		"var _ Service = (*service)(nil)",
	} {
		if !strings.Contains(string(service), declaration) {
			t.Errorf("generated service missing %q:\n%s", declaration, service)
		}
	}

	handler, err := os.ReadFile(filepath.Join(base, "user_http.go"))
	if err != nil {
		t.Fatalf("ReadFile(user_http.go) error = %v", err)
	}
	if !strings.Contains(string(handler), "service Service") {
		t.Errorf("generated HTTP handler does not depend on Service interface:\n%s", handler)
	}
}

func TestNewModuleCreatesTransportlessModule(t *testing.T) {
	t.Chdir(t.TempDir())

	if err := NewModule("worker", "example.com/app", ModuleConfig{Transport: ModuleTransportNone}); err != nil {
		t.Fatalf("NewModule() error = %v", err)
	}

	base := filepath.Join("internal", "modules", "worker")
	entries, err := os.ReadDir(base)
	if err != nil {
		t.Fatalf("ReadDir(%q) error = %v", base, err)
	}
	got := make([]string, 0, len(entries))
	for _, entry := range entries {
		got = append(got, entry.Name())
	}
	slices.Sort(got)
	want := []string{
		"worker_model.go",
		"worker_module.go",
		"worker_repository.go",
		"worker_repository_memory.go",
		"worker_service.go",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("generated files = %v, want %v", got, want)
	}

	wiring, err := os.ReadFile(filepath.Join(base, "worker_module.go"))
	if err != nil {
		t.Fatalf("ReadFile(worker_module.go) error = %v", err)
	}
	for _, unexpected := range []string{"RegisterRoutes", "RegisterGRPC", "fwhttp", "grpc.Server", "handler"} {
		if strings.Contains(string(wiring), unexpected) {
			t.Errorf("transportless module contains %q:\n%s", unexpected, wiring)
		}
	}
}

func TestNewModuleCreatesGRPCModule(t *testing.T) {
	if err := checkProtoTools(); err != nil {
		t.Skip(err)
	}
	t.Chdir(t.TempDir())

	if err := NewModule("user", "example.com/app", ModuleConfig{Transport: ModuleTransportGRPC}); err != nil {
		t.Fatalf("NewModule() error = %v", err)
	}

	for _, path := range []string{
		filepath.Join("proto", "user.proto"),
		filepath.Join("internal", "modules", "user", "user_grpc.go"),
		filepath.Join("internal", "modules", "user", "pb", "user.pb.go"),
		filepath.Join("internal", "modules", "user", "pb", "user_grpc.pb.go"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("generated file %q: %v", path, err)
		}
	}
	if _, err := os.Stat(filepath.Join("internal", "modules", "user", "user_http.go")); !os.IsNotExist(err) {
		t.Fatalf("HTTP handler generated for gRPC module; stat error = %v", err)
	}

	wiring, err := os.ReadFile(filepath.Join("internal", "modules", "user", "user_module.go"))
	if err != nil {
		t.Fatalf("ReadFile(user_module.go) error = %v", err)
	}
	if !strings.Contains(string(wiring), "handler *GRPCHandler") || strings.Contains(string(wiring), "fwhttp") {
		t.Errorf("generated gRPC module wiring is incorrect:\n%s", wiring)
	}

	handler, err := os.ReadFile(filepath.Join("internal", "modules", "user", "user_grpc.go"))
	if err != nil {
		t.Fatalf("ReadFile(user_grpc.go) error = %v", err)
	}
	for _, declaration := range []string{
		"type GRPCHandler struct",
		"func NewGRPCHandler(service Service) *GRPCHandler",
		"func (m *Module) RegisterGRPC(server *grpc.Server)",
	} {
		if !strings.Contains(string(handler), declaration) {
			t.Errorf("generated gRPC handler missing %q:\n%s", declaration, handler)
		}
	}
}

func TestNewModuleRollsBackGRPCModuleWhenProtoExists(t *testing.T) {
	if err := checkProtoTools(); err != nil {
		t.Skip(err)
	}
	t.Chdir(t.TempDir())
	protoPath := filepath.Join("proto", "user.proto")
	writeFixture(t, protoPath, "existing")

	err := NewModule("user", "example.com/app", ModuleConfig{Transport: ModuleTransportGRPC})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("NewModule() error = %v, want existing proto error", err)
	}
	if _, err := os.Lstat(filepath.Join("internal", "modules", "user")); !os.IsNotExist(err) {
		t.Fatalf("incomplete gRPC module was not removed; stat error = %v", err)
	}
	content, err := os.ReadFile(protoPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", protoPath, err)
	}
	if string(content) != "existing" {
		t.Fatalf("existing proto content = %q, want preserved", content)
	}
}

func TestNewModuleGRPCChecksToolsBeforeCreatingFiles(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("PATH", t.TempDir())

	err := NewModule("user", "example.com/app", ModuleConfig{Transport: ModuleTransportGRPC})
	if err == nil || !strings.Contains(err.Error(), "missing protobuf tools") {
		t.Fatalf("NewModule() error = %v, want missing tools error", err)
	}
	if _, err := os.Lstat("internal"); !os.IsNotExist(err) {
		t.Fatalf("module directories created without protobuf tools; stat error = %v", err)
	}
}

func TestModuleTransportValidation(t *testing.T) {
	tests := []struct {
		name      string
		transport string
		want      string
		wantErr   bool
	}{
		{name: "default", want: ModuleTransportHTTP},
		{name: "http", transport: ModuleTransportHTTP, want: ModuleTransportHTTP},
		{name: "grpc", transport: ModuleTransportGRPC, want: ModuleTransportGRPC},
		{name: "none", transport: ModuleTransportNone, want: ModuleTransportNone},
		{name: "unsupported", transport: "graphql", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := moduleTransport(tt.transport)
			if tt.wantErr {
				if err == nil || !strings.Contains(err.Error(), "unsupported module transport") {
					t.Fatalf("moduleTransport() error = %v, want unsupported transport error", err)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("moduleTransport() = %q, %v; want %q", got, err, tt.want)
			}
		})
	}
}

func TestNewModuleRejectsExistingModule(t *testing.T) {
	t.Chdir(t.TempDir())

	if err := NewModule("user", "example.com/app", ModuleConfig{}); err != nil {
		t.Fatalf("first NewModule() error = %v", err)
	}
	if err := NewModule("user", "example.com/app", ModuleConfig{}); err == nil {
		t.Fatal("second NewModule() error = nil, want existing module error")
	}
}

func TestNewModuleRejectsUnsafeInputBeforeCreatingFiles(t *testing.T) {
	workspace := t.TempDir()
	t.Chdir(workspace)

	if err := NewModule("../user", "example.com/app", ModuleConfig{}); err == nil {
		t.Fatal("NewModule() error = nil, want invalid module name error")
	}
	if _, err := os.Lstat("internal"); !os.IsNotExist(err) {
		t.Fatalf("module directories created for invalid name; stat error = %v", err)
	}

	if err := NewModule("user", "example.com/app\nreplace evil.invalid => /tmp", ModuleConfig{}); err == nil {
		t.Fatal("NewModule() error = nil, want invalid module path error")
	}
	if _, err := os.Lstat("internal"); !os.IsNotExist(err) {
		t.Fatalf("module directories created for invalid module path; stat error = %v", err)
	}

	if err := NewModule("user", "example.com/app", ModuleConfig{Transport: "graphql"}); err == nil {
		t.Fatal("NewModule() error = nil, want invalid transport error")
	}
	if _, err := os.Lstat("internal"); !os.IsNotExist(err) {
		t.Fatalf("module directories created for invalid transport; stat error = %v", err)
	}
}
