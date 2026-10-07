package fw

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
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
	imports     []ModuleName
	recorder    *eventRecorder
	initStarted chan struct{}
	blockInit   bool
	runStarted  chan struct{}
	runErr      error
	stopErr     error
	closeErr    error
}

func (m *managedModule) Name() ModuleName      { return ModuleName(m.name) }
func (m *managedModule) Imports() []ModuleName { return m.imports }

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

type managedFinalizer struct {
	managedService
	finalizeStarted chan struct{}
	releaseFinalize chan struct{}
	finalizeCtxErr  chan error
	finalizeBudget  chan time.Duration
	finalizeErr     error
}

func (s *managedFinalizer) Finalize(ctx context.Context) error {
	s.recorder.add("finalize service " + s.name)
	if s.finalizeStarted != nil {
		close(s.finalizeStarted)
	}
	if s.finalizeCtxErr != nil {
		s.finalizeCtxErr <- ctx.Err()
	}
	if s.finalizeBudget != nil {
		deadline, _ := ctx.Deadline()
		s.finalizeBudget <- time.Until(deadline)
	}
	if s.releaseFinalize != nil {
		select {
		case <-s.releaseFinalize:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return s.finalizeErr
}

type lifecycleLogger struct {
	recorder *eventRecorder
}

func (l *lifecycleLogger) Info(msg string, args ...any) {
	switch msg {
	case "shutdown complete":
		l.recorder.add("log shutdown complete")
	case "application state changed":
		for i := 0; i+1 < len(args); i += 2 {
			key, keyOK := args[i].(string)
			value, valueOK := args[i+1].(string)
			if keyOK && valueOK && key == "to" && value == appStateClosed.String() {
				l.recorder.add("log state closed")
				return
			}
		}
	}
}

func (*lifecycleLogger) Error(string, ...any) {}
func (*lifecycleLogger) Debug(string, ...any) {}
func (*lifecycleLogger) Warn(string, ...any)  {}
func (l *lifecycleLogger) With(...any) Logger { return l }

func (s *managedService) Name() string               { return s.name }
func (*managedService) Health(context.Context) error { return nil }

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
	app := New(Config{Logger: discardLogger{}})
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
	app := New(Config{Logger: discardLogger{}})
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
	app := New(Config{Logger: discardLogger{}})
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

func TestTransportPreparationFailureDoesNotStartRunners(t *testing.T) {
	recorder := &eventRecorder{}
	var runnerStarted atomic.Bool
	prepareErr := errors.New("address already in use")
	module := &listenFailureModule{
		managedModule: managedModule{name: "todo", recorder: recorder},
		runnerStarted: &runnerStarted,
	}
	app := New(Config{
		Logger:     discardLogger{},
		Transports: []Transport{&managedTransport{name: "http", prepareErr: prepareErr}},
	})
	app.RegisterModules(module)

	err := app.Start(context.Background())
	if !errors.Is(err, prepareErr) {
		t.Fatalf("Start() error = %v, want transport preparation error", err)
	}
	if runnerStarted.Load() {
		t.Fatal("module runner started before transports were prepared")
	}
}

func TestContextCancellationGracefullyStopsTransports(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	httpRun := make(chan struct{})
	grpcRun := make(chan struct{})
	httpStopped := make(chan struct{})
	grpcStopped := make(chan struct{})
	app := New(Config{
		Logger: discardLogger{},
		Transports: []Transport{
			&managedTransport{name: "http", runStarted: httpRun, stopped: httpStopped},
			&managedTransport{name: "grpc", runStarted: grpcRun, stopped: grpcStopped},
		},
	})

	done := make(chan error, 1)
	go func() { done <- app.Start(ctx) }()
	waitForSignal(t, httpRun, "HTTP transport")
	waitForSignal(t, grpcRun, "gRPC transport")
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
	waitForSignal(t, httpStopped, "HTTP transport stop")
	waitForSignal(t, grpcStopped, "gRPC transport stop")
}

func TestTransportFailureStopsApplicationAndPropagates(t *testing.T) {
	runErr := errors.New("listener failed")
	stopped := make(chan struct{})
	app := New(Config{
		Logger:     discardLogger{},
		Transports: []Transport{&managedTransport{name: "http", runErr: runErr, stopped: stopped}},
	})

	err := app.Start(context.Background())
	if !errors.Is(err, runErr) {
		t.Fatalf("Start() error = %v, want transport failure", err)
	}
	waitForSignal(t, stopped, "transport stop")
}

func TestReadinessIsUnavailableWhileStopping(t *testing.T) {
	stopStarted := make(chan struct{})
	releaseStop := make(chan struct{})
	module := &blockingStopModule{
		managedModule: managedModule{name: "todo", recorder: &eventRecorder{}},
		stopStarted:   stopStarted,
		releaseStop:   releaseStop,
	}
	app := New(Config{Logger: discardLogger{}})
	app.RegisterModules(module)

	startDone := make(chan error, 1)
	go func() { startDone <- app.Start(context.Background()) }()
	waitForAppState(t, app, appStateRunning)

	stopDone := make(chan error, 1)
	go func() { stopDone <- app.Stop(context.Background()) }()
	waitForSignal(t, stopStarted, "module stop")
	if app.evaluateHealth(context.Background()).Healthy {
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

type managedTransport struct {
	name       string
	prepareErr error
	runErr     error
	runStarted chan struct{}
	stopped    chan struct{}
}

func (t *managedTransport) Name() string { return t.name }

func (t *managedTransport) Prepare(context.Context, TransportDeps) error {
	return t.prepareErr
}

func (t *managedTransport) Run(ctx context.Context) error {
	if t.runStarted != nil {
		close(t.runStarted)
	}
	if t.runErr != nil {
		return t.runErr
	}
	<-ctx.Done()
	return ctx.Err()
}

func (t *managedTransport) Stop(context.Context) error {
	if t.stopped != nil {
		close(t.stopped)
	}
	return nil
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

type blockingStopService struct {
	managedService
	stopStarted chan struct{}
}

func (s *blockingStopService) Stop(ctx context.Context) error {
	close(s.stopStarted)
	<-ctx.Done()
	return ctx.Err()
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
	app := New(Config{Logger: discardLogger{}})
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

func TestFinalizerRunsAfterLifecycleLogsAndBlocksStop(t *testing.T) {
	recorder := &eventRecorder{}
	finalizeStarted := make(chan struct{})
	releaseFinalize := make(chan struct{})
	service := &managedService{name: "postgres", recorder: recorder}
	finalizer := &managedFinalizer{
		managedService:  managedService{name: "observability", recorder: recorder},
		finalizeStarted: finalizeStarted,
		releaseFinalize: releaseFinalize,
	}
	app := New(Config{Logger: &lifecycleLogger{recorder: recorder}})
	if err := app.RegisterService(finalizer); err != nil {
		t.Fatalf("RegisterService(finalizer) error = %v", err)
	}
	if err := app.RegisterService(service); err != nil {
		t.Fatalf("RegisterService(service) error = %v", err)
	}

	startDone := make(chan error, 1)
	go func() { startDone <- app.Start(context.Background()) }()
	waitForAppState(t, app, appStateRunning)

	stopDone := make(chan error, 1)
	go func() { stopDone <- app.Stop(context.Background()) }()
	waitForSignal(t, finalizeStarted, "application service finalizer")
	waitForAppState(t, app, appStateClosed)
	select {
	case err := <-stopDone:
		t.Fatalf("Stop() returned before finalization completed: %v", err)
	default:
	}
	secondStopDone := make(chan error, 1)
	go func() { secondStopDone <- app.Stop(context.Background()) }()
	select {
	case err := <-secondStopDone:
		t.Fatalf("Stop() in closed state returned before finalization completed: %v", err)
	default:
	}
	close(releaseFinalize)

	if err := <-stopDone; err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if err := <-secondStopDone; err != nil {
		t.Fatalf("second Stop() error = %v", err)
	}
	if err := <-startDone; err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	events := recorder.snapshot()
	assertBefore(t, events, "close service postgres", "log shutdown complete")
	assertBefore(t, events, "log shutdown complete", "log state closed")
	assertBefore(t, events, "log state closed", "finalize service observability")
	assertBefore(t, events, "finalize service observability", "close service observability")
	if eventIndex(events, "stop service observability") >= 0 {
		t.Fatalf("events %v unexpectedly stop finalizer before finalization", events)
	}
}

func TestFinalizerReceivesReservedShutdownContext(t *testing.T) {
	tests := []struct {
		name                string
		shutdownTimeout     time.Duration
		finalizationTimeout time.Duration
		callerTimeout       time.Duration
		withoutFinalizer    bool
		wantWork            time.Duration
		wantReserve         time.Duration
	}{
		{name: "defaults", wantWork: 25 * time.Second, wantReserve: 5 * time.Second},
		{name: "custom", shutdownTimeout: 200 * time.Millisecond, finalizationTimeout: 80 * time.Millisecond, wantWork: 120 * time.Millisecond, wantReserve: 80 * time.Millisecond},
		{name: "short caller deadline", callerTimeout: 120 * time.Millisecond, wantWork: 60 * time.Millisecond, wantReserve: 60 * time.Millisecond},
		{name: "oversized reserve", shutdownTimeout: 200 * time.Millisecond, finalizationTimeout: time.Second, wantWork: 100 * time.Millisecond, wantReserve: 100 * time.Millisecond},
		{name: "no finalizers", shutdownTimeout: 200 * time.Millisecond, withoutFinalizer: true, wantWork: 200 * time.Millisecond},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				recorder := &eventRecorder{}
				slowService := &blockingStopService{
					managedService: managedService{name: "river", recorder: recorder},
					stopStarted:    make(chan struct{}),
				}
				finalizer := &managedFinalizer{
					managedService: managedService{name: "observability", recorder: recorder},
					finalizeCtxErr: make(chan error, 1),
					finalizeBudget: make(chan time.Duration, 1),
				}
				app := New(Config{
					Logger:              discardLogger{},
					ShutdownTimeout:     tt.shutdownTimeout,
					FinalizationTimeout: tt.finalizationTimeout,
				})
				if err := app.RegisterService(slowService); err != nil {
					t.Fatal(err)
				}
				if !tt.withoutFinalizer {
					if err := app.RegisterService(finalizer); err != nil {
						t.Fatal(err)
					}
				}

				startDone := make(chan error, 1)
				go func() { startDone <- app.Start(t.Context()) }()
				synctest.Wait()
				ctx := t.Context()
				if tt.callerTimeout > 0 {
					var cancel context.CancelFunc
					ctx, cancel = context.WithTimeout(ctx, tt.callerTimeout)
					defer cancel()
				}

				startedAt := time.Now()
				if err := app.Stop(ctx); !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("Stop() error = %v, want shutdown deadline error", err)
				}
				if err := <-startDone; !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("Start() error = %v, want shutdown deadline error", err)
				}
				if elapsed := time.Since(startedAt); elapsed != tt.wantWork {
					t.Fatalf("shutdown work took %s, want %s", elapsed, tt.wantWork)
				}
				if !tt.withoutFinalizer {
					if err := <-finalizer.finalizeCtxErr; err != nil {
						t.Fatalf("Finalize() context error = %v, want active context", err)
					}
					if budget := <-finalizer.finalizeBudget; budget != tt.wantReserve {
						t.Fatalf("Finalize() budget = %s, want %s", budget, tt.wantReserve)
					}
				}
			})
		})
	}
}

