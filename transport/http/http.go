package fwhttp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charlesonunze/fw"
)

const (
	defaultName              = "http"
	defaultAddr              = ":8888"
	defaultReadHeaderTimeout = 5 * time.Second
	defaultIdleTimeout       = 2 * time.Minute
)

// Middleware wraps an HTTP handler.
type Middleware func(http.Handler) http.Handler

// Config configures an HTTP transport. Router is required and Addr defaults to
// ":8888". A framework-created server uses conservative header-read and idle
// timeouts. When Server is provided, the transport owns its lifecycle, sets its
// Addr and Handler, and leaves all other settings unchanged.
type Config struct {
	Name       string
	Addr       string
	Router     Router
	Server     *http.Server
	Middleware []Middleware
}

// Transport owns one HTTP server runtime.
type Transport struct {
	config Config

	mu       sync.Mutex
	logger   fw.Logger
	listener net.Listener
	server   *http.Server
	prepared bool
	running  bool
	stopping atomic.Bool
	stopOnce sync.Once
	stopErr  error
}

// New creates an HTTP transport from config. Network resources are acquired by
// Prepare when the application starts.
func New(config Config) *Transport {
	return &Transport{config: config}
}

// Name returns the transport's operational name.
func (t *Transport) Name() string {
	if t.config.Name != "" {
		return t.config.Name
	}
	return defaultName
}

// Prepare registers module and health routes, builds the middleware chain, and
// binds the listener before any application runner starts.
func (t *Transport) Prepare(ctx context.Context, deps fw.TransportDeps) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if t.config.Router == nil {
		return errors.New("http transport requires a router")
	}
	if deps.Health == nil {
		return errors.New("http transport requires a health evaluator")
	}
	if deps.Logger == nil {
		return errors.New("http transport requires a logger")
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	if t.prepared {
		return errors.New("http transport is already prepared")
	}

	var handler http.Handler = t.config.Router
	for i := len(t.config.Middleware) - 1; i >= 0; i-- {
		if t.config.Middleware[i] == nil {
			return fmt.Errorf("http middleware %d is nil", i)
		}
		handler = t.config.Middleware[i](handler)
		if handler == nil {
			return fmt.Errorf("http middleware %d returned a nil handler", i)
		}
	}

	addr := t.config.Addr
	if addr == "" {
		addr = defaultAddr
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on http addr %q: %w", addr, err)
	}
	listenerOwned := true
	defer func() {
		if listenerOwned {
			_ = listener.Close()
		}
	}()

	for _, module := range deps.Modules {
		if httpModule, ok := module.(Module); ok {
			httpModule.RegisterRoutes(t.config.Router)
		}
	}
	t.config.Router.Get("/health/live", livenessHandler())
	t.config.Router.Get("/health/ready", readinessHandler(deps.Health))

	server := t.config.Server
	if server == nil {
		server = &http.Server{
			ReadHeaderTimeout: defaultReadHeaderTimeout,
			IdleTimeout:       defaultIdleTimeout,
		}
	}
	server.Addr = addr
	server.Handler = handler

	t.config.Addr = addr
	t.logger = deps.Logger
	t.listener = listener
	t.server = server
	t.prepared = true
	listenerOwned = false
	return nil
}

// Run serves HTTP until Stop is called or the server fails.
func (t *Transport) Run(context.Context) error {
	t.mu.Lock()
	if !t.prepared {
		t.mu.Unlock()
		return errors.New("http transport is not prepared")
	}
	t.running = true
	server := t.server
	listener := t.listener
	logger := t.logger
	t.mu.Unlock()

	logger.Info("starting http server", "transport", t.Name(), "addr", listener.Addr().String())
	err := server.Serve(listener)
	if t.stopping.Load() || errors.Is(err, http.ErrServerClosed) || errors.Is(err, net.ErrClosed) {
		return nil
	}
	return fmt.Errorf("http server: %w", err)
}

// Stop gracefully drains HTTP requests and force-closes the server when ctx
// expires. It is safe to call after Prepare even when Run never started.
func (t *Transport) Stop(ctx context.Context) error {
	if ctx == nil {
		return errors.New("http stop context is nil")
	}
	t.stopOnce.Do(func() {
		t.stopErr = t.stop(ctx)
	})
	return t.stopErr
}

func (t *Transport) stop(ctx context.Context) error {
	t.stopping.Store(true)

	t.mu.Lock()
	if !t.prepared {
		t.mu.Unlock()
		return nil
	}
	server := t.server
	listener := t.listener
	running := t.running
	logger := t.logger
	t.mu.Unlock()

	if !running {
		return closeListener(listener, "http")
	}

	if err := server.Shutdown(ctx); err != nil {
		logger.Warn("http graceful shutdown failed, forcing close", "transport", t.Name(), "error", err)
		closeErr := server.Close()
		if closeErr != nil && !errors.Is(closeErr, http.ErrServerClosed) {
			closeErr = fmt.Errorf("force close http server: %w", closeErr)
		} else {
			closeErr = nil
		}
		return errors.Join(
			fmt.Errorf("gracefully stop http server: %w", err),
			closeErr,
			closeListener(listener, "http"),
		)
	}
	logger.Info("http server stopped gracefully", "transport", t.Name())
	return closeListener(listener, "http")
}

func closeListener(listener net.Listener, name string) error {
	if listener == nil {
		return nil
	}
	if err := listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
		return fmt.Errorf("close %s listener: %w", name, err)
	}
	return nil
}

func livenessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
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

func readinessHandler(health func(context.Context) fw.HealthReport) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		report := health(r.Context())
		response := readinessResponse{
			Status:  "ok",
			Modules: make(map[string]moduleHealthStatus, len(report.Modules)),
		}
		if !report.Healthy {
			response.Status = "degraded"
		}
		for module, healthy := range report.Modules {
			status := "error"
			if healthy {
				status = "ok"
			}
			response.Modules[module] = moduleHealthStatus{Status: status}
		}

		w.Header().Set("Content-Type", "application/json")
		if !report.Healthy {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		_ = json.NewEncoder(w).Encode(response)
	}
}

var _ fw.Transport = (*Transport)(nil)
