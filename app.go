package fw

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
)

// App is the application container that manages modules, services, optional
// transports, and their shared lifecycle.
type App struct {
	modules            []Module
	registeredModules  []Module
	initializedModules []Module
	preRegistered      []Service
	transports         []Transport
	health             *healthEvaluator
	logger             Logger
	services           *ServiceRegistry

	lifecycleMu   sync.Mutex
	state         appState
	stopRequested bool
	stopContext   context.Context
	stopCh        chan struct{}
	stopped       chan struct{}
	runtimeCancel context.CancelCauseFunc
	shutdownErr   error
	closeOnce     sync.Once
	closeErr      error
	ready         atomic.Bool
}

// Option configures the App.
type OptionsFunc func(*App)

// New creates a new App with the given options.
func New(opts ...OptionsFunc) *App {
	a := &App{
		health:   newHealthEvaluator(),
		services: NewServiceRegistry(),
		state:    appStateNew,
		stopCh:   make(chan struct{}),
		stopped:  make(chan struct{}),
	}
	// Unit-level health evaluation remains useful before a transport is started.
	// Start marks the app unavailable until startup completes.
	a.ready.Store(true)
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// WithTransport installs an optional application transport such as HTTP or
// gRPC. Transports prepare and start in option order and stop concurrently.
func WithTransport(transport Transport) OptionsFunc {
	return func(a *App) { a.transports = append(a.transports, transport) }
}

// WithLogger sets a custom logger implementation.
// If not set, fw uses a JSON slog logger at INFO level.
func WithLogger(l Logger) OptionsFunc {
	return func(a *App) { a.logger = l }
}

// RegisterService registers an external service such as a database, broker, or
// cache before module registration. Successfully registered services are shut
// down gracefully on exit. Use As to expose additional interface contracts.
// Call RegisterService before Start.
func (a *App) RegisterService(svc Service, options ...RegistrationOption) error {
	if a.services == nil {
		a.services = NewServiceRegistry()
	}
	if err := a.services.Register(svc, options...); err != nil {
		return fmt.Errorf("fw: register application service: %w", err)
	}
	a.preRegistered = append(a.preRegistered, svc)
	return nil
}

// RegisterModules adds modules to the app.
// fw calls Register on every module before calling Init on any module.
func (a *App) RegisterModules(modules ...Module) {
	a.modules = append(a.modules, modules...)
}

func (a *App) ensureLogger() {
	if a.logger == nil {
		a.logger = newDefaultLogger("info")
	}
}

// setup initialises shared state before modules are wired.
func (a *App) setup() error {
	if a.services == nil {
		a.services = NewServiceRegistry()
	}
	a.registeredModules = nil
	a.initializedModules = nil
	for i, transport := range a.transports {
		if isNilTransport(transport) {
			return fmt.Errorf("fw: transport %d is nil", i)
		}
	}
	return nil
}

// registerModules calls Register on every module before initialization begins.
func (a *App) registerModules(deps *Deps) error {
	for _, mod := range a.modules {
		a.logger.Info("registering module", "module", mod.Name())
		a.registeredModules = append(a.registeredModules, mod)
		if err := mod.Register(deps); err != nil {
			return fmt.Errorf("fw: failed to register module %q: %w", mod.Name(), err)
		}
	}
	return nil
}

// initModules calls Init after every module has registered its services.
func (a *App) initModules(ctx context.Context, deps *Deps) error {
	for _, mod := range a.registeredModules {
		a.logger.Info("initializing module", "module", mod.Name())
		a.initializedModules = append(a.initializedModules, mod)
		if err := mod.Init(ctx, deps); err != nil {
			return fmt.Errorf("fw: failed to initialize module %q: %w", mod.Name(), err)
		}
	}
	return nil
}

func (a *App) closeResources() error {
	a.closeOnce.Do(func() {
		var closeErrors []error
		for i := len(a.registeredModules) - 1; i >= 0; i-- {
			if err := a.closeModule(a.registeredModules[i]); err != nil {
				closeErrors = append(closeErrors, err)
			}
		}

		for i := len(a.preRegistered) - 1; i >= 0; i-- {
			svc := a.preRegistered[i]
			a.logger.Info("closing service", "service", svc.Name())
			if err := svc.Close(); err != nil {
				a.logger.Error("service close error", "service", svc.Name(), "error", err)
				closeErrors = append(closeErrors, fmt.Errorf("fw: close application service %q: %w", svc.Name(), err))
			}
		}

		a.closeErr = errors.Join(closeErrors...)
		a.logger.Info("shutdown complete")
	})
	return a.closeErr
}

func (a *App) closeModule(mod Module) error {
	a.logger.Info("closing module", "module", mod.Name())
	if err := mod.Close(); err != nil {
		a.logger.Error("module close error", "module", mod.Name(), "error", err)
		return fmt.Errorf("fw: close module %q: %w", mod.Name(), err)
	}
	return nil
}
