package chi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	chi "github.com/go-chi/chi/v5"
)

func TestServeHTTPPopulatesMatchedRequestPattern(t *testing.T) {
	var handlerPattern string
	rawRouter := chi.NewRouter()
	rawRouter.Get("/users/{userID}", func(w http.ResponseWriter, r *http.Request) {
		handlerPattern = r.Pattern
		w.WriteHeader(http.StatusNoContent)
	})
	router := NewRouter(rawRouter).(*chiRouter)
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
	router := NewRouter(chi.NewRouter()).(*chiRouter)
	group := router.Group("/api", func(http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
		})
	})
	group.Get("/users/{userID}", func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler ran after middleware short-circuited")
	})
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
	router := NewRouter(chi.NewRouter()).(*chiRouter)
	router.Use(func(http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
		})
	})
	router.Get("/users/{userID}", func(http.ResponseWriter, *http.Request) {
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
	router := NewRouter(chi.NewRouter()).(*chiRouter)
	mounted := chi.NewRouter()
	mounted.Get("/users/{userID}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	router.Mount("/api", mounted)
	request := httptest.NewRequest(http.MethodGet, "/api/users/42", nil)

	router.ServeHTTP(httptest.NewRecorder(), request)

	const want = "/api/users/{userID}"
	if request.Pattern != want {
		t.Fatalf("outer request pattern = %q, want %q", request.Pattern, want)
	}
}

func TestServeHTTPPreservesPatternWhenNoChiRouteMatches(t *testing.T) {
	router := NewRouter(chi.NewRouter()).(*chiRouter)
	request := httptest.NewRequest(http.MethodGet, "/missing", nil)
	request.Pattern = "/fallback/"

	router.ServeHTTP(httptest.NewRecorder(), request)

	if request.Pattern != "/fallback/" {
		t.Fatalf("outer request pattern = %q, want preserved fallback pattern", request.Pattern)
	}
}
