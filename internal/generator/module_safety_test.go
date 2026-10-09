package generator

import (
	"path/filepath"
	"testing"
)

func TestGeneratedModuleSafety(t *testing.T) {
	if testing.Short() {
		t.Skip("generated module behavior tests")
	}
	t.Setenv("GOWORK", "off")
	tests := []struct {
		name, transport, router string
	}{
		{"chi", ModuleTransportHTTP, routerChi},
		{"gin", ModuleTransportHTTP, routerGin},
		{"grpc", ModuleTransportGRPC, ""},
		{"none", ModuleTransportNone, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.transport == ModuleTransportGRPC {
				if err := checkProtoTools(); err != nil {
					t.Skip(err)
				}
			}
			t.Chdir(t.TempDir())
			writeFixture(t, "go.mod", "module example.com/app\n\ngo 1.26.9\n")
			if err := NewModule("user", "example.com/app", ModuleConfig{
				Transport: tt.transport, Router: tt.router,
			}); err != nil {
				t.Fatal(err)
			}
			base := filepath.Join("internal", "modules", "user")
			writeFixture(t, filepath.Join(base, "user_repository_test.go"), generatedRepositoryTests)
			switch tt.transport {
			case ModuleTransportHTTP:
				if err := writeTemplate(filepath.Join(base, "user_http_test.go"), generatedHTTPTests, moduleData{Router: tt.router}); err != nil {
					t.Fatal(err)
				}
			case ModuleTransportGRPC:
				writeFixture(t, filepath.Join(base, "user_grpc_test.go"), generatedGRPCTests)
			}
			if err := addLocalReplacements(".", tt.router, frameworkRoot(t)); err != nil {
				t.Fatal(err)
			}
			if err := runGo(".", "mod", "tidy"); err != nil {
				t.Fatal(err)
			}
			if err := runGo(".", "test", "-race", "-count=1", "./..."); err != nil {
				t.Fatalf("generated %s module safety tests: %v", tt.name, err)
			}
		})
	}
}

const generatedRepositoryTests = `package user

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestMemoryRepositoryOwnsCreatedValues(t *testing.T) {
	repo := NewMemoryRepository()
	entity := &User{}
	if err := repo.Create(t.Context(), entity); err != nil { t.Fatal(err) }
	id := entity.ID
	if id == "" { t.Fatal("Create did not assign an ID") }
	entity.ID = "changed by caller"
	stored, err := repo.FindByID(t.Context(), id)
	if err != nil { t.Fatal(err) }
	if stored.ID != id {
		t.Fatalf("stored ID = %q, want %q", stored.ID, id)
	}
}

func TestMemoryRepositoryReturnsIndependentValues(t *testing.T) {
	repo := NewMemoryRepository()
	entity := &User{}
	if err := repo.Create(t.Context(), entity); err != nil { t.Fatal(err) }
	id := entity.ID
	first, err := repo.FindByID(t.Context(), id)
	if err != nil { t.Fatal(err) }
	second, err := repo.FindByID(t.Context(), id)
	if err != nil { t.Fatal(err) }
	first.ID = "changed by reader"
	stored, err := repo.FindByID(t.Context(), id)
	if err != nil { t.Fatal(err) }
	if second.ID != id || stored.ID != id || entity.ID != id {
		t.Fatalf("read mutation escaped: second=%q, stored=%q, input=%q", second.ID, stored.ID, entity.ID)
	}
}

func TestMemoryRepositoryNotFoundHasStableIdentity(t *testing.T) {
	repo := NewMemoryRepository()
	entity, err := repo.FindByID(t.Context(), "missing")
	_, other := repo.FindByID(t.Context(), "also missing")
	if entity != nil || !errors.Is(err, ErrNotFound) || !errors.Is(other, ErrNotFound) {
		t.Fatalf("FindByID = %v, %v; want nil and a stable not-found error", entity, err)
	}
}

func TestMemoryRepositoryConcurrentOwnership(t *testing.T) {
	repo := NewMemoryRepository()
	entity := &User{}
	if err := repo.Create(t.Context(), entity); err != nil { t.Fatal(err) }
	id := entity.ID
	var workers sync.WaitGroup
	for range 8 {
		workers.Go(func() {
			for range 50 {
				value, err := repo.FindByID(t.Context(), id)
				if err != nil { t.Error(err); return }
				if value.ID != id { t.Errorf("stored ID = %q, want %q", value.ID, id); return }
				value.ID = "local change"
				created := &User{}
				if err := repo.Create(t.Context(), created); err != nil { t.Error(err); return }
				created.ID = "another local change"
			}
		})
	}
	workers.Wait()
}

type resultRepository struct {
	entity *User
	err error
}

func (*resultRepository) Create(context.Context, *User) error { return nil }
func (r *resultRepository) FindByID(context.Context, string) (*User, error) {
	return r.entity, r.err
}
`

