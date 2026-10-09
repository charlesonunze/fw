package generator

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDecoupleModuleIncludesGeneratedServiceMethods(t *testing.T) {
	for _, transport := range []string{"http", "grpc"} {
		t.Run(transport, func(t *testing.T) {
			t.Chdir(t.TempDir())
			writeFixture(t, "go.mod", "module example.com/app\n\ngo 1.26.9\n")
			for _, name := range []string{"user", "order"} {
				if err := NewModule(name, "example.com/app", ModuleConfig{Router: routerChi}); err != nil {
					t.Fatal(err)
				}
			}
			writeFixture(t, "internal/modules/order/order_dependencies.go", `package order

import "example.com/app/internal/modules/user"

var users user.Service
`)
			if transport == "grpc" {
				writeFixture(t, "internal/modules/order/order_grpc.go", `package order

import "google.golang.org/grpc"

func (*Module) RegisterGRPC(*grpc.Server) {}
`)
			}
			if err := DecoupleModule("order", "example.com/app", "output", ":8081", transport, routerChi, ""); err != nil {
				t.Fatal(err)
			}
			content, err := os.ReadFile("output/internal/clients/user/client.go")
			if err != nil {
				t.Fatal(err)
			}
			for _, method := range []string{"Create", "GetByID"} {
				if !strings.Contains(string(content), method+"(ctx context.Context) error") {
					t.Errorf("client scaffold missing %s method:\n%s", method, content)
				}
			}
			for path, want := range map[string]string{
				"go.mod":     "go 1.26.9",
				"Dockerfile": "FROM golang:1.26.9-alpine",
			} {
				content, err := os.ReadFile(filepath.Join("output", path))
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(string(content), want) {
					t.Errorf("%s did not preserve the source Go version:\n%s", path, content)
				}
			}
		})
	}
}

func TestModuleSupportsTransportIgnoresNestedPackages(t *testing.T) {
	transports := []struct {
		name, transport, router, declaration string
	}{
		{"chi", "http", routerChi, "import \"github.com/go-chi/chi/v5\"\nfunc (*Module) RegisterRoutes(chi.Router) {}"},
		{"gin", "http", routerGin, "import \"github.com/gin-gonic/gin\"\nfunc (*Module) RegisterRoutes(gin.IRouter) {}"},
		{"grpc", "grpc", "", "import \"google.golang.org/grpc\"\nfunc (*Module) RegisterGRPC(*grpc.Server) {}"},
	}
	for _, transport := range transports {
		for _, rootSupports := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/root-supports-%t", transport.name, rootSupports), func(t *testing.T) {
				t.Chdir(t.TempDir())
				writeFixture(t, "internal/modules/user/user_module.go", `package user
type Module struct{}
func New() *Module { return &Module{} }
`)
				if rootSupports {
					writeFixture(t, "internal/modules/user/user_transport.go", "package user\n"+transport.declaration)
					writeFixture(t, "internal/modules/user/zzhelper/helper.go", `package helper
type Helper struct{}
func New() *Helper { return &Helper{} }
`)
				} else {
					writeFixture(t, "internal/modules/user/zzhelper/helper.go", "package helper\n"+transport.declaration+`
type Module struct{}
func New() *Module { return &Module{} }
`)
				}
				got, err := moduleSupportsTransport("user", transport.transport, transport.router)
				if err != nil {
					t.Fatal(err)
				}
				if got != rootSupports {
					t.Fatalf("moduleSupportsTransport() = %t, want %t", got, rootSupports)
				}
			})
		}
	}
}

