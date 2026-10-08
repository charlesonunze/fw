package fw

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

type controlledService struct {
	graphService
	run func(context.Context) error
}

func (s *controlledService) Run(ctx context.Context) error { return s.run(ctx) }

type controlledModule struct {
	managedModule
	run func(context.Context) error
}

func (m *controlledModule) Run(ctx context.Context) error { return m.run(ctx) }

type controlledTransport struct {
	managedTransport
	run     func(context.Context) error
	stopErr error
}

func (t *controlledTransport) Run(ctx context.Context) error { return t.run(ctx) }
func (t *controlledTransport) Stop(context.Context) error    { return t.stopErr }

func TestShutdownAbortsUnfinishedWork(t *testing.T) {
	for _, kind := range []string{"transport", "module", "service", "module without Stop", "service without Stop", "finalizer runner"} {
		t.Run(kind, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				recorder := &eventRecorder{}
				started := make(chan struct{})
				release := make(chan struct{})
				run := func(context.Context) error {
					close(started)
					<-release
					return nil
				}
				app := New(Config{
					Logger:          &lifecycleLogger{recorder: recorder},
					ShutdownTimeout: 100 * time.Millisecond, FinalizationTimeout: 20 * time.Millisecond,
				})
				registerGraphService(t, app, &graphService{name: "postgres", recorder: recorder}, As[graphDatabase]())
				registerGraphService(t, app, &managedFinalizer{
					managedService: managedService{name: "observability", recorder: recorder},
				})
				module := &controlledModule{
					managedModule: managedModule{name: "todo", imports: []ModuleName{"user"}, recorder: recorder},
					run:           run,
				}
				service := &controlledService{graphService: graphService{name: "river", recorder: recorder}, run: run}
				switch kind {
				case "transport":
					app.transports = []Transport{&controlledTransport{managedTransport: managedTransport{name: "http"}, run: run}}
				case "module":
					app.RegisterModules(module)
				case "module without Stop":
					app.RegisterModules(struct {
						Module
						Runner
					}{Module: module, Runner: module})
				case "service":
					registerGraphService(t, app, service, DependsOn[graphDatabase]())
				case "service without Stop":
					registerGraphService(t, app, struct {
						Service
						Runner
					}{Service: service, Runner: service}, DependsOn[graphDatabase]())
				case "finalizer runner":
					registerGraphService(t, app, struct {
						Service
						Runner
						Finalizer
					}{Service: service, Runner: service, Finalizer: &graphFinalizer{graphService: service.graphService}})
				}
				app.RegisterModules(&managedModule{name: "user", recorder: recorder})

				startDone := make(chan error, 1)
				go func() { startDone <- app.Start(t.Context()) }()
				<-started
				synctest.Wait()
				stopDone := make(chan error, 8)
				for range cap(stopDone) {
					go func() { stopDone <- app.Stop(t.Context()) }()
				}
				startErr := <-startDone
				if !errors.Is(startErr, ErrShutdownIncomplete) || !errors.Is(startErr, context.DeadlineExceeded) {
					t.Errorf("shutdown error = %v, want incomplete shutdown and deadline", startErr)
				}
				for range cap(stopDone) {
					if err := <-stopDone; err != startErr {
						t.Errorf("concurrent Stop() error = %v, want the original result %v", err, startErr)
					}
				}
				if app.state != appStateAborted || app.evaluateHealth(t.Context()).Healthy {
					t.Errorf("aborted app state = %s, ready = %t", app.state, app.ready.Load())
				}
				assertNoShutdownCleanup(t, recorder.snapshot())
				assertContains(t, recorder.snapshot(), "log shutdown incomplete")
				assertContains(t, recorder.snapshot(), "log state aborted")
				if eventIndex(recorder.snapshot(), "stop service postgres") >= 0 {
					t.Error("stopped a dependency while its consumer was still running")
				}
				if strings.HasPrefix(kind, "module") || kind == "transport" {
					if eventIndex(recorder.snapshot(), "stop module user") >= 0 {
						t.Error("stopped an imported module while its consumer was still running")
					}
				}
				if err := app.Stop(t.Context()); err != startErr {
					t.Errorf("repeated Stop() error = %v, want the original result %v", err, startErr)
				}

				close(release)
				synctest.Wait()
				before := recorder.snapshot()
				if err := app.Stop(t.Context()); err != startErr {
					t.Errorf("Stop() after late runner exit = %v, want %v", err, startErr)
				}
				if !reflect.DeepEqual(before, recorder.snapshot()) {
					t.Error("Stop() retried cleanup after an aborted shutdown")
				}
				if err := app.Start(t.Context()); err == nil || !strings.Contains(err.Error(), "aborted state") {
					t.Errorf("Start() after abort = %v, want rejected restart", err)
				}
			})
		})
	}
}

