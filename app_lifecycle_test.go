package fw

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
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

func TestStartClosesResourcesAfterModuleInitFailure(t *testing.T) {
	var events []string
	initialized := &lifecycleModule{name: "todo", events: &events}
	failing := &lifecycleModule{name: "auth", initErr: errors.New("broken wiring"), events: &events}
	pending := &lifecycleModule{name: "user", events: &events}

	app := New(WithLogger(discardLogger{}))
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

	app := New(WithLogger(discardLogger{}))
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
	var transport *lifecycleTransport
	app := New(WithTransport(transport), WithLogger(discardLogger{}))
	if err := app.RegisterService(&lifecycleService{name: "postgres", events: &events}); err != nil {
		t.Fatalf("RegisterService() error = %v", err)
	}

	err := app.Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), "transport 0 is nil") {
		t.Fatalf("Start() error = %v, want nil transport error", err)
	}
	want := []string{"close service postgres"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("lifecycle events = %v, want %v", events, want)
	}
}

func TestStartClosesResourcesAfterTransportPreparationFailure(t *testing.T) {
	var events []string
	service := &lifecycleService{name: "postgres", events: &events}
	module := &lifecycleModule{name: "user", events: &events}
	prepareErr := errors.New("address already in use")
	app := New(
		WithTransport(&lifecycleTransport{prepareErr: prepareErr}),
		WithLogger(discardLogger{}),
	)
	if err := app.RegisterService(service); err != nil {
		t.Fatalf("RegisterService() error = %v", err)
	}
	app.RegisterModules(module)

	err := app.Start(context.Background())
	if !errors.Is(err, prepareErr) {
		t.Fatalf("Start() error = %v, want transport preparation error", err)
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

func TestStartStopsOnlySuccessfullyPreparedTransports(t *testing.T) {
	var events []string
	prepareErr := errors.New("address already in use")
	first := &recordingTransport{name: "http", events: &events}
	second := &recordingTransport{name: "grpc", events: &events, prepareErr: prepareErr}
	app := New(
		WithTransport(first),
		WithTransport(second),
		WithLogger(discardLogger{}),
	)

	err := app.Start(context.Background())
	if !errors.Is(err, prepareErr) {
		t.Fatalf("Start() error = %v, want transport preparation error", err)
	}
	want := []string{
		"prepare transport http",
		"prepare transport grpc",
		"stop transport http",
	}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("transport events = %v, want %v", events, want)
	}
}

type lifecycleTransport struct {
	prepareErr error
}

func (*lifecycleTransport) Name() string { return "test" }

func (t *lifecycleTransport) Prepare(context.Context, TransportDeps) error {
	return t.prepareErr
}

func (*lifecycleTransport) Run(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}

func (*lifecycleTransport) Stop(context.Context) error { return nil }

type recordingTransport struct {
	name       string
	events     *[]string
	prepareErr error
}

func (t *recordingTransport) Name() string { return t.name }

func (t *recordingTransport) Prepare(context.Context, TransportDeps) error {
	*t.events = append(*t.events, "prepare transport "+t.name)
	return t.prepareErr
}

func (*recordingTransport) Run(context.Context) error { return nil }

func (t *recordingTransport) Stop(context.Context) error {
	*t.events = append(*t.events, "stop transport "+t.name)
	return nil
}
