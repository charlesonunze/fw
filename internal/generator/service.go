package generator

import (
	"fmt"
	"path/filepath"
)

type serviceData struct {
	Name string
}

// NewService generates a standalone application service package.
func NewService(name, modPath string) (err error) {
	if err := validateModuleName(name); err != nil {
		return err
	}
	if err := validateModulePath(modPath); err != nil {
		return err
	}

	data := serviceData{Name: name}

	base := filepath.Join("internal", "services", name)
	if err := validateLocalDirectoryPath(filepath.Dir(base)); err != nil {
		return err
	}
	if err := createGeneratedDir(base); err != nil {
		return err
	}
	defer cleanupGeneratedDir(base, &err)

	path := filepath.Join(base, name+"_service.go")
	fmt.Printf("  create %s\n", path)
	if err = writeTemplate(path, serviceTmpl, data); err != nil {
		return err
	}

	fmt.Printf("\nService %q created at %s\n", name, base)
	fmt.Printf("Register it in cmd/main.go:\n\n")
	fmt.Printf("  import \"%s/internal/services/%s\"\n\n", modPath, name)
	fmt.Printf("  %sService := %s.New()\n", name, name)
	fmt.Printf("  if err := app.RegisterService(%sService); err != nil {\n", name)
	fmt.Printf("    log.Fatal(err)\n  }\n\n")

	return nil
}

var serviceTmpl = `package {{ .Name }}

import "github.com/charlesonunze/fw"

// Service is the application-wide {{ .Name }} service.
type Service struct{}

// New creates a {{ .Name }} service.
func New() *Service {
	return &Service{}
}

// Name returns the service registry key.
func (*Service) Name() string { return "{{ .Name }}" }

// Close releases resources owned by the service.
func (*Service) Close() error { return nil }

var _ fw.Service = (*Service)(nil)
`