func TestFinalizationTimeoutCapsTerminalPhase(t *testing.T) {
	tests := []struct {
		name            string
		shutdownTimeout time.Duration
		callerTimeout   time.Duration
		wantDuration    time.Duration
	}{
		{name: "finalizer cap", shutdownTimeout: time.Second, wantDuration: 40 * time.Millisecond},
		{name: "short caller deadline", shutdownTimeout: time.Second, callerTimeout: 20 * time.Millisecond, wantDuration: 20 * time.Millisecond},
		{name: "short shutdown timeout", shutdownTimeout: 20 * time.Millisecond, wantDuration: 20 * time.Millisecond},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				recorder := &eventRecorder{}
				ctxErrors := make(chan error, 2)
				app := New(Config{
					Logger:              discardLogger{},
					ShutdownTimeout:     tt.shutdownTimeout,
					FinalizationTimeout: 40 * time.Millisecond,
				})
				for _, name := range []string{"logs", "traces"} {
					finalizer := &managedFinalizer{
						managedService:  managedService{name: name, recorder: recorder},
						releaseFinalize: make(chan struct{}),
						finalizeCtxErr:  ctxErrors,
					}
					if err := app.RegisterService(finalizer); err != nil {
						t.Fatal(err)
					}
				}

				startDone := make(chan error, 1)
				go func() { startDone <- app.Start(t.Context()) }()
				synctest.Wait()
				ctx := t.Context()
				if tt.callerTimeout > 0 {
					var cancel context.CancelFunc
					ctx, cancel = context.WithTimeout(ctx, tt.callerTimeout)
					defer cancel()
				}

				startedAt := time.Now()
				if err := app.Stop(ctx); !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("Stop() error = %v, want finalization deadline error", err)
				}
				if err := <-startDone; !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("Start() error = %v, want finalization deadline error", err)
				}
				if elapsed := time.Since(startedAt); elapsed != tt.wantDuration {
					t.Fatalf("Stop() elapsed = %s, want shared %s finalization budget", elapsed, tt.wantDuration)
				}
				if err := <-ctxErrors; err != nil {
					t.Fatalf("first Finalize() context error = %v, want nil", err)
				}
				if err := <-ctxErrors; !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("second Finalize() context error = %v, want deadline exceeded", err)
				}
				events := recorder.snapshot()
				assertContains(t, events, "close service logs")
				assertContains(t, events, "close service traces")
			})
		})
	}
}

