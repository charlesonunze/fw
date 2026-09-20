package generator

import (
	"fmt"
	"path/filepath"
)

const (
	// ModuleTransportHTTP generates a net/http-compatible module handler.
	ModuleTransportHTTP = "http"
	// ModuleTransportGRPC generates protobuf definitions and a gRPC handler.
	ModuleTransportGRPC = "grpc"
	// ModuleTransportNone generates a module without a transport handler.
	ModuleTransportNone = "none"
)

// ModuleConfig configures generated module boundaries.
type ModuleConfig struct {
	Transport string
}

type moduleData struct {
	Name       string // lowercase, e.g. "user"
	Pascal     string // PascalCase, e.g. "User"
	ModulePath string // go module path, e.g. "github.com/you/myapp"
	Transport  string
}

// NewModule generates a flat, self-contained module package.
func NewModule(name, modPath string, config ModuleConfig) (err error) {
	if err := validateModuleName(name); err != nil {
		return err
	}
	if err := validateModulePath(modPath); err != nil {
		return err
	}
	transport, err := moduleTransport(config.Transport)
	if err != nil {
		return err
	}
	if transport == ModuleTransportGRPC {
		if err := checkProtoTools(); err != nil {
			return err
		}
	}

	data := moduleData{
		Name:       name,
		Pascal:     pascal(name),
		ModulePath: modPath,
		Transport:  transport,
	}

	base := filepath.Join("internal", "modules", name)
	if err := validateLocalDirectoryPath(filepath.Dir(base)); err != nil {
		return err
	}
	if err := createGeneratedDir(base); err != nil {
		return err
	}
	defer cleanupGeneratedDir(base, &err)

	type moduleFile struct {
		path string
		tmpl string
	}
	files := []moduleFile{
		{filepath.Join(base, name+"_module.go"), moduleWiringTmpl},
		{filepath.Join(base, name+"_model.go"), moduleModelTmpl},
		{filepath.Join(base, name+"_service.go"), moduleServiceTmpl},
		{filepath.Join(base, name+"_repository.go"), moduleRepositoryTmpl},
		{filepath.Join(base, name+"_repository_memory.go"), moduleMemoryRepositoryTmpl},
	}
	switch transport {
	case ModuleTransportHTTP:
		files = append(files, moduleFile{filepath.Join(base, name+"_http.go"), moduleHTTPTmpl})
	case ModuleTransportGRPC:
		files = append(files, moduleFile{filepath.Join(base, name+"_grpc.go"), moduleGRPCTmpl})
	}

	for _, file := range files {
		fmt.Printf("  create %s\n", file.path)
		if err = writeTemplate(file.path, file.tmpl, data); err != nil {
			return err
		}
	}
	if transport == ModuleTransportGRPC {
		if err = NewProto(name, modPath); err != nil {
			return err
		}
	}

	fmt.Printf("\nModule %q created at %s\n", name, base)
	fmt.Printf("Don't forget to register it in cmd/main.go:\n\n")
	fmt.Printf("  import \"%s/internal/modules/%s\"\n\n", modPath, name)
	fmt.Printf("  app.RegisterModules(\n    %s.New(),\n  )\n\n", name)
	if transport == ModuleTransportGRPC {
		fmt.Printf("Ensure cmd/main.go configures a github.com/charlesonunze/fw/transport/grpc transport.\n")
		fmt.Printf("Run 'go mod tidy' to add the generated gRPC dependencies.\n\n")
	}

	return nil
}

func moduleTransport(transport string) (string, error) {
	if transport == "" {
		return ModuleTransportHTTP, nil
	}
	switch transport {
	case ModuleTransportHTTP, ModuleTransportGRPC, ModuleTransportNone:
		return transport, nil
	default:
		return "", fmt.Errorf("unsupported module transport %q: use http, grpc, or none", transport)
	}
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

import (
	"context"

	"github.com/charlesonunze/fw"
)

// Service is the {{ .Name }} capability exposed to other modules.
type Service interface {
	fw.Service
	Create(ctx context.Context, entity *{{ .Pascal }}) error
	GetByID(ctx context.Context, id string) (*{{ .Pascal }}, error)
}

type service struct {
	repo Repository
}

// NewService creates a Service.
func NewService(repo Repository) *service {
	return &service{repo: repo}
}

// Name returns the service registry key.
func (*service) Name() string { return "{{ .Name }}.service" }

// Health reports whether the service is ready.
func (*service) Health(context.Context) error { return nil }

// Close cleans up resources held by the service.
func (*service) Close() error { return nil }

// Create creates a {{ .Name }}.
func (s *service) Create(ctx context.Context, entity *{{ .Pascal }}) error {
	return s.repo.Create(ctx, entity)
}

// GetByID returns a {{ .Name }} by ID.
func (s *service) GetByID(ctx context.Context, id string) (*{{ .Pascal }}, error) {
	return s.repo.FindByID(ctx, id)
}

var _ Service = (*service)(nil)
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
	service Service
}

