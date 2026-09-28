package fw

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

type graphDatabase interface {
	Service
	graphDatabase()
}

type graphStore interface {
	Service
	graphStore()
}

type graphCache interface {
	Service
	graphCache()
}

type graphBroker interface {
	Service
	graphBroker()
}

type graphJobs interface {
	Service
	graphJobs()
}

type graphService struct {
	name       string
	recorder   *eventRecorder
	runStarted chan struct{}
	runErr     error
	stopErr    error
	closeErr   error
}

func (s *graphService) Name() string               { return s.name }
func (*graphService) Health(context.Context) error { return nil }
func (*graphService) graphDatabase()               {}
func (*graphService) graphStore()                  {}
func (*graphService) graphCache()                  {}
func (*graphService) graphBroker()                 {}
func (*graphService) graphJobs()                   {}

func (s *graphService) Run(ctx context.Context) error {
	if s.recorder != nil {
		s.recorder.add("run service " + s.name)
	}
	if s.runStarted != nil {
		close(s.runStarted)
	}
	if s.runErr != nil {
		return s.runErr
	}
	<-ctx.Done()
	if s.recorder != nil {
		s.recorder.add("runner stopped service " + s.name)
	}
	return ctx.Err()
}

func (s *graphService) Stop(context.Context) error {
	if s.recorder != nil {
		s.recorder.add("stop service " + s.name)
	}
	return s.stopErr
}

func (s *graphService) Close() error {
	if s.recorder != nil {
		s.recorder.add("close service " + s.name)
	}
	return s.closeErr
}

func TestOrderApplicationServices(t *testing.T) {
	tests := []struct {
		name     string
		register func(t *testing.T, app *App)
		want     []string
	}{
		{
			name: "independent services preserve registration order",
			register: func(t *testing.T, app *App) {
				registerGraphService(t, app, &graphService{name: "redis"})
				registerGraphService(t, app, &graphService{name: "postgres"})
			},
			want: []string{"redis", "postgres"},
		},
		{
			name: "multiple dependencies precede dependent",
			register: func(t *testing.T, app *App) {
				registerGraphService(t, app, &graphService{name: "reminders"},
					DependsOn[graphDatabase](),
					DependsOn[graphCache](),
					DependsOn[graphBroker](),
				)
				registerGraphService(t, app, &graphService{name: "redis"}, As[graphCache]())
				registerGraphService(t, app, &graphService{name: "postgres"}, As[graphDatabase]())
				registerGraphService(t, app, &graphService{name: "rabbitmq"}, As[graphBroker]())
			},
			want: []string{"redis", "postgres", "rabbitmq", "reminders"},
		},
		{
			name: "unrelated service keeps the earliest available position",
			register: func(t *testing.T, app *App) {
				registerGraphService(t, app, &graphService{name: "river"}, DependsOn[graphDatabase]())
				registerGraphService(t, app, &graphService{name: "metrics"})
				registerGraphService(t, app, &graphService{name: "postgres"}, As[graphDatabase]())
			},
			want: []string{"metrics", "postgres", "river"},
		},
		{
			name: "transitive dependencies are ordered",
			register: func(t *testing.T, app *App) {
				registerGraphService(t, app, &graphService{name: "scheduler"}, DependsOn[graphJobs]())
				registerGraphService(t, app, &graphService{name: "river"},
					As[graphJobs](),
					DependsOn[graphDatabase](),
				)
				registerGraphService(t, app, &graphService{name: "postgres"}, As[graphDatabase]())
			},
			want: []string{"postgres", "river", "scheduler"},
		},
		{
			name: "diamond dependency visits shared service once",
			register: func(t *testing.T, app *App) {
				registerGraphService(t, app, &graphService{name: "notification"},
					DependsOn[graphCache](),
					DependsOn[graphJobs](),
				)
				registerGraphService(t, app, &graphService{name: "redis"},
					As[graphCache](),
					DependsOn[graphDatabase](),
				)
				registerGraphService(t, app, &graphService{name: "river"},
					As[graphJobs](),
					DependsOn[graphDatabase](),
				)
				registerGraphService(t, app, &graphService{name: "postgres"}, As[graphDatabase]())
			},
			want: []string{"postgres", "redis", "river", "notification"},
		},
		{
			name: "different contracts for one dependency collapse to one edge",
			register: func(t *testing.T, app *App) {
				registerGraphService(t, app, &graphService{name: "consumer"},
					DependsOn[graphDatabase](),
					DependsOn[graphStore](),
				)
				registerGraphService(t, app, &graphService{name: "postgres"},
					As[graphDatabase](),
					As[graphStore](),
				)
			},
			want: []string{"postgres", "consumer"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			app := New(Config{Logger: discardLogger{}})
			test.register(t, app)
			if err := app.setup(); err != nil {
				t.Fatalf("setup() error = %v", err)
			}
			got := serviceNames(app.preRegistered)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("service order = %v, want %v", got, test.want)
			}
		})
	}
}