func TestRestructureModulePreservesAssets(t *testing.T) {
	t.Chdir(t.TempDir())
	files := map[string]string{
		"user.go":                 "package user\n",
		"templates/welcome.html":  "<h1>Welcome</h1>\n",
		"migrations/001_init.sql": "CREATE TABLE users (id text);\n",
		"testdata/payload.bin":    "\x00\x01\xff",
	}
	for path, content := range files {
		path := filepath.Join("internal/modules/user", path)
		writeFixture(t, path, content)
		if err := os.Chmod(path, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := restructureModule("user", "example.com/app", "user-service", "output"); err != nil {
		t.Fatal(err)
	}
	for path, want := range files {
		path := filepath.Join("output/internal", path)
		got, err := os.ReadFile(path)
		if err != nil {
			t.Error(err)
			continue
		}
		if string(got) != want {
			t.Errorf("%s content = %q, want %q", path, got, want)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("%s permissions = %o, want 600", path, info.Mode().Perm())
		}
	}
}

func TestDecoupleModuleRejectsSymlinkedAssets(t *testing.T) {
	tests := []struct {
		name, target, link string
	}{
		{"asset", "outside.txt", "linked.txt"},
		{"directory", "outside", "linked"},
		{"go file", "outside.txt", "linked.go"},
		{"nested go file", "outside.txt", "assets/linked.go"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			writeFixture(t, "go.mod", "module example.com/app\n\ngo 1.26.9\n")
			if err := NewModule("user", "example.com/app", ModuleConfig{Router: routerChi}); err != nil {
				t.Fatal(err)
			}
			writeFixture(t, "outside.txt", "do not copy")
			writeFixture(t, "outside/data.txt", "do not copy")
			link := filepath.Join("internal/modules/user", tt.link)
			if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
				t.Fatal(err)
			}
			target, err := filepath.Abs(tt.target)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, link); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			err = DecoupleModule("user", "example.com/app", "output", ":8081", "http", routerChi, "")
			if err == nil || !strings.Contains(err.Error(), "non-regular") {
				t.Fatalf("error = %v, want non-regular file error", err)
			}
			if _, err := os.Stat("output"); !os.IsNotExist(err) {
				t.Fatalf("incomplete output was not removed: %v", err)
			}
		})
	}
}

func TestDecoupleModuleRejectsInvalidGoMetadata(t *testing.T) {
	tests := []struct {
		name, content, want string
	}{
		{"missing file", "", "read go.mod"},
		{"missing version", "module example.com/app\n", "go directive"},
		{"invalid version", "module example.com/app\ngo invalid\n", "parse go.mod"},
		{"malformed require", "module example.com/app\ngo 1.26.9\nrequire (\n", "parse go.mod"},
		{"unknown directive", "module example.com/app\ngo 1.26.9\nunknown value\n", "parse go.mod"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			if tt.content != "" {
				writeFixture(t, "go.mod", tt.content)
			}
			if err := NewModule("user", "example.com/app", ModuleConfig{Router: routerChi}); err != nil {
				t.Fatal(err)
			}
			err := DecoupleModule("user", "example.com/app", "output", ":8081", "http", routerChi, "")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
			if _, err := os.Stat("output"); !os.IsNotExist(err) {
				t.Fatalf("created output with invalid metadata: %v", err)
			}
		})
	}
}

func writeDecoupleAssetFixture(t *testing.T) {
	t.Helper()
	writeFixture(t, "internal/modules/user/user_assets.go", `package user

import _ "embed"

//go:embed templates/welcome.html
var welcome string
`)
	writeFixture(t, "internal/modules/user/templates/welcome.html", "welcome\n")
	writeFixture(t, "internal/modules/user/zzhelper/helper.go", `package helper

import _ "embed"

//go:embed label.txt
var label string

type Helper struct { Label string }

func New() *Helper { return &Helper{Label: label} }
`)
	writeFixture(t, "internal/modules/user/zzhelper/label.txt", "welcome\n")
	writeFixture(t, "internal/modules/user/user_assets_test.go", `package user

import (
	"testing"
	"example.com/app/internal/modules/user/zzhelper"
)

func TestEmbeddedAssets(t *testing.T) {
	if welcome != "welcome\n" {
		t.Fatalf("welcome = %q", welcome)
	}
	if got := helper.New().Label; got != welcome {
		t.Fatalf("nested asset = %q, want %q", got, welcome)
	}
}
`)
}
