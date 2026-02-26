package generator

import (
	"fmt"
	"path/filepath"
)

type moduleData struct {
	Name       string // lowercase, e.g. "user"
	Pascal     string // PascalCase, e.g. "User"
	ModulePath string // go module path, e.g. "github.com/you/myapp"
}

// NewModule generates a module with domain, service, repo, api, transform, and module.go.
func NewModule(name, modPath string) error {
	data := moduleData{
		Name:       name,
		Pascal:     pascal(name),
		ModulePath: modPath,
	}

	base := filepath.Join("internal", "modules", name)

	files := map[string]string{
		filepath.Join(base, "domain", name+"_domain.go"):       moduleDomainTmpl,
		filepath.Join(base, "service", name+"_service.go"):     moduleServiceTmpl,
		filepath.Join(base, "repo", name+"_repo_inmemory.go"):  moduleRepoTmpl,
		filepath.Join(base, "api", name+"_handler.go"):         moduleHandlerTmpl,
		filepath.Join(base, "transform", name+"_transform.go"): moduleTransformTmpl,
		filepath.Join(base, "module.go"):                       moduleWiringTmpl,
	}

	for path, tmpl := range files {
		fmt.Printf("  create %s\n", path)
		if err := writeTemplate(path, tmpl, data); err != nil {
			return err
		}
	}

	fmt.Printf("\nModule %q created at %s\n", name, base)
	fmt.Printf("Don't forget to register it in cmd/main.go:\n\n")
	fmt.Printf("  import \"%s/internal/modules/%s\"\n\n", modPath, name)
	fmt.Printf("  app.Register(\n    %s.New(),\n  )\n\n", name)

	return nil
}

var moduleDomainTmpl = `package domain

import "context"

// {{ .Pascal }} is the domain entity.
type {{ .Pascal }} struct {
	ID string ` + "`json:\"id\"`" + `
}

// {{ .Pascal }}Repository defines the data access interface.
type {{ .Pascal }}Repository interface {
	Create(ctx context.Context, entity *{{ .Pascal }}) error
	FindByID(ctx context.Context, id string) (*{{ .Pascal }}, error)
}
`

var moduleServiceTmpl = `package service

import (
	"context"

	"{{ .ModulePath }}/internal/modules/{{ .Name }}/domain"
)

// {{ .Pascal }}Service contains the business logic.
type {{ .Pascal }}Service struct {
	repo domain.{{ .Pascal }}Repository
}

// New creates a new {{ .Pascal }}Service.
func New(repo domain.{{ .Pascal }}Repository) *{{ .Pascal }}Service {
	return &{{ .Pascal }}Service{repo: repo}
}

// Name returns the service registry key.
func (s *{{ .Pascal }}Service) Name() string { return "{{ .Name }}.service" }

// Create creates a new {{ .Name }}.
func (s *{{ .Pascal }}Service) Create(ctx context.Context, entity *domain.{{ .Pascal }}) error {
	return s.repo.Create(ctx, entity)
}

// GetByID returns a {{ .Name }} by ID.
func (s *{{ .Pascal }}Service) GetByID(ctx context.Context, id string) (*domain.{{ .Pascal }}, error) {
	return s.repo.FindByID(ctx, id)
}
`

var moduleRepoTmpl = `package repo

import (
	"context"
	"crypto/rand"
	"fmt"
	"sync"

	"{{ .ModulePath }}/internal/modules/{{ .Name }}/domain"
)

// {{ .Pascal }}InMemoryRepo is an in-memory implementation of {{ .Pascal }}Repository.
type {{ .Pascal }}InMemoryRepo struct {
	mu    sync.RWMutex
	items map[string]*domain.{{ .Pascal }}
}

// New creates a new in-memory repository.
func New() *{{ .Pascal }}InMemoryRepo {
	return &{{ .Pascal }}InMemoryRepo{
		items: make(map[string]*domain.{{ .Pascal }}),
	}
}

func (r *{{ .Pascal }}InMemoryRepo) Create(_ context.Context, entity *domain.{{ .Pascal }}) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	entity.ID = generateID()
	r.items[entity.ID] = entity
	return nil
}

func (r *{{ .Pascal }}InMemoryRepo) FindByID(_ context.Context, id string) (*domain.{{ .Pascal }}, error) {
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

var moduleHandlerTmpl = `package api

import (
	"net/http"

	"{{ .ModulePath }}/internal/modules/{{ .Name }}/service"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
)

// {{ .Pascal }}Handler handles HTTP requests for the {{ .Name }} module.
type {{ .Pascal }}Handler struct {
	service *service.{{ .Pascal }}Service
}

// New creates a new {{ .Pascal }}Handler.
func New(svc *service.{{ .Pascal }}Service) *{{ .Pascal }}Handler {
	return &{{ .Pascal }}Handler{service: svc}
}

// GetByID handles GET /{{ .Name }}s/{id}
func (h *{{ .Pascal }}Handler) GetByID(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	entity, err := h.service.GetByID(r.Context(), id)
	if err != nil {
		render.Status(r, http.StatusNotFound)
		render.JSON(w, r, map[string]string{"error": "{{ .Name }} not found"})
		return
	}

	render.JSON(w, r, entity)
}
`

var moduleTransformTmpl = `package transform

import "{{ .ModulePath }}/internal/modules/{{ .Name }}/domain"

// {{ .Pascal }}Response is the API response representation.
type {{ .Pascal }}Response struct {
	ID string ` + "`json:\"id\"`" + `
}

// To{{ .Pascal }}Response converts a domain entity to an API response.
func To{{ .Pascal }}Response(entity *domain.{{ .Pascal }}) {{ .Pascal }}Response {
	return {{ .Pascal }}Response{
		ID: entity.ID,
	}
}
`

var moduleWiringTmpl = `package {{ .Name }}

import (
	"github.com/charlesonunze/fw"
	"{{ .ModulePath }}/internal/modules/{{ .Name }}/api"
	"{{ .ModulePath }}/internal/modules/{{ .Name }}/repo"
	"{{ .ModulePath }}/internal/modules/{{ .Name }}/service"
	"github.com/go-chi/chi/v5"
)

// {{ .Pascal }}Module is the {{ .Name }} module.
type {{ .Pascal }}Module struct {
	service *service.{{ .Pascal }}Service
	handler *api.{{ .Pascal }}Handler
}

// New creates a new {{ .Pascal }}Module.
func New() *{{ .Pascal }}Module {
	return &{{ .Pascal }}Module{}
}

func (m *{{ .Pascal }}Module) Name() string { return "{{ .Name }}" }

func (m *{{ .Pascal }}Module) Init(deps *fw.Deps) error {
	r := repo.New()
	m.service = service.New(r)
	m.handler = api.New(m.service)

	deps.Services.Register(m.service)
	return nil
}

func (m *{{ .Pascal }}Module) RegisterRoutes(r chi.Router) {
	r.Route("/{{ .Name }}s", func(r chi.Router) {
		r.Get("/{id}", m.handler.GetByID)
	})
}

func (m *{{ .Pascal }}Module) Close() error { return nil }
`