func TestApplicationServiceDependencyValidation(t *testing.T) {
	t.Run("missing dependency", func(t *testing.T) {
		app := New(Config{Logger: discardLogger{}})
		registerGraphService(t, app, &graphService{name: "river"}, DependsOn[graphDatabase]())

		err := app.setup()
		if err == nil || !strings.Contains(err.Error(), `service "river" depends on unregistered provider type`) {
			t.Fatalf("setup() error = %v, want missing dependency error", err)
		}
	})

	t.Run("self dependency", func(t *testing.T) {
		app := New(Config{Logger: discardLogger{}})
		registerGraphService(t, app, &graphService{name: "postgres"},
			As[graphDatabase](),
			DependsOn[graphDatabase](),
		)

		err := app.setup()
		if err == nil || !strings.Contains(err.Error(), `service "postgres" depends on itself`) {
			t.Fatalf("setup() error = %v, want self dependency error", err)
		}
	})

	t.Run("two service cycle", func(t *testing.T) {
		app := New(Config{Logger: discardLogger{}})
		registerGraphService(t, app, &graphService{name: "river"},
			As[graphJobs](),
			DependsOn[graphDatabase](),
		)
		registerGraphService(t, app, &graphService{name: "postgres"},
			As[graphDatabase](),
			DependsOn[graphJobs](),
		)

		err := app.setup()
		if err == nil || !strings.Contains(err.Error(), "river -> postgres -> river") {
			t.Fatalf("setup() error = %v, want complete cycle", err)
		}
	})

	t.Run("long cycle", func(t *testing.T) {
		app := New(Config{Logger: discardLogger{}})
		registerGraphService(t, app, &graphService{name: "scheduler"},
			As[graphBroker](),
			DependsOn[graphJobs](),
		)
		registerGraphService(t, app, &graphService{name: "river"},
			As[graphJobs](),
			DependsOn[graphDatabase](),
		)
		registerGraphService(t, app, &graphService{name: "postgres"},
			As[graphDatabase](),
			DependsOn[graphBroker](),
		)

		err := app.setup()
		if err == nil || !strings.Contains(err.Error(), "scheduler -> river -> postgres -> scheduler") {
			t.Fatalf("setup() error = %v, want complete cycle", err)
		}
	})
}

func TestDependsOnOptionValidation(t *testing.T) {
	t.Run("requires interface", func(t *testing.T) {
		app := New(Config{Logger: discardLogger{}})
		err := app.RegisterService(&graphService{name: "river"}, DependsOn[*graphService]())
		if err == nil || !strings.Contains(err.Error(), "requires an interface type") {
			t.Fatalf("RegisterService() error = %v, want interface error", err)
		}
	})

	t.Run("rejects duplicate dependency type", func(t *testing.T) {
		app := New(Config{Logger: discardLogger{}})
		err := app.RegisterService(
			&graphService{name: "river"},
			DependsOn[graphDatabase](),
			DependsOn[graphDatabase](),
		)
		if err == nil || !strings.Contains(err.Error(), "declared more than once") {
			t.Fatalf("RegisterService() error = %v, want duplicate dependency error", err)
		}
	})

	t.Run("rejects registry usage", func(t *testing.T) {
		registry := NewServiceRegistry()
		err := registry.Register(&graphService{name: "river"}, DependsOn[graphDatabase]())
		if err == nil || !strings.Contains(err.Error(), "only valid with App.RegisterService") {
			t.Fatalf("Register() error = %v, want application service error", err)
		}
	})
}

