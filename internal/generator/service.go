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
	fmt.Printf("  if err := app.RegisterService(%sService, fw.As[%s.Service]()); err != nil {\n", name, name)
	fmt.Printf("    log.Fatal(err)\n  }\n\n")

	return nil
}

var serviceTmpl = `package {{ .Name }}

import (
	"context"

	"github.com/charlesonunze/fw"
)

// Service is the application-wide {{ .Name }} service contract.
type Service interface {
	fw.Service
}

type service struct{}

// New creates a {{ .Name }} service.
func New() Service {
	return &service{}
}

// Name returns the service registry key.
func (*service) Name() string { return "{{ .Name }}" }

// Health reports whether the service is ready.
func (*service) Health(context.Context) error { return nil }

// Close releases resources owned by the service.
func (*service) Close() error { return nil }

var _ Service = (*service)(nil)
`
