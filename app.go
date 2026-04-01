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
	modules       []Module
	preRegistered []Service
	middleware    []func(http.Handler) http.Handler
	router        Router
	grpcServer    *grpc.Server
	logger        Logger
	services      *ServiceRegistry
	httpAddr      string
	grpcAddr      string
}

// Option configures the App.
type OptionsFunc func(*App)

// New creates a new App with the given options.
func New(opts ...OptionsFunc) *App {
	a := &App{
		httpAddr: ":8888",
		grpcAddr: ":9999",
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// WithAddr sets the HTTP server listen address. Default: ":8888".
func WithAddr(addr string) OptionsFunc {
	return func(a *App) { a.httpAddr = addr }
}

// WithGRPCAddr sets the gRPC server listen address. Default: ":9999".
// Set to "-" to disable gRPC entirely.
func WithGRPCAddr(addr string) OptionsFunc {
	return func(a *App) { a.grpcAddr = addr }
}

// WithRouter sets a custom router (e.g. fw.ChiRouter(r), a gin adapter, etc.).
// If not set, fw uses a bare chi router.
func WithRouter(r Router) OptionsFunc {
	return func(a *App) { a.router = r }
}

// WithGRPCServer sets a pre-configured gRPC server.
// Use this to supply interceptors, TLS credentials, keepalive options, etc.
// If not set and at least one module implements GRPCModule, fw creates a
// plain grpc.NewServer().
func WithGRPCServer(s *grpc.Server) OptionsFunc {
	return func(a *App) { a.grpcServer = s }
}

// WithLogger sets a custom logger implementation.
// If not set, fw uses a JSON slog logger at INFO level.
func WithLogger(l Logger) OptionsFunc {
	return func(a *App) { a.logger = l }
}

// Use registers global HTTP middleware applied to every route.
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
// Registration order does not matter — services are resolved lazily at call time,
// not during Init.
func (a *App) RegisterModules(modules ...Module) {
	a.modules = append(a.modules, modules...)
}

// Start initializes all services and modules, starts the HTTP server
// (and optionally a gRPC server), and blocks until a shutdown signal is received.
func (a *App) Start() error {
	a.setup()

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

// setup initialises shared state before modules are wired.
func (a *App) setup() {
	if a.logger == nil {
		a.logger = newDefaultLogger("info")
	}
	a.services = NewServiceRegistry()
	for _, svc := range a.preRegistered {
		a.services.Register(svc)
	}
	if a.router == nil {
		a.router = newChiRouter()
	}
	a.router.Use(a.middleware...)
}

// initModules calls Init on every registered module.
func (a *App) initModules(deps *Deps) error {
	for _, mod := range a.modules {
		a.logger.Info("initializing module", "module", mod.Name())
		if err := mod.Init(deps); err != nil {
			return fmt.Errorf("fw: failed to initialize module %q: %w", mod.Name(), err)
		}
	}
	return nil
}

// registerRoutes mounts HTTP routes and health endpoints.
func (a *App) registerRoutes() {
	for _, mod := range a.modules {
		if hm, ok := mod.(HTTPModule); ok {
			hm.RegisterRoutes(a.router)
		}
	}
	a.router.Get("/health/live", a.livenessHandler())
	a.router.Get("/health/ready", a.readinessHandler())
}

// buildGRPCServer wires gRPC if any modules implement GRPCModule.
func (a *App) buildGRPCServer() error {
	if a.grpcAddr == "" || a.grpcAddr == "-" {
		return nil
	}
	var grpcModules []GRPCModule
	for _, mod := range a.modules {
		if gm, ok := mod.(GRPCModule); ok {
			grpcModules = append(grpcModules, gm)
		}
	}
	if len(grpcModules) == 0 {
		return nil
	}
	if a.grpcServer == nil {
		a.grpcServer = grpc.NewServer()
	}
	for _, gm := range grpcModules {
		gm.RegisterGRPC(a.grpcServer)
	}
	return nil
}

// serve starts the HTTP (and optionally gRPC) server and blocks.
func (a *App) serve() error {
	httpServer := &http.Server{
		Addr:    a.httpAddr,
		Handler: a.router,
	}

	errCh := make(chan error, 2)

	go func() {
		a.logger.Info("starting http server", "addr", a.httpAddr)
		err := httpServer.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("fw: http server error: %w", err)
		}
	}()

	if a.grpcServer != nil {
		lis, err := net.Listen("tcp", a.grpcAddr)
		if err != nil {
			return fmt.Errorf("fw: failed to listen on gRPC addr %q: %w", a.grpcAddr, err)
		}
		go func() {
			a.logger.Info("starting grpc server", "addr", a.grpcAddr)
			err := a.grpcServer.Serve(lis)
			if err != nil {
				errCh <- fmt.Errorf("fw: grpc server error: %w", err)
			}
		}()
	}

	return a.waitForShutdown(httpServer, errCh)
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

func (a *App) waitForShutdown(httpServer *http.Server, errCh chan error) error {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		return err
	case sig := <-quit:
		a.logger.Info("shutdown signal received", "signal", sig.String())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(ctx); err != nil {
		a.logger.Error("HTTP server shutdown error", "error", err)
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

	for _, mod := range a.modules {
		a.logger.Info("closing module", "module", mod.Name())
		if err := mod.Close(); err != nil {
			a.logger.Error("module close error", "module", mod.Name(), "error", err)
		}
	}

	for _, svc := range a.preRegistered {
		a.logger.Info("closing service", "service", svc.Name())
		if err := svc.Close(); err != nil {
			a.logger.Error("service close error", "service", svc.Name(), "error", err)
		}
	}

	a.logger.Info("shutdown complete")
	return nil
}
