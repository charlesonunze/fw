package fw

import (
	"context"
	"errors"
	"net"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type eventRecorder struct {
	mu     sync.Mutex
	events []string
}

func (r *eventRecorder) add(event string) {
	r.mu.Lock()
	r.events = append(r.events, event)
	r.mu.Unlock()
}

func (r *eventRecorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.events...)
}

type managedModule struct {
	name        string
	recorder    *eventRecorder
	initStarted chan struct{}
	blockInit   bool
	runStarted  chan struct{}
	runErr      error
	stopErr     error
	closeErr    error
}

func (m *managedModule) Name() string { return m.name }

func (m *managedModule) Register(*Deps) error {
	m.recorder.add("register module " + m.name)
	return nil
}

func (m *managedModule) Init(ctx context.Context, _ *Deps) error {
	m.recorder.add("init module " + m.name)
	if m.initStarted != nil {
		close(m.initStarted)
	}
	if !m.blockInit {
		return nil
	}
	<-ctx.Done()
	m.recorder.add("init cancelled module " + m.name)
	return ctx.Err()
}

func (*managedModule) Health(context.Context) error { return nil }

func (m *managedModule) Run(ctx context.Context) error {
	m.recorder.add("run module " + m.name)
	if m.runStarted != nil {
		close(m.runStarted)
	}
	if m.runErr != nil {
		return m.runErr
	}
	<-ctx.Done()
	m.recorder.add("runner stopped module " + m.name)
	return ctx.Err()
}

func (m *managedModule) Stop(context.Context) error {
	m.recorder.add("stop module " + m.name)
	return m.stopErr
}

func (m *managedModule) Close() error {
	m.recorder.add("close module " + m.name)
	return m.closeErr
}

type managedService struct {
	name       string
	recorder   *eventRecorder
	runStarted chan struct{}
	stopErr    error
	closeErr   error
}

func (s *managedService) Name() string { return s.name }

func (s *managedService) Run(ctx context.Context) error {
	s.recorder.add("run service " + s.name)
	if s.runStarted != nil {
		close(s.runStarted)
	}
	<-ctx.Done()
	s.recorder.add("runner stopped service " + s.name)
	return ctx.Err()
}

func (s *managedService) Stop(context.Context) error {
	s.recorder.add("stop service " + s.name)
	return s.stopErr
}

func (s *managedService) Close() error {
	s.recorder.add("close service " + s.name)
	return s.closeErr
}

func TestStartAndStopCoordinateComponentLifecycle(t *testing.T) {
	recorder := &eventRecorder{}
	moduleStarted := make(chan struct{})
	serviceStarted := make(chan struct{})
	module := &managedModule{name: "todo", recorder: recorder, runStarted: moduleStarted}
	service := &managedService{name: "postgres", recorder: recorder, runStarted: serviceStarted}
	app := New(WithLogger(discardLogger{}))
	if err := app.RegisterService(service); err != nil {
		t.Fatalf("RegisterService() error = %v", err)
	}
	app.RegisterModules(module)

	startDone := make(chan error, 1)
	go func() { startDone <- app.Start(context.Background()) }()
	waitForSignal(t, moduleStarted, "module runner")
	waitForSignal(t, serviceStarted, "service runner")

	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := app.Stop(stopCtx); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if err := <-startDone; err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := app.Stop(context.Background()); err != nil {
		t.Fatalf("second Stop() error = %v", err)
	}

	events := recorder.snapshot()
	assertBefore(t, events, "stop module todo", "stop service postgres")
	assertBefore(t, events, "runner stopped module todo", "stop service postgres")
	assertBefore(t, events, "stop service postgres", "close module todo")
	assertBefore(t, events, "runner stopped service postgres", "close module todo")
	assertBefore(t, events, "close module todo", "close service postgres")
}

