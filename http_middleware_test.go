package fw

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

type middlewareTestRouter struct {
	handler http.Handler
}

func (r *middlewareTestRouter) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.handler.ServeHTTP(w, req)
}

func (*middlewareTestRouter) Get(string, http.HandlerFunc) {}

func (*middlewareTestRouter) Post(string, http.HandlerFunc) {}

func (*middlewareTestRouter) Put(string, http.HandlerFunc) {}

func (*middlewareTestRouter) Delete(string, http.HandlerFunc) {}

func (*middlewareTestRouter) Patch(string, http.HandlerFunc) {}

func (*middlewareTestRouter) Handle(string, string, http.HandlerFunc) {}

func (r *middlewareTestRouter) Group(string, ...func(http.Handler) http.Handler) Router { return r }

func (*middlewareTestRouter) Use(...func(http.Handler) http.Handler) {}

func (*middlewareTestRouter) Mount(string, http.Handler) {}

func TestHTTPMiddlewareOrder(t *testing.T) {
	var events []string
	record := func(name string) func(http.Handler) http.Handler {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				events = append(events, "before "+name)
				next.ServeHTTP(w, r)
				events = append(events, "after "+name)
			})
		}
	}

	router := &middlewareTestRouter{handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		events = append(events, "handler")
	})}
	app := New(
		WithHTTP(HTTPConfig{
			Router:     router,
			Middleware: []func(http.Handler) http.Handler{record("config one"), record("config two")},
		}),
		WithLogger(discardLogger{}),
	)
	app.Use(record("app one"), record("app two"))

	if err := app.setup(); err != nil {
		t.Fatalf("setup() error = %v", err)
	}
	recorder := httptest.NewRecorder()
	app.buildHTTPServer().Handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

	want := []string{
		"before config one",
		"before config two",
		"before app one",
		"before app two",
		"handler",
		"after app two",
		"after app one",
		"after config two",
		"after config one",
	}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("middleware events = %v, want %v", events, want)
	}
}

func TestHTTPConfigMiddlewareCanShortCircuit(t *testing.T) {
	handlerCalled := false
	router := &middlewareTestRouter{handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		handlerCalled = true
	})}
	app := New(WithHTTP(HTTPConfig{
		Router: router,
		Middleware: []func(http.Handler) http.Handler{
			func(http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					http.Error(w, "blocked", http.StatusUnauthorized)
				})
			},
		},
	}), WithLogger(discardLogger{}))

	if err := app.setup(); err != nil {
		t.Fatalf("setup() error = %v", err)
	}
	recorder := httptest.NewRecorder()
	app.buildHTTPServer().Handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status code = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	if handlerCalled {
		t.Fatal("router handler was called after middleware short-circuited")
	}
}

func TestAppUseRequiresHTTPTransport(t *testing.T) {
	app := New(WithGRPC(GRPCConfig{}), WithLogger(discardLogger{}))
	app.Use(func(next http.Handler) http.Handler { return next })

	if app.httpConfig != nil {
		t.Fatal("app.Use enabled the HTTP transport")
	}
	err := app.Start()
	if err == nil || !strings.Contains(err.Error(), "app.Use requires HTTP transport configuration") {
		t.Fatalf("Start() error = %v, want missing HTTP transport error", err)
	}
}