// NewHTTPHandler creates an HTTPHandler.
func NewHTTPHandler(service Service) *HTTPHandler {
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

var moduleGRPCTmpl = `package {{ .Name }}

import (
	"context"

	{{ .Name }}pb "{{ .ModulePath }}/internal/modules/{{ .Name }}/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// GRPCHandler exposes the {{ .Name }} service over gRPC.
type GRPCHandler struct {
	{{ .Name }}pb.Unimplemented{{ .Pascal }}ServiceServer
	service Service
}

// NewGRPCHandler creates a GRPCHandler.
func NewGRPCHandler(service Service) *GRPCHandler {
	return &GRPCHandler{service: service}
}

// Get{{ .Pascal }} handles the generated Get{{ .Pascal }} RPC.
func (h *GRPCHandler) Get{{ .Pascal }}(
	ctx context.Context,
	request *{{ .Name }}pb.Get{{ .Pascal }}Request,
) (*{{ .Name }}pb.Get{{ .Pascal }}Response, error) {
	entity, err := h.service.GetByID(ctx, request.GetId())
	if err != nil {
		return nil, status.Error(codes.NotFound, "{{ .Name }} not found")
	}
	return &{{ .Name }}pb.Get{{ .Pascal }}Response{Id: entity.ID}, nil
}

// RegisterGRPC exposes the module's gRPC service.
func (m *Module) RegisterGRPC(server *grpc.Server) {
	{{ .Name }}pb.Register{{ .Pascal }}ServiceServer(server, m.handler)
}
`

var moduleWiringTmpl = `package {{ .Name }}

import (
	"context"

	"github.com/charlesonunze/fw"
	{{- if eq .Transport "http" }}
	fwhttp "github.com/charlesonunze/fw/transport/http"
	{{- end }}
)

// Module owns the {{ .Name }} domain and its transports.
type Module struct {
	service Service
	{{- if eq .Transport "http" }}
	handler *HTTPHandler
	{{- else if eq .Transport "grpc" }}
	handler *GRPCHandler
	{{- end }}
}

// Name identifies the {{ .Name }} module.
const Name fw.ModuleName = "{{ .Name }}"

// New creates a {{ .Name }} module.
func New() *Module {
	return &Module{}
}

// Name returns the module name.
func (m *Module) Name() fw.ModuleName { return Name }

// Imports declares this module's direct dependencies.
func (m *Module) Imports() []fw.ModuleName { return nil }

// Register constructs and exposes the module's services.
func (m *Module) Register(deps *fw.Deps) error {
	repo := NewMemoryRepository()
	m.service = NewService(repo)
	return deps.Services.Register(m.service, fw.As[Service]())
}

// Init completes the module's internal wiring.
func (m *Module) Init(_ context.Context, _ *fw.Deps) error {
	{{- if eq .Transport "http" }}
	m.handler = NewHTTPHandler(m.service)
	{{- else if eq .Transport "grpc" }}
	m.handler = NewGRPCHandler(m.service)
	{{- end }}
	return nil
}

	{{- if eq .Transport "http" }}
// RegisterRoutes exposes the module's HTTP routes.
func (m *Module) RegisterRoutes(r fwhttp.Router) {
	r.Group("/{{ .Name }}s").Get("/{id}", m.handler.GetByID)
}
	{{- end }}

// Health reports whether the module is ready.
func (m *Module) Health(ctx context.Context) error {
	if m.service == nil {
		return nil
	}
	return m.service.Health(ctx)
}

// Close releases resources owned by the module.
func (m *Module) Close() error {
	if m.service == nil {
		return nil
	}
	return m.service.Close()
}
`