func TestStartupFailureReservesFinalizationBudget(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		recorder := &eventRecorder{}
		prepareErr := errors.New("listen failed")
		module := &blockingStopModule{
			managedModule: managedModule{name: "todo", recorder: recorder},
			stopStarted:   make(chan struct{}),
		}
		finalizer := &managedFinalizer{
			managedService: managedService{name: "observability", recorder: recorder},
			finalizeCtxErr: make(chan error, 1),
			finalizeBudget: make(chan time.Duration, 1),
		}
		app := New(Config{
			Logger:              discardLogger{},
			Transports:          []Transport{&managedTransport{name: "http", prepareErr: prepareErr}},
			ShutdownTimeout:     200 * time.Millisecond,
			FinalizationTimeout: 80 * time.Millisecond,
		})
		app.RegisterModules(module)
		if err := app.RegisterService(finalizer); err != nil {
			t.Fatal(err)
		}

		startedAt := time.Now()
		err := app.Start(t.Context())
		if !errors.Is(err, prepareErr) || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Start() error = %v, want preparation and stop errors", err)
		}
		if elapsed := time.Since(startedAt); elapsed != 120*time.Millisecond {
			t.Fatalf("startup cleanup took %s, want 120ms", elapsed)
		}
		if err := <-finalizer.finalizeCtxErr; err != nil {
			t.Fatalf("Finalize() context error = %v, want active context", err)
		}
		if budget := <-finalizer.finalizeBudget; budget != 80*time.Millisecond {
			t.Fatalf("Finalize() budget = %s, want 80ms", budget)
		}
		assertBefore(t, recorder.snapshot(), "close module todo", "finalize service observability")
	})
}

