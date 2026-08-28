package generator

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type moduleData struct {
	Name       string // lowercase, e.g. "user"
	Pascal     string // PascalCase, e.g. "User"
	ModulePath string // go module path, e.g. "github.com/you/myapp"
}

// NewModule generates a flat, self-contained module package.
func NewModule(name, modPath string) error {
	data := moduleData{
		Name:       name,
		Pascal:     pascal(name),
		ModulePath: modPath,
	}

	base := filepath.Join("internal", "modules", name)
	if _, err := os.Stat(base); err == nil {
		return fmt.Errorf("module %q already exists at %s", name, base)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("check module path %s: %w", base, err)
	}

	files := []struct {
		path string
		tmpl string
	}{
		{filepath.Join(base, name+"_module.go"), moduleWiringTmpl},
		{filepath.Join(base, name+"_model.go"), moduleModelTmpl},
		{filepath.Join(base, name+"_service.go"), moduleServiceTmpl},
		{filepath.Join(base, name+"_repository.go"), moduleRepositoryTmpl},
		{filepath.Join(base, name+"_repository_memory.go"), moduleMemoryRepositoryTmpl},
		{filepath.Join(base, name+"_http.go"), moduleHTTPTmpl},
	}

	for _, file := range files {
		fmt.Printf("  create %s\n", file.path)
		if err := writeTemplate(file.path, file.tmpl, data); err != nil {
			return err
		}
	}

	fmt.Printf("\nModule %q created at %s\n", name, base)
	fmt.Printf("Don't forget to register it in cmd/main.go:\n\n")
	fmt.Printf("  import \"%s/internal/modules/%s\"\n\n", modPath, name)
	fmt.Printf("  app.RegisterModules(\n    %s.New(),\n  )\n\n", name)

	return nil
}

var moduleModelTmpl = `package {{ .Name }}

// {{ .Pascal }} is the module's domain entity.
type {{ .Pascal }} struct {
	ID string ` + "`json:\"id\"`" + `
}
`

var moduleRepositoryTmpl = `package {{ .Name }}

import "context"

// Repository defines the persistence required by Service.
type Repository interface {
	Create(ctx context.Context, entity *{{ .Pascal }}) error
	FindByID(ctx context.Context, id string) (*{{ .Pascal }}, error)
}
`

var moduleServiceTmpl = `package {{ .Name }}

import "context"

// Service contains the {{ .Name }} module's business logic.
type Service struct {
	repo Repository
}

// NewService creates a Service.
func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

// Name returns the service registry key.
func (s *Service) Name() string { return "{{ .Name }}.service" }

// Close cleans up resources held by the service.
func (s *Service) Close() error { return nil }

// Create creates a {{ .Name }}.
func (s *Service) Create(ctx context.Context, entity *{{ .Pascal }}) error {
	return s.repo.Create(ctx, entity)
}

// GetByID returns a {{ .Name }} by ID.
func (s *Service) GetByID(ctx context.Context, id string) (*{{ .Pascal }}, error) {
	return s.repo.FindByID(ctx, id)
}
`

var moduleMemoryRepositoryTmpl = `package {{ .Name }}

import (
	"context"
	"crypto/rand"
	"fmt"
	"sync"
)

// MemoryRepository stores {{ .Name }} records in memory.
type MemoryRepository struct {
	mu    sync.RWMutex
	items map[string]*{{ .Pascal }}
}

// NewMemoryRepository creates an empty in-memory repository.
func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{
		items: make(map[string]*{{ .Pascal }}),
	}
}

func (r *MemoryRepository) Create(_ context.Context, entity *{{ .Pascal }}) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	entity.ID = generateID()
	r.items[entity.ID] = entity
	return nil
}

func (r *MemoryRepository) FindByID(_ context.Context, id string) (*{{ .Pascal }}, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entity, ok := r.items[id]
	if !ok {
		return nil, fmt.Errorf("{{ .Name }} %q not found", id)
	}
	return entity, nil
}

func generateID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}
`

var moduleHTTPTmpl = `package {{ .Name }}

import (
	"encoding/json"
	"net/http"
)

// HTTPHandler handles HTTP requests for the {{ .Name }} module.
type HTTPHandler struct {
	service *Service
}

// NewHTTPHandler creates an HTTPHandler.
func NewHTTPHandler(service *Service) *HTTPHandler {
	return &HTTPHandler{service: service}
}

// GetByID handles GET /{{ .Name }}s/{id}.
func (h *HTTPHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	entity, err := h.service.GetByID(r.Context(), r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "{{ .Name }} not found"})
		return
	}

	writeJSON(w, http.StatusOK, entity)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
`

var moduleWiringTmpl = `package {{ .Name }}

import (
	"context"

	"github.com/charlesonunze/fw"
	fwhttp "github.com/charlesonunze/fw/transport/http"
)

// Module owns the {{ .Name }} domain and its transports.
type Module struct {
	service *Service
	handler *HTTPHandler
}

// New creates a {{ .Name }} module.
func New() *Module {
	return &Module{}
}

// Name returns the module name.
func (m *Module) Name() string { return "{{ .Name }}" }

// Register constructs and exposes the module's services.
func (m *Module) Register(deps *fw.Deps) error {
	repo := NewMemoryRepository()
	m.service = NewService(repo)
	return deps.Services.Register(m.service)
}

// Init completes the module's internal wiring.
func (m *Module) Init(_ context.Context, _ *fw.Deps) error {
	m.handler = NewHTTPHandler(m.service)
	return nil
}

// RegisterRoutes exposes the module's HTTP routes.
func (m *Module) RegisterRoutes(r fwhttp.Router) {
	r.Group("/{{ .Name }}s").Get("/{id}", m.handler.GetByID)
}

// Health reports whether the module is ready.
func (m *Module) Health(_ context.Context) error { return nil }

// Close releases resources owned by the module.
func (m *Module) Close() error {
	if m.service == nil {
		return nil
	}
	return m.service.Close()
}
`