func TestShutdownWaitsBeforeStoppingDependencies(t *testing.T) {
	for _, kind := range []string{"module", "service"} {
		t.Run(kind, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				recorder := &eventRecorder{}
				started := make(chan struct{})
				release := make(chan struct{})
				run := func(context.Context) error {
					close(started)
					<-release
					recorder.add("consumer finished")
					return nil
				}
				app := New(Config{Logger: discardLogger{}})
				dependency := "stop service postgres"
				if kind == "service" {
					registerGraphService(t, app, &controlledService{graphService: graphService{name: "river", recorder: recorder}, run: run}, DependsOn[graphDatabase]())
					registerGraphService(t, app, &graphService{name: "cache", recorder: recorder}, DependsOn[graphDatabase]())
					registerGraphService(t, app, &graphService{name: "postgres", recorder: recorder}, As[graphDatabase]())
				} else {
					dependency = "stop module user"
					app.RegisterModules(
						&controlledModule{managedModule: managedModule{name: "todo", imports: []ModuleName{"user"}, recorder: recorder}, run: run},
						&managedModule{name: "auth", imports: []ModuleName{"user"}, recorder: recorder},
						&managedModule{name: "user", recorder: recorder},
					)
				}
				startDone := make(chan error, 1)
				go func() { startDone <- app.Start(t.Context()) }()
				<-started
				stopDone := make(chan error, 1)
				go func() { stopDone <- app.Stop(t.Context()) }()
				synctest.Wait()
				if eventIndex(recorder.snapshot(), dependency) >= 0 {
					t.Error("dependency stopped before its consumer finished")
				}
				close(release)
				for _, err := range []error{<-stopDone, <-startDone} {
					if err != nil {
						t.Errorf("shutdown error = %v", err)
					}
				}
				assertBefore(t, recorder.snapshot(), "consumer finished", dependency)
			})
		})
	}
}

func TestShutdownAbortsStopErrors(t *testing.T) {
	for _, kind := range []string{"transport", "module", "service", "service without Run"} {
		t.Run(kind, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				recorder := &eventRecorder{}
				stopErr := errors.New("stop failed")
				app := New(Config{Logger: discardLogger{}})
				registerGraphService(t, app, &graphService{name: "postgres", recorder: recorder}, As[graphDatabase]())
				registerGraphService(t, app, &managedFinalizer{managedService: managedService{name: "observability", recorder: recorder}})
				app.RegisterModules(&managedModule{name: "user", recorder: recorder})
				switch kind {
				case "transport":
					app.transports = []Transport{&controlledTransport{
						managedTransport: managedTransport{name: "http"}, stopErr: stopErr,
						run: func(ctx context.Context) error { <-ctx.Done(); return nil },
					}}
				case "module":
					app.RegisterModules(&managedModule{name: "todo", imports: []ModuleName{"user"}, recorder: recorder, stopErr: stopErr})
				case "service", "service without Run":
					service := &graphService{name: "river", recorder: recorder, stopErr: stopErr}
					var registered Service = service
					if kind == "service without Run" {
						registered = struct {
							Service
							Stopper
						}{Service: service, Stopper: service}
					}
					registerGraphService(t, app, registered, DependsOn[graphDatabase]())
				}
				startDone := make(chan error, 1)
				go func() { startDone <- app.Start(t.Context()) }()
				synctest.Wait()
				for _, err := range []error{app.Stop(t.Context()), <-startDone} {
					if !errors.Is(err, ErrShutdownIncomplete) || !errors.Is(err, stopErr) {
						t.Errorf("shutdown error = %v, want stop error and incomplete shutdown", err)
					}
				}
				synctest.Wait()
				assertNoShutdownCleanup(t, recorder.snapshot())
				if eventIndex(recorder.snapshot(), "stop service postgres") >= 0 {
					t.Error("stopped dependency after failed consumer stop")
				}
			})
		})
	}
}

func TestStartupTransportStopFailureAbortsCleanup(t *testing.T) {
	prepareErr := errors.New("prepare failed")
	stopErr := errors.New("stop failed")
	recorder := &eventRecorder{}
	app := New(Config{
		Logger: discardLogger{},
		Transports: []Transport{
			&controlledTransport{managedTransport: managedTransport{name: "http"}, stopErr: stopErr},
			&managedTransport{name: "grpc", prepareErr: prepareErr},
		},
	})
	app.RegisterModules(&managedModule{name: "todo", recorder: recorder})
	registerGraphService(t, app, &managedFinalizer{managedService: managedService{name: "observability", recorder: recorder}})
	err := app.Start(t.Context())
	for _, want := range []error{ErrShutdownIncomplete, prepareErr, stopErr} {
		if !errors.Is(err, want) {
			t.Errorf("Start() error = %v, missing %v", err, want)
		}
	}
	assertNoShutdownCleanup(t, recorder.snapshot())
	if eventIndex(recorder.snapshot(), "stop module todo") >= 0 {
		t.Error("stopped module after transport stop failed")
	}
}

func TestWaitForComponentsAcceptsCompletedWorkAfterDeadline(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	done := make(chan struct{})
	close(done)
	for range 100 {
		if err := waitForComponents(ctx, done, "runner"); err != nil {
			t.Fatal(err)
		}
	}
}

func assertNoShutdownCleanup(t *testing.T, events []string) {
	t.Helper()
	for _, event := range events {
		if strings.HasPrefix(event, "close ") || strings.HasPrefix(event, "finalize ") || event == "log shutdown complete" || event == "log state closed" {
			t.Errorf("unexpected cleanup after incomplete shutdown: %s", event)
		}
	}
}
