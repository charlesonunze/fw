package fw

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

type phaseUserService struct{}

func (*phaseUserService) Name() string                 { return "phase.user" }
func (*phaseUserService) Health(context.Context) error { return nil }
func (*phaseUserService) Close() error                 { return nil }
func (*phaseUserService) User()                        {}

type phaseUserProvider interface {
	User()
}

type phaseAuthService struct{}

func (*phaseAuthService) Name() string                 { return "phase.auth" }
func (*phaseAuthService) Health(context.Context) error { return nil }
func (*phaseAuthService) Close() error                 { return nil }
func (*phaseAuthService) Authorize()                   {}

type phaseAuthorizer interface {
	Authorize()
}

type phaseTodoService struct{}

func (*phaseTodoService) Name() string                 { return "phase.todo" }
func (*phaseTodoService) Health(context.Context) error { return nil }
func (*phaseTodoService) Close() error                 { return nil }

type phaseDatabaseService struct{}

func (*phaseDatabaseService) Name() string                 { return "phase.database" }
func (*phaseDatabaseService) Health(context.Context) error { return nil }
func (*phaseDatabaseService) Close() error                 { return nil }

type phaseModule struct {
	name       string
	imports    []ModuleName
	events     *[]string
	registerFn func(*Deps) error
	initFn     func(*Deps) error
}

func (m *phaseModule) Name() ModuleName      { return ModuleName(m.name) }
func (m *phaseModule) Imports() []ModuleName { return m.imports }

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
		name:    "todo",
		imports: []ModuleName{"auth"},
		events:  &events,
		registerFn: func(deps *Deps) error {
			if _, err := GetService[phaseAuthorizer](deps.Services); err != nil {
				return err
			}
			return deps.Services.Register(&phaseTodoService{})
		},
		initFn: func(*Deps) error { return nil },
	}
	auth := &phaseModule{
		name:    "auth",
		imports: []ModuleName{"user"},
		events:  &events,
		registerFn: func(deps *Deps) error {
			if _, err := GetService[phaseUserProvider](deps.Services); err != nil {
				return err
			}
			return deps.Services.Register(&phaseAuthService{}, As[phaseAuthorizer]())
		},
		initFn: func(*Deps) error { return nil },
	}
	user := &phaseModule{
		name:   "user",
		events: &events,
		registerFn: func(deps *Deps) error {
			return deps.Services.Register(&phaseUserService{}, As[phaseUserProvider]())
		},
		initFn: func(*Deps) error { return nil },
	}

	app := New(Config{Logger: discardLogger{}})
	app.RegisterModules(todo, auth, user)
	if err := app.setup(); err != nil {
		t.Fatalf("setup() error = %v", err)
	}
	if err := app.registerModules(); err != nil {
		t.Fatalf("registerModules() error = %v", err)
	}
	if err := app.initModules(context.Background()); err != nil {
		t.Fatalf("initModules() error = %v", err)
	}

	want := []string{
		"register user",
		"register auth",
		"register todo",
		"init user",
		"init auth",
		"init todo",
	}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("lifecycle events = %v, want %v", events, want)
	}

	app.closeResources()
	want = append(want, "close todo", "close auth", "close user")
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("cleanup events = %v, want %v", events, want)
	}
}

func TestModuleCannotResolveTransitivelyImportedService(t *testing.T) {
	var events []string
	user := &phaseModule{
		name:   "user",
		events: &events,
		registerFn: func(deps *Deps) error {
			return deps.Services.Register(&phaseUserService{}, As[phaseUserProvider]())
		},
		initFn: func(*Deps) error { return nil },
	}
	auth := &phaseModule{
		name:    "auth",
		imports: []ModuleName{"user"},
		events:  &events,
		registerFn: func(deps *Deps) error {
			if _, err := GetService[phaseUserProvider](deps.Services); err != nil {
				return err
			}
			return deps.Services.Register(&phaseAuthService{}, As[phaseAuthorizer]())
		},
		initFn: func(*Deps) error { return nil },
	}
	todo := &phaseModule{
		name:    "todo",
		imports: []ModuleName{"auth"},
		events:  &events,
		registerFn: func(deps *Deps) error {
			if _, err := GetService[phaseAuthorizer](deps.Services); err != nil {
				return err
			}
			_, err := GetService[phaseUserProvider](deps.Services)
			return err
		},
		initFn: func(*Deps) error { return nil },
	}

	app := New(Config{Logger: discardLogger{}})
	app.RegisterModules(todo, auth, user)
	if err := app.setup(); err != nil {
		t.Fatalf("setup() error = %v", err)
	}
	err := app.registerModules()
	if err == nil || !strings.Contains(err.Error(), `module "todo" cannot access provider type`) {
		t.Fatalf("registerModules() error = %v, want direct-import access error", err)
	}
}

func TestModuleScopeIncludesApplicationAndOwnedServices(t *testing.T) {
	var events []string
	database := &phaseDatabaseService{}
	userService := &phaseUserService{}
	user := &phaseModule{
		name:   "user",
		events: &events,
		registerFn: func(deps *Deps) error {
			resolved, err := GetService[*phaseDatabaseService](deps.Services)
			if err != nil {
				return err
			}
			if resolved != database {
				t.Fatalf("application service = %p, want %p", resolved, database)
			}
			return deps.Services.Register(userService)
		},
		initFn: func(deps *Deps) error {
			resolved, err := GetService[*phaseUserService](deps.Services)
			if err != nil {
				return err
			}
			if resolved != userService {
				t.Fatalf("owned service = %p, want %p", resolved, userService)
			}
			return nil
		},
	}

	app := New(Config{Logger: discardLogger{}})
	if err := app.RegisterService(database); err != nil {
		t.Fatalf("RegisterService() error = %v", err)
	}
	app.RegisterModules(user)
	if err := app.setup(); err != nil {
		t.Fatalf("setup() error = %v", err)
	}
	if err := app.registerModules(); err != nil {
		t.Fatalf("registerModules() error = %v", err)
	}
	if err := app.initModules(context.Background()); err != nil {
		t.Fatalf("initModules() error = %v", err)
	}
}
