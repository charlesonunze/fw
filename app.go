package fw

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"
)

// App is the application container that manages modules, services,
// and the HTTP and gRPC servers.
type App struct {
	modules           []Module
	registeredModules []Module
	preRegistered     []Service
	middleware        []func(http.Handler) http.Handler
	httpConfig        *HTTPConfig
	grpcConfig        *GRPCConfig
	grpcServer        *grpc.Server
	logger            Logger
	services          *ServiceRegistry
}

// Option configures the App.
type OptionsFunc func(*App)

// New creates a new App with the given options.
func New(opts ...OptionsFunc) *App {
	a := &App{services: NewServiceRegistry()}
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
// down gracefully on exit. Call RegisterService before Start.
func (a *App) RegisterService(svc Service) error {
	if a.services == nil {
		a.services = NewServiceRegistry()
	}
	if err := a.services.Register(svc); err != nil {
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

// Start initializes all services and modules, starts the configured transports,
// and blocks until a shutdown signal is received. With no transports configured,
// Start runs as a worker-only application.
func (a *App) Start() error {
	a.ensureLogger()
	defer a.closeResources()

	if err := a.setup(); err != nil {
		return err
	}

	deps := &Deps{
		Logger:   a.logger,
		Services: a.services,
	}

	if err := a.registerModules(deps); err != nil {
		return err
	}

	if err := a.initModules(deps); err != nil {
		return err
	}

	a.registerRoutes()

	if err := a.buildGRPCServer(); err != nil {
		return err
	}

	return a.serve()
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
		if err := mod.Register(deps); err != nil {
			a.closeModule(mod)
			return fmt.Errorf("fw: failed to register module %q: %w", mod.Name(), err)
		}
		a.registeredModules = append(a.registeredModules, mod)
	}
	return nil
}

// initModules calls Init after every module has registered its services.
func (a *App) initModules(deps *Deps) error {
	for _, mod := range a.registeredModules {
		a.logger.Info("initializing module", "module", mod.Name())
		if err := mod.Init(deps); err != nil {
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

// serve starts configured transports and blocks.
func (a *App) serve() error {
	httpServer := a.buildHTTPServer()
	defer a.shutdownServers(httpServer)

	errCh := make(chan error, 2)

	if httpServer != nil {
		go func() {
			a.logger.Info("starting http server", "addr", a.httpConfig.Addr)
			err := httpServer.ListenAndServe()
			if err != nil && err != http.ErrServerClosed {
				errCh <- fmt.Errorf("fw: http server error: %w", err)
			}
		}()
	}

	if a.grpcServer != nil {
		lis, err := net.Listen("tcp", a.grpcConfig.Addr)
		if err != nil {
			return fmt.Errorf("fw: failed to listen on gRPC addr %q: %w", a.grpcConfig.Addr, err)
		}
		go func() {
			a.logger.Info("starting grpc server", "addr", a.grpcConfig.Addr)
			err := a.grpcServer.Serve(lis)
			if err != nil {
				errCh <- fmt.Errorf("fw: grpc server error: %w", err)
			}
		}()
	}

	return a.waitForShutdown(errCh)
}

func (a *App) buildHTTPServer() *http.Server {
	if a.httpConfig == nil {
		return nil
	}
	server := a.httpConfig.Server
	if server == nil {
		server = &http.Server{}
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
	Error  string `json:"error,omitempty"`
}

type readinessResponse struct {
	Status  string                        `json:"status"`
	Modules map[string]moduleHealthStatus `json:"modules"`
}

// readinessHandler calls Health() on every module and aggregates results.
// Returns 200 if all healthy, 503 if any are degraded.
func (a *App) readinessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		resp := readinessResponse{
			Status:  "ok",
			Modules: make(map[string]moduleHealthStatus),
		}

		for _, mod := range a.modules {
			if err := mod.Health(ctx); err != nil {
				resp.Modules[mod.Name()] = moduleHealthStatus{Status: "error", Error: err.Error()}
				resp.Status = "degraded"
			} else {
				resp.Modules[mod.Name()] = moduleHealthStatus{Status: "ok"}
			}
		}

		w.Header().Set("Content-Type", "application/json")
		if resp.Status != "ok" {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		_ = json.NewEncoder(w).Encode(resp)
	}
}

func (a *App) waitForShutdown(errCh chan error) error {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(quit)

	select {
	case err := <-errCh:
		return err
	case sig := <-quit:
		a.logger.Info("shutdown signal received", "signal", sig.String())
		return nil
	}
}

func (a *App) shutdownServers(httpServer *http.Server) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if httpServer != nil {
		if err := httpServer.Shutdown(ctx); err != nil {
			a.logger.Error("HTTP server shutdown error", "error", err)
		}
	}

	if a.grpcServer != nil {
		done := make(chan struct{})
		go func() {
			a.grpcServer.GracefulStop()
			close(done)
		}()
		select {
		case <-done:
			a.logger.Info("gRPC server stopped gracefully")
		case <-ctx.Done():
			a.logger.Warn("gRPC graceful stop timed out, forcing stop")
			a.grpcServer.Stop()
		}
	}
}

func (a *App) closeResources() {
	for i := len(a.registeredModules) - 1; i >= 0; i-- {
		a.closeModule(a.registeredModules[i])
	}

	for i := len(a.preRegistered) - 1; i >= 0; i-- {
		svc := a.preRegistered[i]
		a.logger.Info("closing service", "service", svc.Name())
		if err := svc.Close(); err != nil {
			a.logger.Error("service close error", "service", svc.Name(), "error", err)
		}
	}

	a.logger.Info("shutdown complete")
}

func (a *App) closeModule(mod Module) {
	a.logger.Info("closing module", "module", mod.Name())
	if err := mod.Close(); err != nil {
		a.logger.Error("module close error", "module", mod.Name(), "error", err)
	}
}