func TestRunnerFailureStopsApplicationAndPropagates(t *testing.T) {
	runErr := errors.New("consumer disconnected")
	recorder := &eventRecorder{}
	module := &managedModule{name: "notification", recorder: recorder, runErr: runErr}
	service := &managedService{name: "rabbitmq", recorder: recorder}
	app := New(WithLogger(discardLogger{}))
	if err := app.RegisterService(service); err != nil {
		t.Fatalf("RegisterService() error = %v", err)
	}
	app.RegisterModules(module)

	err := app.Start(context.Background())
	if !errors.Is(err, runErr) {
		t.Fatalf("Start() error = %v, want runner error", err)
	}
	if stopErr := app.Stop(context.Background()); !errors.Is(stopErr, runErr) {
		t.Fatalf("Stop() after failure error = %v, want runner error", stopErr)
	}

	events := recorder.snapshot()
	assertContains(t, events, "stop module notification")
	assertContains(t, events, "stop service rabbitmq")
	assertBefore(t, events, "close module notification", "close service rabbitmq")
}

func TestStopDuringStartupCancelsModuleInit(t *testing.T) {
	recorder := &eventRecorder{}
	initStarted := make(chan struct{})
	module := &managedModule{
		name:        "todo",
		recorder:    recorder,
		initStarted: initStarted,
		blockInit:   true,
	}
	app := New(WithLogger(discardLogger{}))
	app.RegisterModules(module)

	startDone := make(chan error, 1)
	go func() { startDone <- app.Start(context.Background()) }()
	waitForSignal(t, initStarted, "module initialization")

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := app.Stop(ctx); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if err := <-startDone; err != nil {
		t.Fatalf("Start() error = %v, want graceful startup cancellation", err)
	}

	events := recorder.snapshot()
	assertBefore(t, events, "init cancelled module todo", "stop module todo")
	assertBefore(t, events, "stop module todo", "close module todo")
}

func TestTransportListenFailureDoesNotStartRunners(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	recorder := &eventRecorder{}
	var runnerStarted atomic.Bool
	module := &listenFailureModule{
		managedModule: managedModule{name: "todo", recorder: recorder},
		runnerStarted: &runnerStarted,
	}
	app := New(
		WithHTTP(HTTPConfig{Addr: listener.Addr().String(), Router: &noopRouter{}}),
		WithLogger(discardLogger{}),
	)
	app.RegisterModules(module)

	err = app.Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), "failed to listen on HTTP addr") {
		t.Fatalf("Start() error = %v, want HTTP listen error", err)
	}
	if runnerStarted.Load() {
		t.Fatal("module runner started before transports were prepared")
	}
}

func TestContextCancellationGracefullyStopsTransports(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	app := New(
		WithHTTP(HTTPConfig{Addr: "127.0.0.1:0", Router: &noopRouter{}}),
		WithGRPC(GRPCConfig{Addr: "127.0.0.1:0"}),
		WithLogger(discardLogger{}),
	)

	done := make(chan error, 1)
	go func() { done <- app.Start(ctx) }()
	waitForAppState(t, app, appStateRunning)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Start() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("transports did not stop after context cancellation")
	}
}

func TestReadinessIsUnavailableWhileStopping(t *testing.T) {
	stopStarted := make(chan struct{})
	releaseStop := make(chan struct{})
	module := &blockingStopModule{
		managedModule: managedModule{name: "todo", recorder: &eventRecorder{}},
		stopStarted:   stopStarted,
		releaseStop:   releaseStop,
	}
	app := New(WithLogger(discardLogger{}))
	app.RegisterModules(module)

	startDone := make(chan error, 1)
	go func() { startDone <- app.Start(context.Background()) }()
	waitForAppState(t, app, appStateRunning)

	stopDone := make(chan error, 1)
	go func() { stopDone <- app.Stop(context.Background()) }()
	waitForSignal(t, stopStarted, "module stop")
	if app.evaluateHealth(context.Background()).healthy {
		t.Fatal("application remained ready while stopping")
	}
	close(releaseStop)
	if err := <-stopDone; err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if err := <-startDone; err != nil {
		t.Fatalf("Start() error = %v", err)
	}
}

