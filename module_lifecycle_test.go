package fw

import (
	"context"
	"reflect"
	"testing"
)

type phaseUserService struct{}

func (*phaseUserService) Name() string { return "phase.user" }
func (*phaseUserService) Close() error { return nil }

type phaseAuthService struct{}

func (*phaseAuthService) Name() string { return "phase.auth" }
func (*phaseAuthService) Close() error { return nil }

type phaseTodoService struct{}

func (*phaseTodoService) Name() string { return "phase.todo" }
func (*phaseTodoService) Close() error { return nil }

type phaseModule struct {
	name       string
	events     *[]string
	registerFn func(*Deps) error
	initFn     func(*Deps) error
}

func (m *phaseModule) Name() string { return m.name }

func (m *phaseModule) Register(deps *Deps) error {
	*m.events = append(*m.events, "register "+m.name)
	return m.registerFn(deps)
}

func (m *phaseModule) Init(_ context.Context, deps *Deps) error {
	*m.events = append(*m.events, "init "+m.name)
	return m.initFn(deps)
}

func (*phaseModule) Health(context.Context) error { return nil }

func (m *phaseModule) Close() error {
	*m.events = append(*m.events, "close "+m.name)
	return nil
}

func TestModuleRegistrationOrderDoesNotAffectDependencyResolution(t *testing.T) {
	var events []string
	todo := &phaseModule{
		name:   "todo",
		events: &events,
		registerFn: func(deps *Deps) error {
			return deps.Services.Register(&phaseTodoService{})
		},
		initFn: func(deps *Deps) error {
			_, err := GetService[*phaseAuthService](deps.Services)
			return err
		},
	}
	auth := &phaseModule{
		name:   "auth",
		events: &events,
		registerFn: func(deps *Deps) error {
			return deps.Services.Register(&phaseAuthService{})
		},
		initFn: func(deps *Deps) error {
			_, err := GetService[*phaseUserService](deps.Services)
			return err
		},
	}
	user := &phaseModule{
		name:   "user",
		events: &events,
		registerFn: func(deps *Deps) error {
			return deps.Services.Register(&phaseUserService{})
		},
		initFn: func(*Deps) error { return nil },
	}

	app := New(WithLogger(discardLogger{}))
	app.RegisterModules(todo, auth, user)
	if err := app.setup(); err != nil {
		t.Fatalf("setup() error = %v", err)
	}
	deps := &Deps{Logger: app.logger, Services: app.services}
	if err := app.registerModules(deps); err != nil {
		t.Fatalf("registerModules() error = %v", err)
	}
	if err := app.initModules(context.Background(), deps); err != nil {
		t.Fatalf("initModules() error = %v", err)
	}

	want := []string{
		"register todo",
		"register auth",
		"register user",
		"init todo",
		"init auth",
		"init user",
	}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("lifecycle events = %v, want %v", events, want)
	}

	app.closeResources()
	want = append(want, "close user", "close auth", "close todo")
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("cleanup events = %v, want %v", events, want)
	}
}
