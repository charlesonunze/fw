package fwhttp

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charlesonunze/fw"
)

type testLogger struct{}

func (testLogger) Info(string, ...any)     {}
func (testLogger) Error(string, ...any)    {}
func (testLogger) Debug(string, ...any)    {}
func (testLogger) Warn(string, ...any)     {}
func (l testLogger) With(...any) fw.Logger { return l }

type testRouter struct {
	mu       sync.RWMutex
	routes   map[string]http.HandlerFunc
	fallback http.Handler
}

func newTestRouter() *testRouter {
	return &testRouter{routes: make(map[string]http.HandlerFunc)}
}

func (r *testRouter) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	r.mu.RLock()
	handler := r.routes[request.Method+" "+request.URL.Path]
	fallback := r.fallback
	r.mu.RUnlock()
	if handler != nil {
		handler(w, request)
		return
	}
	if fallback != nil {
		fallback.ServeHTTP(w, request)
		return
	}
	http.NotFound(w, request)
}

func (r *testRouter) add(method, path string, handler http.HandlerFunc) {
	r.mu.Lock()
	r.routes[method+" "+path] = handler
	r.mu.Unlock()
}

func (r *testRouter) Get(path string, handler http.HandlerFunc) { r.add(http.MethodGet, path, handler) }
func (r *testRouter) Post(path string, handler http.HandlerFunc) {
	r.add(http.MethodPost, path, handler)
}
func (r *testRouter) Put(path string, handler http.HandlerFunc) { r.add(http.MethodPut, path, handler) }
func (r *testRouter) Delete(path string, handler http.HandlerFunc) {
	r.add(http.MethodDelete, path, handler)
}
func (r *testRouter) Patch(path string, handler http.HandlerFunc) {
	r.add(http.MethodPatch, path, handler)
}
func (r *testRouter) Handle(method, path string, handler http.HandlerFunc) {
	r.add(method, path, handler)
}
func (r *testRouter) Group(string, ...func(http.Handler) http.Handler) Router { return r }
func (*testRouter) Use(...func(http.Handler) http.Handler)                    {}
func (r *testRouter) Mount(pattern string, handler http.Handler) {
	r.add(http.MethodGet, pattern, handler.ServeHTTP)
}

type testModule struct {
	registrations int
}

func (*testModule) Name() string                         { return "todo" }
func (*testModule) Register(*fw.Deps) error              { return nil }
func (*testModule) Init(context.Context, *fw.Deps) error { return nil }
func (*testModule) Health(context.Context) error         { return nil }
func (*testModule) Close() error                         { return nil }
func (m *testModule) RegisterRoutes(Router)              { m.registrations++ }

func testDeps(modules ...fw.Module) fw.TransportDeps {
	return fw.TransportDeps{
		Modules: modules,
		Logger:  testLogger{},
		Health: func(context.Context) fw.HealthReport {
			return fw.HealthReport{Healthy: true, Modules: map[string]bool{}}
		},
	}
}

func TestPrepareUsesDefaultsAndRegistersHTTPModules(t *testing.T) {
	module := &testModule{}
	router := newTestRouter()
	transport := New(Config{Addr: "127.0.0.1:0", Router: router})
	if err := transport.Prepare(context.Background(), testDeps(module)); err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	t.Cleanup(func() { _ = transport.Stop(context.Background()) })

	if module.registrations != 1 {
		t.Fatalf("route registrations = %d, want 1", module.registrations)
	}
	if transport.Name() != defaultName {
		t.Fatalf("Name() = %q, want %q", transport.Name(), defaultName)
	}
	if transport.server.ReadHeaderTimeout != defaultReadHeaderTimeout {
		t.Fatalf("ReadHeaderTimeout = %s, want %s", transport.server.ReadHeaderTimeout, defaultReadHeaderTimeout)
	}
	if transport.server.IdleTimeout != defaultIdleTimeout {
		t.Fatalf("IdleTimeout = %s, want %s", transport.server.IdleTimeout, defaultIdleTimeout)
	}
	if transport.server.ReadTimeout != 0 || transport.server.WriteTimeout != 0 {
		t.Fatalf("streaming-sensitive timeouts should remain unset: %+v", transport.server)
	}
}