type listenFailureModule struct {
	managedModule
	runnerStarted *atomic.Bool
}

func (m *listenFailureModule) Run(context.Context) error {
	m.runnerStarted.Store(true)
	return nil
}

type blockingStopModule struct {
	managedModule
	stopStarted chan struct{}
	releaseStop chan struct{}
}

func (m *blockingStopModule) Stop(ctx context.Context) error {
	close(m.stopStarted)
	select {
	case <-m.releaseStop:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestLifecycleAggregatesStopAndCloseErrors(t *testing.T) {
	runErr := errors.New("runner failed")
	moduleStopErr := errors.New("module stop failed")
	moduleCloseErr := errors.New("module close failed")
	serviceStopErr := errors.New("service stop failed")
	serviceCloseErr := errors.New("service close failed")
	recorder := &eventRecorder{}
	module := &managedModule{
		name: "todo", recorder: recorder, runErr: runErr,
		stopErr: moduleStopErr, closeErr: moduleCloseErr,
	}
	service := &managedService{
		name: "postgres", recorder: recorder,
		stopErr: serviceStopErr, closeErr: serviceCloseErr,
	}
	app := New(WithLogger(discardLogger{}))
	if err := app.RegisterService(service); err != nil {
		t.Fatalf("RegisterService() error = %v", err)
	}
	app.RegisterModules(module)

	err := app.Start(context.Background())
	for _, want := range []error{runErr, moduleStopErr, moduleCloseErr, serviceStopErr, serviceCloseErr} {
		if !errors.Is(err, want) {
			t.Errorf("Start() error = %v, missing %v", err, want)
		}
	}
}

func TestLifecycleLogsStateTransitions(t *testing.T) {
	logger := &healthTestLogger{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	app := New(WithLogger(logger))

	if err := app.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	var transitions [][2]string
	for _, entry := range logger.snapshot() {
		if entry.msg != "application state changed" {
			continue
		}
		values := map[string]string{}
		for i := 0; i+1 < len(entry.args); i += 2 {
			key, ok := entry.args[i].(string)
			if ok {
				values[key] = entry.args[i+1].(string)
			}
		}
		transitions = append(transitions, [2]string{values["from"], values["to"]})
	}
	want := [][2]string{{"new", "starting"}, {"starting", "stopping"}, {"stopping", "closed"}}
	if !reflect.DeepEqual(transitions, want) {
		t.Fatalf("state transitions = %v, want %v", transitions, want)
	}
}

func TestLifecycleRejectsInvalidCalls(t *testing.T) {
	app := New(WithLogger(discardLogger{}))
	if err := app.Stop(context.Background()); err == nil || !strings.Contains(err.Error(), "has not been started") {
		t.Fatalf("Stop() before Start error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := app.Start(ctx); err != nil {
		t.Fatalf("first Start() error = %v", err)
	}
	if err := app.Start(context.Background()); err == nil || !strings.Contains(err.Error(), "cannot start from closed state") {
		t.Fatalf("second Start() error = %v", err)
	}
}

func waitForSignal(t *testing.T, signal <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", name)
	}
}

func waitForAppState(t *testing.T, app *App, want appState) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		app.lifecycleMu.Lock()
		state := app.state
		app.lifecycleMu.Unlock()
		if state == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("application did not reach %s state", want)
}

func assertContains(t *testing.T, events []string, want string) {
	t.Helper()
	if eventIndex(events, want) < 0 {
		t.Fatalf("events %v do not contain %q", events, want)
	}
}

func assertBefore(t *testing.T, events []string, first, second string) {
	t.Helper()
	firstIndex := eventIndex(events, first)
	secondIndex := eventIndex(events, second)
	if firstIndex < 0 || secondIndex < 0 || firstIndex >= secondIndex {
		t.Fatalf("events %v do not place %q before %q", events, first, second)
	}
}

func eventIndex(events []string, target string) int {
	for i, event := range events {
		if event == target {
			return i
		}
	}
	return -1
}
