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
			wantConfig: "app := fw.New(fw.Config{",
		},
		{
			name:       "gin",
			router:     routerGin,
			wantImport: `"github.com/gin-gonic/gin"`,
			wantSetup:  "router := gin.New()",
			wantConfig: "app := fw.New(fw.Config{",
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
			assertPortableLocalReplacements(t, project, router, root)

			t.Chdir(filepath.Join(workspace, project))
			assertDevelopmentFiles(t, ".")
			if err := NewService("mailer", "example.com/"+project); err != nil {
				t.Fatalf("NewService() error = %v", err)
			}
			if err := NewModule("user", "example.com/"+project, ModuleConfig{}); err != nil {
				t.Fatalf("NewModule() error = %v", err)
			}
			if err := runGo(".", "test", "./..."); err != nil {
				t.Fatalf("generated %s project does not compile: %v", router, err)
			}
		})
	}
}

func TestRelativeReplacementPath(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "apps", "todo")

	tests := []struct {
		name   string
		target string
		want   string
	}{
		{name: "sibling", target: filepath.Join(root, "apps", "fw"), want: "../fw"},
		{name: "child", target: filepath.Join(project, "local", "fw"), want: "./local/fw"},
		{name: "same directory", target: project, want: "."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := relativeReplacementPath(project, tt.target)
			if err != nil {
				t.Fatalf("relativeReplacementPath() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("relativeReplacementPath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGeneratedAlternativeModuleTransportsCompile(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping generated project compilation in short mode")
	}
	t.Setenv("GOWORK", "off")
	root := frameworkRoot(t)

	for _, transport := range []string{ModuleTransportNone, ModuleTransportGRPC} {
		t.Run(transport, func(t *testing.T) {
			if transport == ModuleTransportGRPC {
				if err := checkProtoTools(); err != nil {
					t.Skip(err)
				}
			}

			workspace := t.TempDir()
			t.Chdir(workspace)
			project := transport + "-app"
			modulePath := "example.com/" + project
			if err := NewProject(project, modulePath, routerChi, root); err != nil {
				t.Fatalf("NewProject() error = %v", err)
			}

			t.Chdir(filepath.Join(workspace, project))
			if err := NewModule("user", modulePath, ModuleConfig{Transport: transport}); err != nil {
				t.Fatalf("NewModule() error = %v", err)
			}
			if err := runGo(".", "mod", "tidy"); err != nil {
				t.Fatalf("tidy generated %s module dependencies: %v", transport, err)
			}
			if err := runGo(".", "test", "./..."); err != nil {
				t.Fatalf("generated %s module does not compile: %v", transport, err)
			}
		})
	}
}

func TestNewProjectRejectsUnsafeInputBeforeCreatingFiles(t *testing.T) {
	workspace := t.TempDir()
	t.Chdir(workspace)

	if err := NewProject("../escape", "example.com/app", routerChi, ""); err == nil {
		t.Fatal("NewProject() error = nil, want invalid project name error")
	}
	if _, err := os.Lstat(filepath.Join(filepath.Dir(workspace), "escape")); !os.IsNotExist(err) {
		t.Fatalf("unsafe project path was created; stat error = %v", err)
	}

	if err := NewProject("todo", "example.com/app\nreplace evil.invalid => /tmp", routerChi, ""); err == nil {
		t.Fatal("NewProject() error = nil, want invalid module path error")
	}
	if _, err := os.Lstat("todo"); !os.IsNotExist(err) {
		t.Fatalf("project was created for invalid module path; stat error = %v", err)
	}
}

func TestNewProjectRollsBackFailedGeneration(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("PATH", t.TempDir())

	err := NewProject("todo", "example.com/todo", routerChi, "")
	if err == nil || !strings.Contains(err.Error(), "initialize go module") {
		t.Fatalf("NewProject() error = %v, want go mod initialization error", err)
	}
	if _, statErr := os.Lstat("todo"); !os.IsNotExist(statErr) {
		t.Fatalf("incomplete project was not removed; stat error = %v", statErr)
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

func assertPortableLocalReplacements(t *testing.T, projectDir, router, fwPath string) {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(projectDir, "go.mod"))
	if err != nil {
		t.Fatalf("ReadFile(go.mod) error = %v", err)
	}
	goMod := string(content)

	relFWPath, err := relativeReplacementPath(projectDir, fwPath)
	if err != nil {
		t.Fatalf("relativeReplacementPath(fw) error = %v", err)
	}
	if filepath.IsAbs(filepath.FromSlash(relFWPath)) {
		t.Fatalf("fw replacement path is absolute: %q", relFWPath)
	}
	wantFW := "replace github.com/charlesonunze/fw => " + relFWPath
	if !strings.Contains(goMod, wantFW) {
		t.Fatalf("go.mod missing relative fw replacement %q:\n%s", wantFW, goMod)
	}

	if router == "" {
		return
	}
	relAdapterPath, err := relativeReplacementPath(projectDir, filepath.Join(fwPath, "adapters", router))
	if err != nil {
		t.Fatalf("relativeReplacementPath(adapter) error = %v", err)
	}
	if filepath.IsAbs(filepath.FromSlash(relAdapterPath)) {
		t.Fatalf("adapter replacement path is absolute: %q", relAdapterPath)
	}
	wantAdapter := "replace github.com/charlesonunze/fw/adapters/" + router + " => " + relAdapterPath
	if !strings.Contains(goMod, wantAdapter) {
		t.Fatalf("go.mod missing relative adapter replacement %q:\n%s", wantAdapter, goMod)
	}
}
