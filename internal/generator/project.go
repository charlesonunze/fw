package generator

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const (
	// DefaultRouter is used when fw new is called without --router.
	DefaultRouter = "chi"

	routerChi = "chi"
	routerGin = "gin"
)

type projectData struct {
	ProjectName string
	ModulePath  string
	Router      string
}

// NewProject scaffolds a runnable project using the selected router adapter.
// If localFWPath is non-empty, replace directives point to the local framework
// and adapter modules.
func NewProject(name, modulePath, router, localFWPath string) (err error) {
	if err := validateProjectName(name); err != nil {
		return err
	}
	if err := validateModulePath(modulePath); err != nil {
		return err
	}
	if err := validateRouter(router); err != nil {
		return err
	}
	if localFWPath != "" {
		if _, err := validateLocalReplacements(router, localFWPath); err != nil {
			return err
		}
	}
	if err := createGeneratedDir(name); err != nil {
		return err
	}
	defer cleanupGeneratedDir(name, &err)

	data := projectData{
		ProjectName: name,
		ModulePath:  modulePath,
		Router:      router,
	}

	mainPath := filepath.Join(name, "cmd", "main.go")
	fmt.Printf("  create %s\n", mainPath)
	if err = writeTemplate(mainPath, projectMainTmpl, data); err != nil {
		return err
	}
	if err = writeDevelopmentFiles(name); err != nil {
		return err
	}

	fmt.Printf("  init   go mod\n")
	if err = runGo(name, "mod", "init", modulePath); err != nil {
		return fmt.Errorf("initialize go module: %w", err)
	}

	if localFWPath != "" {
		if err = addLocalReplacements(name, router, localFWPath); err != nil {
			return err
		}
	}

	fmt.Printf("  tidy   go mod\n")
	if err = runGo(name, "mod", "tidy"); err != nil {
		return fmt.Errorf("tidy go module: %w", err)
	}

	fmt.Printf("\nProject %q created successfully!\n", name)
	fmt.Printf("\n  cd %s\n  fw generate module <name>\n  fw dev\n\n", name)

	return nil
}

func validateRouter(router string) error {
	switch router {
	case routerChi, routerGin:
		return nil
	default:
		return fmt.Errorf("unsupported router %q: use chi or gin", router)
	}
}

func addLocalReplacements(projectDir, router, localFWPath string) error {
	absFWPath, err := validateLocalReplacements(router, localFWPath)
	if err != nil {
		return err
	}

	fmt.Printf("  edit   go mod (local replacements)\n")
	args := []string{
		"mod", "edit",
		"-require=github.com/charlesonunze/fw@v0.0.0",
		"-replace=github.com/charlesonunze/fw=" + absFWPath,
	}
	if router != "" {
		adapterPath := filepath.Join(absFWPath, "adapters", router)
		adapterModule := "github.com/charlesonunze/fw/adapters/" + router
		args = append(args,
			"-require="+adapterModule+"@v0.0.0",
			"-replace="+adapterModule+"="+adapterPath,
		)
	}
	if err := runGo(projectDir, args...); err != nil {
		return fmt.Errorf("add local module replacements: %w", err)
	}
	return nil
}

func validateLocalReplacements(router, localFWPath string) (string, error) {
	absFWPath, err := filepath.Abs(localFWPath)
	if err != nil {
		return "", fmt.Errorf("resolve local fw path: %w", err)
	}
	if info, err := os.Lstat(filepath.Join(absFWPath, "go.mod")); err != nil || !info.Mode().IsRegular() {
		if err == nil {
			err = fmt.Errorf("go.mod is not a regular file")
		}
		return "", fmt.Errorf("local fw path %q does not contain go.mod: %w", absFWPath, err)
	}
	if router != "" {
		adapterPath := filepath.Join(absFWPath, "adapters", router)
		if info, err := os.Lstat(filepath.Join(adapterPath, "go.mod")); err != nil || !info.Mode().IsRegular() {
			if err == nil {
				err = fmt.Errorf("go.mod is not a regular file")
			}
			return "", fmt.Errorf("local %s adapter path %q does not contain go.mod: %w", router, adapterPath, err)
		}
	}
	return absFWPath, nil
}

func runGo(dir string, args ...string) error {
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

var projectMainTmpl = `package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/charlesonunze/fw"
	fwhttp "github.com/charlesonunze/fw/transport/http"
{{- if eq .Router "chi" }}
	fwrouter "github.com/charlesonunze/fw/adapters/chi"
	"github.com/go-chi/chi/v5"
{{- else }}
	fwrouter "github.com/charlesonunze/fw/adapters/gin"
	"github.com/gin-gonic/gin"
{{- end }}
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

{{- if eq .Router "chi" }}
	router := chi.NewRouter()
{{- else }}
	router := gin.New()
{{- end }}
	app := fw.New(
		fw.WithTransport(fwhttp.New(fwhttp.Config{
			Addr:   ":8080",
			Router: fwrouter.NewRouter(router),
		})),
	)

	// Register your modules here:
	// app.RegisterModules(
	// 	user.New(),
	// )

	if err := app.Start(ctx); err != nil {
		log.Fatal(err)
	}
}
`