const generatedHTTPTests = `package user

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	{{- if eq .Router "gin" }}
	"github.com/gin-gonic/gin"
	{{- else }}
	"github.com/go-chi/chi/v5"
	{{- end }}
)

func TestHTTPHandlerErrors(t *testing.T) {
	_, missing := NewMemoryRepository().FindByID(t.Context(), "missing")
	tests := []struct {
		name string
		err error
		status int
		message string
	}{
		{"success", nil, http.StatusOK, ""},
		{"not found", missing, http.StatusNotFound, "user not found"},
		{"wrapped not found", fmt.Errorf("lookup: %w", missing), http.StatusNotFound, "user not found"},
		{"backend failure", errors.New("postgres password=private"), http.StatusInternalServerError, "internal server error"},
		{"unrelated same message", errors.New("user not found"), http.StatusInternalServerError, "internal server error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewHTTPHandler(NewService(&resultRepository{entity: &User{ID: "123"}, err: tt.err}))
			{{- if eq .Router "gin" }}
			gin.SetMode(gin.TestMode)
			router := gin.New()
			router.GET("/users/:id", handler.GetByID)
			{{- else }}
			router := chi.NewRouter()
			router.Get("/users/{id}", handler.GetByID)
			{{- end }}
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/users/123", nil))
			if recorder.Code != tt.status {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, tt.status, recorder.Body)
			}
			var body struct { ID, Error string }
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil { t.Fatal(err) }
			if body.Error != tt.message { t.Fatalf("error = %q, want %q", body.Error, tt.message) }
			if tt.err == nil && body.ID != "123" { t.Fatalf("ID = %q", body.ID) }
		})
	}
}
`

const generatedGRPCTests = `package user

import (
	"context"
	"errors"
	"fmt"
	"testing"
	userpb "example.com/app/internal/modules/user/pb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestGRPCHandlerErrors(t *testing.T) {
	_, missing := NewMemoryRepository().FindByID(t.Context(), "missing")
	tests := []struct {
		name string
		err error
		code codes.Code
		message string
	}{
		{"success", nil, codes.OK, ""},
		{"not found", missing, codes.NotFound, "user not found"},
		{"wrapped not found", fmt.Errorf("lookup: %w", missing), codes.NotFound, "user not found"},
		{"backend failure", errors.New("postgres password=private"), codes.Internal, "internal server error"},
		{"unrelated same message", errors.New("user not found"), codes.Internal, "internal server error"},
		{"canceled", context.Canceled, codes.Canceled, "context canceled"},
		{"wrapped canceled", fmt.Errorf("private detail: %w", context.Canceled), codes.Canceled, "context canceled"},
		{"deadline", context.DeadlineExceeded, codes.DeadlineExceeded, "context deadline exceeded"},
		{"wrapped deadline", fmt.Errorf("private detail: %w", context.DeadlineExceeded), codes.DeadlineExceeded, "context deadline exceeded"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewGRPCHandler(NewService(&resultRepository{entity: &User{ID: "123"}, err: tt.err}))
			response, err := handler.GetUser(t.Context(), &userpb.GetUserRequest{Id: "123"})
			if status.Code(err) != tt.code { t.Fatalf("code = %s, want %s; error=%v", status.Code(err), tt.code, err) }
			if tt.err == nil {
				if response.GetId() != "123" { t.Fatalf("ID = %q", response.GetId()) }
				return
			}
			if response != nil { t.Fatalf("response = %v, want nil", response) }
			if got := status.Convert(err).Message(); got != tt.message {
				t.Fatalf("message = %q, want %q", got, tt.message)
			}
		})
	}
}
`
