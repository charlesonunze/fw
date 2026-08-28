package fw

import (
	"context"
	"errors"
	"net"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"google.golang.org/grpc"
)

type lifecycleService struct {
	name   string
	events *[]string
}

func (s *lifecycleService) Name() string { return s.name }

func (s *lifecycleService) Close() error {
	*s.events = append(*s.events, "close service "+s.name)
	return nil
}

type lifecycleModule struct {
	name        string
	registerErr error
	initErr     error
	events      *[]string
}

type lifecycleGRPCModule struct {
	*lifecycleModule
}

func (*lifecycleGRPCModule) RegisterGRPC(*grpc.Server) {}

func (m *lifecycleModule) Name() string { return m.name }

func (m *lifecycleModule) Register(*Deps) error {
	*m.events = append(*m.events, "register module "+m.name)
	return m.registerErr
}

func (m *lifecycleModule) Init(context.Context, *Deps) error {
	*m.events = append(*m.events, "init module "+m.name)
	return m.initErr
}

func (*lifecycleModule) Health(context.Context) error { return nil }

func (m *lifecycleModule) Close() error {
	*m.events = append(*m.events, "close module "+m.name)
	return nil
}

type discardLogger struct{}

func (discardLogger) Info(string, ...any)  {}
func (discardLogger) Error(string, ...any) {}
func (discardLogger) Debug(string, ...any) {}
func (discardLogger) Warn(string, ...any)  {}
func (l discardLogger) With(...any) Logger { return l }

type noopRouter struct{}

func (*noopRouter) ServeHTTP(http.ResponseWriter, *http.Request)              {}
func (*noopRouter) Get(string, http.HandlerFunc)                              {}
func (*noopRouter) Post(string, http.HandlerFunc)                             {}
func (*noopRouter) Put(string, http.HandlerFunc)                              {}
func (*noopRouter) Delete(string, http.HandlerFunc)                           {}
func (*noopRouter) Patch(string, http.HandlerFunc)                            {}
func (*noopRouter) Handle(string, string, http.HandlerFunc)                   {}
func (r *noopRouter) Group(string, ...func(http.Handler) http.Handler) Router { return r }
func (*noopRouter) Use(...func(http.Handler) http.Handler)                    {}
func (*noopRouter) Mount(string, http.Handler)                                {}

func TestStartClosesResourcesAfterModuleInitFailure(t *testing.T) {
	var events []string
	initialized := &lifecycleModule{name: "todo", events: &events}
	failing := &lifecycleModule{name: "auth", initErr: errors.New("broken wiring"), events: &events}
	pending := &lifecycleModule{name: "user", events: &events}

	app := New(WithHTTP(HTTPConfig{Router: &noopRouter{}}), WithLogger(discardLogger{}))
	if err := app.RegisterService(&lifecycleService{name: "postgres", events: &events}); err != nil {
		t.Fatalf("RegisterService(postgres) error = %v", err)
	}
	if err := app.RegisterService(&lifecycleService{name: "rabbitmq", events: &events}); err != nil {
		t.Fatalf("RegisterService(rabbitmq) error = %v", err)
	}
	app.RegisterModules(initialized, failing, pending)

	err := app.Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), `failed to initialize module "auth"`) {
		t.Fatalf("Start() error = %v, want auth initialization error", err)
	}
	want := []string{
		"register module todo",
		"register module auth",
		"register module user",
		"init module todo",
		"init module auth",
		"close module user",
		"close module auth",
		"close module todo",
		"close service rabbitmq",
		"close service postgres",
	}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("lifecycle events = %v, want %v", events, want)
	}
}

func TestStartClosesResourcesAfterModuleRegistrationFailure(t *testing.T) {
	var events []string
	registered := &lifecycleModule{name: "todo", events: &events}
	failing := &lifecycleModule{name: "auth", registerErr: errors.New("duplicate service"), events: &events}
	skipped := &lifecycleModule{name: "user", events: &events}

	app := New(WithHTTP(HTTPConfig{Router: &noopRouter{}}), WithLogger(discardLogger{}))
	if err := app.RegisterService(&lifecycleService{name: "postgres", events: &events}); err != nil {
		t.Fatalf("RegisterService() error = %v", err)
	}
	app.RegisterModules(registered, failing, skipped)

	err := app.Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), `failed to register module "auth"`) {
		t.Fatalf("Start() error = %v, want auth registration error", err)
	}

	want := []string{
		"register module todo",
		"register module auth",
		"close module auth",
		"close module todo",
		"close service postgres",
	}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("lifecycle events = %v, want %v", events, want)
	}
}

func TestStartClosesPreRegisteredServicesAfterSetupFailure(t *testing.T) {
	var events []string
	app := New(WithHTTP(HTTPConfig{}), WithLogger(discardLogger{}))
	if err := app.RegisterService(&lifecycleService{name: "postgres", events: &events}); err != nil {
		t.Fatalf("RegisterService() error = %v", err)
	}

	err := app.Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), "HTTP transport requires a router") {
		t.Fatalf("Start() error = %v, want missing router error", err)
	}
	want := []string{"close service postgres"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("lifecycle events = %v, want %v", events, want)
	}
}

func TestStartClosesResourcesAfterGRPCListenFailure(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	var events []string
	service := &lifecycleService{name: "postgres", events: &events}
	module := &lifecycleGRPCModule{lifecycleModule: &lifecycleModule{name: "user", events: &events}}
	app := New(
		WithHTTP(HTTPConfig{Addr: "127.0.0.1:0", Router: &noopRouter{}}),
		WithGRPC(GRPCConfig{Addr: listener.Addr().String()}),
		WithLogger(discardLogger{}),
	)
	if err := app.RegisterService(service); err != nil {
		t.Fatalf("RegisterService() error = %v", err)
	}
	app.RegisterModules(module)

	err = app.Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), "failed to listen on gRPC addr") {
		t.Fatalf("Start() error = %v, want gRPC listen error", err)
	}
	want := []string{
		"register module user",
		"init module user",
		"close module user",
		"close service postgres",
	}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("lifecycle events = %v, want %v", events, want)
	}
}
