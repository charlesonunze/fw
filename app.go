package fw

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"

	"google.golang.org/grpc"
)

// App is the application container that manages modules, services,
// and the HTTP and gRPC servers.
type App struct {
	modules            []Module
	registeredModules  []Module
	initializedModules []Module
	preRegistered      []Service
	middleware         []func(http.Handler) http.Handler
	httpConfig         *HTTPConfig
	grpcConfig         *GRPCConfig
	grpcServer         *grpc.Server
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

// WithHTTP enables and configures the HTTP transport.
func WithHTTP(config HTTPConfig) OptionsFunc {
	return func(a *App) { a.httpConfig = &config }
}

// WithGRPC enables and configures the gRPC transport.
func WithGRPC(config GRPCConfig) OptionsFunc {
	return func(a *App) { a.grpcConfig = &config }
}

// WithLogger sets a custom logger implementation.
// If not set, fw uses a JSON slog logger at INFO level.
func WithLogger(l Logger) OptionsFunc {
	return func(a *App) { a.logger = l }
}

// Use registers global HTTP middleware applied to every route. Middleware is
// applied after [HTTPConfig.Middleware]. Use must be called before [App.Start],
// and Start returns an error when HTTP is not configured.
func (a *App) Use(middleware ...func(http.Handler) http.Handler) {
	a.middleware = append(a.middleware, middleware...)
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
	if a.httpConfig == nil && len(a.middleware) > 0 {
		return fmt.Errorf("fw: app.Use requires HTTP transport configuration")
	}
	if a.httpConfig != nil {
		if a.httpConfig.Router == nil {
			return fmt.Errorf("fw: HTTP transport requires a router")
		}
		if a.httpConfig.Addr == "" {
			a.httpConfig.Addr = defaultHTTPAddr
		}
	}
	if a.grpcConfig != nil && a.grpcConfig.Addr == "" {
		a.grpcConfig.Addr = defaultGRPCAddr
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

// registerRoutes mounts HTTP routes and health endpoints.
func (a *App) registerRoutes() {
	if a.httpConfig == nil {
		return
	}
	router := a.httpConfig.Router
	for _, mod := range a.modules {
		if hm, ok := mod.(HTTPModule); ok {
			hm.RegisterRoutes(router)
		}
	}
	router.Get("/health/live", a.livenessHandler())
	router.Get("/health/ready", a.readinessHandler())
}

// buildGRPCServer wires modules and health into the configured gRPC server.
func (a *App) buildGRPCServer() error {
	if a.grpcConfig == nil {
		a.grpcServer = nil
		return nil
	}
	a.grpcServer = a.grpcConfig.Server
	if a.grpcServer == nil {
		a.grpcServer = grpc.NewServer()
	}
	for _, mod := range a.modules {
		if gm, ok := mod.(GRPCModule); ok {
			gm.RegisterGRPC(a.grpcServer)
		}
	}
	a.registerGRPCHealth()
	return nil
}

func (a *App) buildHTTPServer() *http.Server {
	if a.httpConfig == nil {
		return nil
	}
	server := a.httpConfig.Server
	if server == nil {
		server = &http.Server{
			ReadHeaderTimeout: defaultHTTPReadHeaderTimeout,
			IdleTimeout:       defaultHTTPIdleTimeout,
		}
	}
	server.Addr = a.httpConfig.Addr
	server.Handler = a.buildHTTPHandler()
	return server
}

func (a *App) buildHTTPHandler() http.Handler {
	middleware := make([]func(http.Handler) http.Handler, 0, len(a.httpConfig.Middleware)+len(a.middleware))
	middleware = append(middleware, a.httpConfig.Middleware...)
	middleware = append(middleware, a.middleware...)

	var handler http.Handler = a.httpConfig.Router
	for i := len(middleware) - 1; i >= 0; i-- {
		handler = middleware[i](handler)
	}
	return handler
}

// livenessHandler always returns 200 — the process is alive.
func (a *App) livenessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}
}

type moduleHealthStatus struct {
	Status string `json:"status"`
}

type readinessResponse struct {
	Status  string                        `json:"status"`
	Modules map[string]moduleHealthStatus `json:"modules"`
}

// readinessHandler calls Health() on every module and aggregates results.
// Returns 200 if all healthy, 503 if any are degraded.
func (a *App) readinessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		report := a.evaluateHealth(r.Context())
		resp := readinessResponse{
			Status:  "ok",
			Modules: make(map[string]moduleHealthStatus),
		}
		if !report.healthy {
			resp.Status = "degraded"
		}
		for module, healthy := range report.modules {
			status := "error"
			if healthy {
				status = "ok"
			}
			resp.Modules[module] = moduleHealthStatus{Status: status}
		}

		w.Header().Set("Content-Type", "application/json")
		if resp.Status != "ok" {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		_ = json.NewEncoder(w).Encode(resp)
	}
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
