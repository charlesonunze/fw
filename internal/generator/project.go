package generator

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

type projectData struct {
	ProjectName string
	ModulePath  string
}

// NewProject scaffolds a new project directory with go.mod and main.go.
// If localFwPath is non-empty, a replace directive is added to go.mod for local development.
func NewProject(name, modulePath, localFwPath string) error {
	if _, err := os.Stat(name); err == nil {
		return fmt.Errorf("directory %q already exists", name)
	}

	data := projectData{
		ProjectName: name,
		ModulePath:  modulePath,
	}

	files := map[string]string{
		filepath.Join(name, "cmd", "main.go"): projectMainTmpl,
	}

	for path, tmpl := range files {
		fmt.Printf("  create %s\n", path)
		if err := writeTemplate(path, tmpl, data); err != nil {
			return err
		}
	}

	// Initialize go module
	fmt.Printf("  init   go mod\n")
	cmd := exec.Command("go", "mod", "init", modulePath)
	cmd.Dir = name
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go mod init failed: %w", err)
	}

	// Add replace directive for local development
	if localFwPath != "" {
		fmt.Printf("  edit   go mod (local replace)\n")
		cmd = exec.Command("go", "mod", "edit",
			"-require=github.com/charlesonunze/fw@v0.0.0",
			fmt.Sprintf("-replace=github.com/charlesonunze/fw=%s", localFwPath),
		)
		cmd.Dir = name
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("go mod edit failed: %w", err)
		}
	}

	// Run go mod tidy to fetch dependencies
	fmt.Printf("  tidy   go mod\n")
	cmd = exec.Command("go", "mod", "tidy")
	cmd.Dir = name
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go mod tidy failed: %w", err)
	}

	fmt.Printf("\nProject %q created successfully!\n", name)
	fmt.Printf("\n  cd %s\n  fw generate module <name>\n  go run ./cmd/main.go\n\n", name)

	return nil
}

var projectMainTmpl = `package main

import (
	"log"

	"github.com/charlesonunze/fw"
)

func main() {
	app := fw.New(
		fw.WithAddr(":8080"),
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