func TestPreparePreservesCustomServerSettings(t *testing.T) {
	router := newTestRouter()
	server := &http.Server{ReadHeaderTimeout: time.Second, IdleTimeout: time.Minute}
	transport := New(Config{Addr: "127.0.0.1:0", Router: router, Server: server})
	if err := transport.Prepare(context.Background(), testDeps()); err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	t.Cleanup(func() { _ = transport.Stop(context.Background()) })

	if transport.server != server || server.Handler != router || server.Addr != "127.0.0.1:0" {
		t.Fatalf("custom server was not configured: %+v", server)
	}
	if server.ReadHeaderTimeout != time.Second || server.IdleTimeout != time.Minute {
		t.Fatalf("custom server timeouts changed: %+v", server)
	}
}

func TestPrepareRequiresRouter(t *testing.T) {
	transport := New(Config{})
	err := transport.Prepare(context.Background(), testDeps())
	if err == nil || !strings.Contains(err.Error(), "requires a router") {
		t.Fatalf("Prepare() error = %v, want missing router error", err)
	}
}

func TestPrepareBindFailureDoesNotMutateRouterOrServer(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	defer occupied.Close()

	router := newTestRouter()
	server := &http.Server{}
	module := &testModule{}
	transport := New(Config{
		Addr:   occupied.Addr().String(),
		Router: router,
		Server: server,
	})
	if err := transport.Prepare(context.Background(), testDeps(module)); err == nil {
		t.Fatal("Prepare() error = nil, want bind error")
	}
	if module.registrations != 0 || len(router.routes) != 0 {
		t.Fatalf("bind failure registered module/routes: registrations=%d routes=%v", module.registrations, router.routes)
	}
	if server.Addr != "" || server.Handler != nil {
		t.Fatalf("bind failure mutated custom server: %+v", server)
	}
}

func TestMiddlewareOrderAndShortCircuit(t *testing.T) {
	var events []string
	record := func(name string) Middleware {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				events = append(events, "before "+name)
				next.ServeHTTP(w, request)
				events = append(events, "after "+name)
			})
		}
	}
	router := newTestRouter()
	router.fallback = http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		events = append(events, "handler")
	})
	transport := New(Config{
		Addr:       "127.0.0.1:0",
		Router:     router,
		Middleware: []Middleware{record("one"), record("two")},
	})
	if err := transport.Prepare(context.Background(), testDeps()); err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	t.Cleanup(func() { _ = transport.Stop(context.Background()) })

	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://example.test/", nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}
	transport.server.Handler.ServeHTTP(newResponseRecorder(), request)
	want := []string{"before one", "before two", "handler", "after two", "after one"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("middleware events = %v, want %v", events, want)
	}

	handlerCalled := false
	shortRouter := newTestRouter()
	shortRouter.fallback = http.HandlerFunc(func(http.ResponseWriter, *http.Request) { handlerCalled = true })
	short := New(Config{
		Addr:   "127.0.0.1:0",
		Router: shortRouter,
		Middleware: []Middleware{func(http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "blocked", http.StatusUnauthorized)
			})
		}},
	})
	if err := short.Prepare(context.Background(), testDeps()); err != nil {
		t.Fatalf("short-circuit Prepare() error = %v", err)
	}
	t.Cleanup(func() { _ = short.Stop(context.Background()) })
	recorder := newResponseRecorder()
	short.server.Handler.ServeHTTP(recorder, request)
	if recorder.status != http.StatusUnauthorized || handlerCalled {
		t.Fatalf("short circuit status = %d, handler called = %v", recorder.status, handlerCalled)
	}
}

