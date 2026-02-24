package fw

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/charlesonunze/fw/middleware"
	"github.com/go-chi/chi/v5"
)

// App is the application container that manages modules, dependencies, and the HTTP server.
type App struct {
	modules    []Module
	router     chi.Router
	deps       *Deps
	configPath string
	addr       string
}

// Option configures the App.
type Option func(*App)

// WithAddr sets the HTTP server listen address.
func WithAddr(addr string) Option {
	return func(a *App) {
		a.addr = addr
	}
}

// WithConfigPath sets the path to the YAML config file.
func WithConfigPath(path string) Option {
	return func(a *App) {
		a.configPath = path
	}
}

// New creates a new App with the given options.
func New(opts ...Option) *App {
	a := &App{
		addr:       ":8080",
		configPath: "config.yaml",
	}

	for _, opt := range opts {
		opt(a)
	}

	return a
}

// Register adds modules to the app.
func (a *App) Register(modules ...Module) {
	a.modules = append(a.modules, modules...)
}

// Start initializes all dependencies and modules, starts the HTTP server,
// and blocks until a shutdown signal is received.
func (a *App) Start() error {
	// Load config
	cfg, err := LoadConfig(a.configPath)
	if err != nil {
		return fmt.Errorf("fw: %w", err)
	}

	// Apply config overrides
	if a.addr != "" && cfg.App.Addr != "" {
		// Config file takes precedence if set
		a.addr = cfg.App.Addr
	}

	// Create logger
	logger := NewLogger(cfg.Log.Level)

	// Open database (returns nil if no DSN configured)
	db, err := OpenDatabase(cfg.Database)
	if err != nil {
		return fmt.Errorf("fw: %w", err)
	}

	// Create event bus
	events := NewEventBus()

	// Create service registry
	services := NewServiceRegistry()

	// Build deps
	a.deps = &Deps{
		DB:       db,
		Logger:   logger,
		Config:   cfg,
		Events:   events,
		Services: services,
	}

	// Create router with default middleware
	a.router = chi.NewRouter()
	a.router.Use(middleware.RequestID)
	a.router.Use(middleware.Recovery(logger))
	a.router.Use(middleware.Logging(logger))

	// Initialize all modules
	for _, mod := range a.modules {
		logger.Info("initializing module", "module", mod.Name())
		if err := mod.Init(a.deps); err != nil {
			return fmt.Errorf("fw: failed to initialize module %q: %w", mod.Name(), err)
		}
	}

	// Register routes for all modules
	for _, mod := range a.modules {
		mod.RegisterRoutes(a.router)
	}

	// Health check endpoint
	a.router.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	// Create HTTP server
	server := &http.Server{
		Addr:    a.addr,
		Handler: a.router,
	}

	// Start server in a goroutine
	errCh := make(chan error, 1)
	go func() {
		logger.Info("starting server", "addr", a.addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("fw: server error: %w", err)
		}
	}()

	// Wait for shutdown signal
	return a.waitForShutdown(server, logger, errCh)
}

func (a *App) waitForShutdown(server *http.Server, logger *slog.Logger, errCh chan error) error {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		return err
	case sig := <-quit:
		logger.Info("shutdown signal received", "signal", sig.String())
	}

	// Graceful shutdown with 30s timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Shutdown HTTP server
	if err := server.Shutdown(ctx); err != nil {
		logger.Error("server shutdown error", "error", err)
	}

	// Close all modules in reverse order
	for i := len(a.modules) - 1; i >= 0; i-- {
		mod := a.modules[i]
		logger.Info("closing module", "module", mod.Name())
		if err := mod.Close(); err != nil {
			logger.Error("module close error", "module", mod.Name(), "error", err)
		}
	}

	// Close database
	if a.deps.DB != nil {
		if err := a.deps.DB.Close(); err != nil {
			logger.Error("database close error", "error", err)
		}
	}

	logger.Info("shutdown complete")
	return nil
}
