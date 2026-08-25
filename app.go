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
	modules            []Module
	initializedModules []Module
	preRegistered      []Service
	middleware         []func(http.Handler) http.Handler
	httpConfig         *HTTPConfig
	grpcConfig         *GRPCConfig
	grpcServer         *grpc.Server
	logger             Logger
	services           *ServiceRegistry
}

// Option configures the App.
type OptionsFunc func(*App)

// New creates a new App with the given options.
func New(opts ...OptionsFunc) *App {
	a := &App{}
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

// WithAddr sets the HTTP server listen address and enables HTTP.
// Deprecated: use WithHTTP with HTTPConfig.
func WithAddr(addr string) OptionsFunc {
	return func(a *App) { a.ensureHTTPConfig().Addr = addr }
}

// WithGRPCAddr sets the gRPC server listen address and enables gRPC.
// Deprecated: use WithGRPC with GRPCConfig.
func WithGRPCAddr(addr string) OptionsFunc {
	return func(a *App) {
		if addr == "" || addr == "-" {
			a.grpcConfig = nil
			return
		}
		a.ensureGRPCConfig().Addr = addr
	}
}

// WithRouter sets the router and enables HTTP.
// Deprecated: use WithHTTP with HTTPConfig.
func WithRouter(r Router) OptionsFunc {
	return func(a *App) { a.ensureHTTPConfig().Router = r }
}

// WithGRPCServer sets a pre-configured gRPC server.
// Use this to supply interceptors, TLS credentials, keepalive options, etc.
// Deprecated: use WithGRPC with GRPCConfig.
func WithGRPCServer(s *grpc.Server) OptionsFunc {
	return func(a *App) { a.ensureGRPCConfig().Server = s }
}

func (a *App) ensureHTTPConfig() *HTTPConfig {
	if a.httpConfig == nil {
		a.httpConfig = &HTTPConfig{}
	}
	return a.httpConfig
}

func (a *App) ensureGRPCConfig() *GRPCConfig {
	if a.grpcConfig == nil {
		a.grpcConfig = &GRPCConfig{}
	}
	return a.grpcConfig
}

// WithLogger sets a custom logger implementation.
// If not set, fw uses a JSON slog logger at INFO level.
func WithLogger(l Logger) OptionsFunc {
	return func(a *App) { a.logger = l }
}

// Use registers global HTTP middleware applied to every route when HTTP is configured.
// Must be called before Start().
func (a *App) Use(middleware ...func(http.Handler) http.Handler) {
	a.middleware = append(a.middleware, middleware...)
}

// RegisterService registers an external service like redis, db, or inernal services before modules
// are initialized. Modules retrieve it via fw.GetService[T](deps.Services).
// Services are shut down gracefully on exit.
func (a *App) RegisterService(svc Service) {
	a.preRegistered = append(a.preRegistered, svc)
}

// RegisterModules adds modules to the app.
// Init is called in registration order. Cross-module dependencies must be
// resolved after all modules have initialized, such as during route registration
// or lazily inside service methods, if registration order should not matter.
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
	a.services = NewServiceRegistry()
	a.initializedModules = nil
	for _, svc := range a.preRegistered {
		a.services.Register(svc)
	}
	if a.httpConfig != nil {
		if a.httpConfig.Router == nil {
			return fmt.Errorf("fw: HTTP transport requires a router")
		}
		if a.httpConfig.Addr == "" {
			a.httpConfig.Addr = defaultHTTPAddr
		}
		a.httpConfig.Router.Use(a.middleware...)
	}
	if a.grpcConfig != nil && a.grpcConfig.Addr == "" {
		a.grpcConfig.Addr = defaultGRPCAddr
	}
	return nil
}

// initModules calls Init on every registered module.
func (a *App) initModules(deps *Deps) error {
	for _, mod := range a.modules {
		a.logger.Info("initializing module", "module", mod.Name())
		if err := mod.Init(deps); err != nil {
			if closeErr := mod.Close(); closeErr != nil {
				a.logger.Error("module close error", "module", mod.Name(), "error", closeErr)
			}
			return fmt.Errorf("fw: failed to initialize module %q: %w", mod.Name(), err)
		}
		a.initializedModules = append(a.initializedModules, mod)
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
	server.Handler = a.httpConfig.Router
	return server
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
	for i := len(a.initializedModules) - 1; i >= 0; i-- {
		mod := a.initializedModules[i]
		a.logger.Info("closing module", "module", mod.Name())
		if err := mod.Close(); err != nil {
			a.logger.Error("module close error", "module", mod.Name(), "error", err)
		}
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