func TestHealthEndpointsExposeOnlySanitizedStatus(t *testing.T) {
	router := newTestRouter()
	transport := New(Config{Addr: "127.0.0.1:0", Router: router})
	deps := testDeps()
	deps.Health = func(context.Context) fw.HealthReport {
		return fw.HealthReport{Healthy: false, Modules: map[string]bool{"user": false}}
	}
	if err := transport.Prepare(context.Background(), deps); err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	t.Cleanup(func() { _ = transport.Stop(context.Background()) })

	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://example.test/health/ready", nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}
	recorder := newResponseRecorder()
	transport.server.Handler.ServeHTTP(recorder, request)
	if recorder.status != http.StatusServiceUnavailable {
		t.Fatalf("readiness status = %d, want %d", recorder.status, http.StatusServiceUnavailable)
	}
	var response map[string]any
	if err := json.Unmarshal(recorder.body, &response); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	modules, ok := response["modules"].(map[string]any)
	if !ok {
		t.Fatalf("readiness modules = %#v, want object", response["modules"])
	}
	user, ok := modules["user"].(map[string]any)
	if !ok || len(user) != 1 || user["status"] != "error" {
		t.Fatalf("readiness user status = %#v, want sanitized error status", modules["user"])
	}
	if _, exposed := response["error"]; exposed || strings.Contains(string(recorder.body), "secret") {
		t.Fatalf("readiness response exposed error details: %s", recorder.body)
	}
}

func TestPrepareRejectsInvalidMiddlewareBeforeRegisteringRoutes(t *testing.T) {
	tests := []struct {
		name       string
		middleware Middleware
	}{
		{name: "nil middleware"},
		{
			name: "nil handler",
			middleware: func(http.Handler) http.Handler {
				return nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := newTestRouter()
			transport := New(Config{
				Addr:       "127.0.0.1:0",
				Router:     router,
				Middleware: []Middleware{tt.middleware},
			})
			if err := transport.Prepare(context.Background(), testDeps(&testModule{})); err == nil {
				t.Fatal("Prepare() error = nil, want invalid middleware error")
			}
			if len(router.routes) != 0 {
				t.Fatalf("routes registered after invalid middleware: %v", router.routes)
			}
		})
	}
}

func TestStopForcesActiveRequestsClosedAfterDeadline(t *testing.T) {
	requestStarted := make(chan struct{})
	requestStopped := make(chan struct{})
	router := newTestRouter()
	router.Get("/block", func(_ http.ResponseWriter, request *http.Request) {
		close(requestStarted)
		<-request.Context().Done()
		close(requestStopped)
	})
	transport := New(Config{Addr: "127.0.0.1:0", Router: router})
	if err := transport.Prepare(context.Background(), testDeps()); err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	runDone := make(chan error, 1)
	go func() { runDone <- transport.Run(context.Background()) }()

	clientDone := make(chan error, 1)
	client := &http.Client{Timeout: time.Second}
	go func() {
		response, requestErr := client.Get("http://" + transport.listener.Addr().String() + "/block")
		if response != nil {
			_ = response.Body.Close()
		}
		clientDone <- requestErr
	}()
	waitForSignal(t, requestStarted, "HTTP request")

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := transport.Stop(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Stop() error = %v, want deadline exceeded", err)
	}
	waitForSignal(t, requestStopped, "forced request cancellation")
	if err := <-runDone; err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	select {
	case <-clientDone:
	case <-time.After(time.Second):
		t.Fatal("HTTP client remained blocked after forced close")
	}
}

type responseRecorder struct {
	header http.Header
	status int
	body   []byte
}

func newResponseRecorder() *responseRecorder {
	return &responseRecorder{header: make(http.Header), status: http.StatusOK}
}

func (r *responseRecorder) Header() http.Header    { return r.header }
func (r *responseRecorder) WriteHeader(status int) { r.status = status }
func (r *responseRecorder) Write(body []byte) (int, error) {
	r.body = append(r.body, body...)
	return len(body), nil
}

func waitForSignal(t *testing.T, signal <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", name)
	}
}