func TestApplicationServiceDependenciesControlShutdownOrder(t *testing.T) {
	recorder := &eventRecorder{}
	dependentStarted := make(chan struct{})
	databaseStarted := make(chan struct{})
	dependent := &graphService{
		name:       "river",
		recorder:   recorder,
		runStarted: dependentStarted,
	}
	database := &graphService{
		name:       "postgres",
		recorder:   recorder,
		runStarted: databaseStarted,
	}

	app := New(Config{Logger: discardLogger{}})
	registerGraphService(t, app, dependent, DependsOn[graphDatabase]())
	registerGraphService(t, app, database, As[graphDatabase]())

	startDone := make(chan error, 1)
	go func() { startDone <- app.Start(context.Background()) }()
	waitForSignal(t, dependentStarted, "dependent service runner")
	waitForSignal(t, databaseStarted, "database service runner")

	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := app.Stop(stopCtx); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if err := <-startDone; err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	events := recorder.snapshot()
	assertBefore(t, events, "stop service river", "stop service postgres")
	assertBefore(t, events, "close service river", "close service postgres")
}

func TestApplicationServiceDependencyFailureStillClosesServices(t *testing.T) {
	recorder := &eventRecorder{}
	app := New(Config{Logger: discardLogger{}})
	registerGraphService(t, app, &graphService{name: "river", recorder: recorder}, DependsOn[graphDatabase]())

	err := app.Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), "unregistered provider type") {
		t.Fatalf("Start() error = %v, want dependency error", err)
	}
	assertContains(t, recorder.snapshot(), "close service river")
}

func TestApplicationServiceDependenciesControlCloseOrderOnStartupFailure(t *testing.T) {
	recorder := &eventRecorder{}
	dependent := &graphService{name: "river", recorder: recorder}
	database := &graphService{name: "postgres", recorder: recorder}
	var transport *managedTransport

	app := New(Config{
		Logger:     discardLogger{},
		Transports: []Transport{transport},
	})
	registerGraphService(t, app, dependent, DependsOn[graphDatabase]())
	registerGraphService(t, app, database, As[graphDatabase]())

	err := app.Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), "transport 0 is nil") {
		t.Fatalf("Start() error = %v, want nil transport error", err)
	}
	assertBefore(t, recorder.snapshot(), "close service river", "close service postgres")
}

func TestApplicationServiceDependenciesControlCloseOrderForCancelledStart(t *testing.T) {
	recorder := &eventRecorder{}
	dependent := &graphService{name: "river", recorder: recorder}
	database := &graphService{name: "postgres", recorder: recorder}

	app := New(Config{Logger: discardLogger{}})
	registerGraphService(t, app, dependent, DependsOn[graphDatabase]())
	registerGraphService(t, app, database, As[graphDatabase]())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := app.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	assertBefore(t, recorder.snapshot(), "close service river", "close service postgres")
}

func TestApplicationServiceDependencyCloseErrorsAreAggregated(t *testing.T) {
	dependentCloseErr := errors.New("river close failed")
	databaseCloseErr := errors.New("postgres close failed")
	app := New(Config{Logger: discardLogger{}})
	registerGraphService(t, app, &graphService{name: "river", closeErr: dependentCloseErr}, DependsOn[graphDatabase]())
	registerGraphService(t, app, &graphService{name: "postgres", closeErr: databaseCloseErr}, As[graphDatabase]())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := app.Start(ctx)
	for _, want := range []error{dependentCloseErr, databaseCloseErr} {
		if !errors.Is(err, want) {
			t.Errorf("Start() error = %v, missing %v", err, want)
		}
	}
}

func registerGraphService(
	t *testing.T,
	app *App,
	service Service,
	options ...RegistrationOption,
) {
	t.Helper()
	if err := app.RegisterService(service, options...); err != nil {
		t.Fatalf("RegisterService(%q) error = %v", service.Name(), err)
	}
}

func serviceNames(services []Service) []string {
	names := make([]string, len(services))
	for index, service := range services {
		names[index] = service.Name()
	}
	return names
}
