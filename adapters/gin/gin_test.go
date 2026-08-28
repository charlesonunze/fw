package gin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	gingonic "github.com/gin-gonic/gin"
)

func TestMiddlewareCanShortCircuitRoute(t *testing.T) {
	router := newTestRouter()
	handlerCalled := false

	protected := router.Group("/protected", func(http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
		})
	})
	protected.Get("/resource", func(w http.ResponseWriter, _ *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusNoContent)
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/protected/resource", nil))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	if handlerCalled {
		t.Fatal("protected handler ran after middleware short-circuited")
	}
}

func TestMiddlewareIsBuiltOnceAndPreservesWrappedWriter(t *testing.T) {
	router := newTestRouter()
	builds := 0

	router.Use(func(next http.Handler) http.Handler {
		builds++
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(&wrappedWriter{ResponseWriter: w}, r)
		})
	})
	router.Get("/resource", func(w http.ResponseWriter, _ *http.Request) {
		if _, ok := w.(*wrappedWriter); !ok {
			t.Errorf("writer type = %T, want *wrappedWriter", w)
		}
		w.WriteHeader(http.StatusNoContent)
	})

	for range 2 {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/resource", nil))
	}

	if builds != 1 {
		t.Fatalf("middleware builds = %d, want 1", builds)
	}
}

func TestMiddlewareOrderingAndGroupInheritance(t *testing.T) {
	router := newTestRouter()
	var events []string

	router.Use(recordMiddleware("root", &events))
	group := router.Group("/group", recordMiddleware("group", &events))
	group.Get("/resource", func(http.ResponseWriter, *http.Request) {
		events = append(events, "handler")
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/group/resource", nil))

	want := []string{"root before", "group before", "handler", "group after", "root after"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
}

func TestMiddlewareRequestChangesReachHandler(t *testing.T) {
	type contextKey struct{}
	router := newTestRouter()

	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), contextKey{}, "request value")
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})
	router.Get("/resource", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Context().Value(contextKey{}); got != "request value" {
			t.Errorf("context value = %v, want request value", got)
		}
		w.WriteHeader(http.StatusNoContent)
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/resource", nil))
}

type wrappedWriter struct {
	http.ResponseWriter
}

func newTestRouter() *ginRouter {
	gingonic.SetMode(gingonic.TestMode)
	engine := gingonic.New()
	return NewRouter(engine).(*ginRouter)
}

func recordMiddleware(name string, events *[]string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			*events = append(*events, name+" before")
			next.ServeHTTP(w, r)
			*events = append(*events, name+" after")
		})
	}
}
