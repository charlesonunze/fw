package chi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/charlesonunze/fw"
	chi "github.com/go-chi/chi/v5"
)

type nativeModule struct {
	routes func(chi.Router)
}

func (*nativeModule) Name() fw.ModuleName                  { return "users" }
func (*nativeModule) Imports() []fw.ModuleName             { return nil }
func (*nativeModule) Register(*fw.Deps) error              { return nil }
func (*nativeModule) Init(context.Context, *fw.Deps) error { return nil }
func (*nativeModule) Health(context.Context) error         { return nil }
func (*nativeModule) Close() error                         { return nil }
func (m *nativeModule) RegisterRoutes(r chi.Router)        { m.routes(r) }

func TestRegisterNativeModulesAndHealth(t *testing.T) {
	adapter := NewAdapter(chi.NewRouter())
	module := &nativeModule{routes: func(r chi.Router) {
		r.Route("/teams/{team}", func(r chi.Router) {
			r.Get("/users/{id}", func(w http.ResponseWriter, req *http.Request) {
				if chi.URLParam(req, "team") != "core" || chi.URLParam(req, "id") != "42" {
					t.Error("missing native route parameters")
				}
				w.WriteHeader(http.StatusNoContent)
			})
		})
	}}
	if err := adapter.RegisterModules([]fw.Module{module}); err != nil {
		t.Fatal(err)
	}
	adapter.RegisterHealth(
		func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) },
		func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) },
	)
	for path, want := range map[string]int{
		"/teams/core/users/42": http.StatusNoContent,
		"/health/live":         http.StatusOK,
		"/health/ready":        http.StatusServiceUnavailable,
	} {
		response := httptest.NewRecorder()
		adapter.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != want {
			t.Errorf("%s = %d, want %d", path, response.Code, want)
		}
	}
}

type foreignModule struct{ nativeModule }

func (*foreignModule) RegisterRoutes(*http.ServeMux) {}

type backgroundModule struct{ fw.Module }

func TestRegisterModulesRejectsMismatchBeforeMutatingRouter(t *testing.T) {
	router := chi.NewRouter()
	valid := &nativeModule{routes: func(chi.Router) { t.Fatal("registered before validation finished") }}
	err := NewAdapter(router).RegisterModules([]fw.Module{valid, &foreignModule{}})
	if err == nil || !strings.Contains(err.Error(), "users") || !strings.Contains(err.Error(), "chi.Router") {
		t.Fatalf("RegisterModules() = %v, want named router mismatch", err)
	}
	if len(router.Routes()) != 0 {
		t.Fatal("mismatch mutated routes")
	}
	if err := NewAdapter(router).RegisterModules([]fw.Module{&backgroundModule{}}); err != nil {
		t.Fatalf("background module: %v", err)
	}
}

func TestRegisterModulesRequiresRouter(t *testing.T) {
	for _, adapter := range []*Adapter{nil, {}, NewAdapter(nil)} {
		if err := adapter.RegisterModules(nil); err == nil {
			t.Fatal("missing router accepted")
		}
	}
}

var _ Module = (*nativeModule)(nil)

func TestServeHTTPPopulatesMatchedRequestPattern(t *testing.T) {
	var handlerPattern string
	rawRouter := chi.NewRouter()
	rawRouter.Get("/users/{userID}", func(w http.ResponseWriter, r *http.Request) {
		handlerPattern = r.Pattern
		w.WriteHeader(http.StatusNoContent)
	})
	router := NewAdapter(rawRouter)
	request := httptest.NewRequest(http.MethodGet, "/users/42", nil)

	router.ServeHTTP(httptest.NewRecorder(), request)

	const want = "/users/{userID}"
	if handlerPattern != want {
		t.Fatalf("handler request pattern = %q, want %q", handlerPattern, want)
	}
	if request.Pattern != want {
		t.Fatalf("outer request pattern = %q, want %q", request.Pattern, want)
	}
}

func TestServeHTTPPopulatesGroupedRoutePatternWhenMiddlewareShortCircuits(t *testing.T) {
	rawRouter := chi.NewRouter()
	router := NewAdapter(rawRouter)
	group := chi.NewRouter()
	group.Use(func(http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
		})
	})
	group.Get("/users/{userID}", func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler ran after middleware short-circuited")
	})
	rawRouter.Mount("/api", group)
	request := httptest.NewRequest(http.MethodGet, "/api/users/42", nil)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	const want = "/api/users/{userID}"
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	if request.Pattern != want {
		t.Fatalf("outer request pattern = %q, want %q", request.Pattern, want)
	}
}

func TestServeHTTPPopulatesRoutePatternWhenRootMiddlewareShortCircuits(t *testing.T) {
	rawRouter := chi.NewRouter()
	router := NewAdapter(rawRouter)
	rawRouter.Use(func(http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
		})
	})
	rawRouter.Get("/users/{userID}", func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler ran after middleware short-circuited")
	})
	request := httptest.NewRequest(http.MethodGet, "/users/42", nil)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	const want = "/users/{userID}"
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
	if request.Pattern != want {
		t.Fatalf("outer request pattern = %q, want %q", request.Pattern, want)
	}
}

func TestServeHTTPPopulatesMountedRoutePattern(t *testing.T) {
	rawRouter := chi.NewRouter()
	router := NewAdapter(rawRouter)
	mounted := chi.NewRouter()
	mounted.Get("/users/{userID}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	rawRouter.Mount("/api", mounted)
	request := httptest.NewRequest(http.MethodGet, "/api/users/42", nil)

	router.ServeHTTP(httptest.NewRecorder(), request)

	const want = "/api/users/{userID}"
	if request.Pattern != want {
		t.Fatalf("outer request pattern = %q, want %q", request.Pattern, want)
	}
}

func TestServeHTTPPreservesPatternWhenNoChiRouteMatches(t *testing.T) {
	router := NewAdapter(chi.NewRouter())
	request := httptest.NewRequest(http.MethodGet, "/missing", nil)
	request.Pattern = "/fallback/"

	router.ServeHTTP(httptest.NewRecorder(), request)

	if request.Pattern != "/fallback/" {
		t.Fatalf("outer request pattern = %q, want preserved fallback pattern", request.Pattern)
	}
}
