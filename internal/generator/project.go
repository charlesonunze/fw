package generator

import (
	"errors"
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
func NewProject(name, modulePath, router, localFWPath string) error {
	if err := validateRouter(router); err != nil {
		return err
	}
	if _, err := os.Stat(name); err == nil {
		return fmt.Errorf("directory %q already exists", name)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("check project directory %q: %w", name, err)
	}

	data := projectData{
		ProjectName: name,
		ModulePath:  modulePath,
		Router:      router,
	}

	mainPath := filepath.Join(name, "cmd", "main.go")
	fmt.Printf("  create %s\n", mainPath)
	if err := writeTemplate(mainPath, projectMainTmpl, data); err != nil {
		return err
	}
	if err := writeDevelopmentFiles(name); err != nil {
		return err
	}

	fmt.Printf("  init   go mod\n")
	if err := runGo(name, "mod", "init", modulePath); err != nil {
		return fmt.Errorf("initialize go module: %w", err)
	}

	if localFWPath != "" {
		if err := addLocalReplacements(name, router, localFWPath); err != nil {
			return err
		}
	}

	fmt.Printf("  tidy   go mod\n")
	if err := runGo(name, "mod", "tidy"); err != nil {
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
	absFWPath, err := filepath.Abs(localFWPath)
	if err != nil {
		return fmt.Errorf("resolve local fw path: %w", err)
	}
	if _, err := os.Stat(filepath.Join(absFWPath, "go.mod")); err != nil {
		return fmt.Errorf("local fw path %q does not contain go.mod: %w", absFWPath, err)
	}

	fmt.Printf("  edit   go mod (local replacements)\n")
	args := []string{
		"mod", "edit",
		"-require=github.com/charlesonunze/fw@v0.0.0",
		"-replace=github.com/charlesonunze/fw=" + absFWPath,
	}
	if router != "" {
		adapterPath := filepath.Join(absFWPath, "adapters", router)
		if _, err := os.Stat(filepath.Join(adapterPath, "go.mod")); err != nil {
			return fmt.Errorf("local %s adapter path %q does not contain go.mod: %w", router, adapterPath, err)
		}
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

func runGo(dir string, args ...string) error {
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

var projectMainTmpl = `package main

import (
	"log"

	"github.com/charlesonunze/fw"
{{- if eq .Router "chi" }}
	fwrouter "github.com/charlesonunze/fw/adapters/chi"
	"github.com/go-chi/chi/v5"
{{- else }}
	fwrouter "github.com/charlesonunze/fw/adapters/gin"
	"github.com/gin-gonic/gin"
{{- end }}
)

func main() {
{{- if eq .Router "chi" }}
	router := chi.NewRouter()
{{- else }}
	router := gin.New()
{{- end }}
	app := fw.New(
		fw.WithHTTP(fw.HTTPConfig{
			Addr:   ":8080",
			Router: fwrouter.NewRouter(router),
		}),
	)

	// Register your modules here:
	// app.RegisterModules(
	// 	user.New(),
	// )

	if err := app.Start(); err != nil {
		log.Fatal(err)
	}
}
`
