package generator

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestProjectMainTemplateSupportsRouters(t *testing.T) {
	tests := []struct {
		name       string
		router     string
		wantImport string
		wantSetup  string
		wantConfig string
	}{
		{
			name:       "chi",
			router:     routerChi,
			wantImport: `"github.com/go-chi/chi/v5"`,
			wantSetup:  "router := chi.NewRouter()",
			wantConfig: "fw.WithTransport(fwhttp.New(fwhttp.Config{",
		},
		{
			name:       "gin",
			router:     routerGin,
			wantImport: `"github.com/gin-gonic/gin"`,
			wantSetup:  "router := gin.New()",
			wantConfig: "fw.WithTransport(fwhttp.New(fwhttp.Config{",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "main.go")
			data := projectData{ProjectName: "app", ModulePath: "example.com/app", Router: tt.router}
			if err := writeTemplate(path, projectMainTmpl, data); err != nil {
				t.Fatalf("writeTemplate() error = %v", err)
			}

			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile(%q) error = %v", path, err)
			}
			if !strings.Contains(string(content), tt.wantImport) {
				t.Errorf("generated main missing import %s:\n%s", tt.wantImport, content)
			}
			if !strings.Contains(string(content), tt.wantSetup) {
				t.Errorf("generated main missing setup %q:\n%s", tt.wantSetup, content)
			}
			if !strings.Contains(string(content), tt.wantConfig) {
				t.Errorf("generated main missing config %q:\n%s", tt.wantConfig, content)
			}
			if _, err := parser.ParseFile(token.NewFileSet(), path, content, parser.AllErrors); err != nil {
				t.Errorf("generated main is invalid Go: %v\n%s", err, content)
			}
		})
	}
}

func TestValidateRouter(t *testing.T) {
	for _, router := range []string{routerChi, routerGin} {
		if err := validateRouter(router); err != nil {
			t.Errorf("validateRouter(%q) error = %v", router, err)
		}
	}
	if err := validateRouter("echo"); err == nil {
		t.Fatal("validateRouter(echo) error = nil, want unsupported router error")
	}
}

func TestNewProjectCompiles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping generated project compilation in short mode")
	}
	t.Setenv("GOWORK", "off")
	root := frameworkRoot(t)

	for _, router := range []string{routerChi, routerGin} {
		t.Run(router, func(t *testing.T) {
			workspace := t.TempDir()
			t.Chdir(workspace)

			project := router + "-app"
			if err := NewProject(project, "example.com/"+project, router, root); err != nil {
				t.Fatalf("NewProject() error = %v", err)
			}

			t.Chdir(filepath.Join(workspace, project))
			assertDevelopmentFiles(t, ".")
			if err := NewModule("user", "example.com/"+project); err != nil {
				t.Fatalf("NewModule() error = %v", err)
			}
			if err := runGo(".", "test", "./..."); err != nil {
				t.Fatalf("generated %s project does not compile: %v", router, err)
			}
		})
	}
}

func frameworkRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not determine generator test path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}