func TestStopCancellationReachesFinalizer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		finalizer := &managedFinalizer{
			managedService:  managedService{name: "observability", recorder: &eventRecorder{}},
			finalizeStarted: make(chan struct{}),
			releaseFinalize: make(chan struct{}),
		}
		app := New(Config{Logger: discardLogger{}})
		if err := app.RegisterService(finalizer); err != nil {
			t.Fatal(err)
		}
		startDone := make(chan error, 1)
		go func() { startDone <- app.Start(t.Context()) }()
		synctest.Wait()
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		stopDone := make(chan error, 1)
		go func() { stopDone <- app.Stop(ctx) }()
		<-finalizer.finalizeStarted
		cancel()

		if err := <-stopDone; !errors.Is(err, context.Canceled) {
			t.Fatalf("Stop() error = %v, want cancellation", err)
		}
		if err := <-startDone; !errors.Is(err, context.Canceled) {
			t.Fatalf("Start() error = %v, want cancellation", err)
		}
		assertContains(t, finalizer.recorder.snapshot(), "close service observability")
	})
}

func TestFinalizerErrorsAreAggregated(t *testing.T) {
	finalizeErr := errors.New("flush failed")
	closeErr := errors.New("exporter close failed")
	service := &managedFinalizer{
		managedService: managedService{
			name:     "observability",
			recorder: &eventRecorder{},
			closeErr: closeErr,
		},
		finalizeErr:    finalizeErr,
		finalizeCtxErr: make(chan error, 1),
	}
	app := New(Config{Logger: discardLogger{}})
	if err := app.RegisterService(service); err != nil {
		t.Fatalf("RegisterService() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := app.Start(ctx)
	for _, want := range []error{finalizeErr, closeErr} {
		if !errors.Is(err, want) {
			t.Errorf("Start() error = %v, missing %v", err, want)
		}
	}
	if err := <-service.finalizeCtxErr; err != nil {
		t.Fatalf("Finalize() context error = %v, want fresh shutdown context after startup cancellation", err)
	}
}

func TestLifecycleLogsStateTransitions(t *testing.T) {
	logger := &healthTestLogger{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	app := New(Config{Logger: logger})

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
	app := New(Config{Logger: discardLogger{}})
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
