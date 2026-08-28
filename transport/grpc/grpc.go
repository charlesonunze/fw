package fwgrpc

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"

	"github.com/charlesonunze/fw"
	"google.golang.org/grpc"
)

const (
	defaultName = "grpc"
	defaultAddr = ":9999"
)

// Module is implemented by application modules that expose gRPC services.
type Module interface {
	RegisterGRPC(server *grpc.Server)
}

// Config configures a gRPC transport. Addr defaults to ":9999". When Server
// is nil, the transport creates one. A provided server remains configurable
// with interceptors, TLS, keepalive, and other gRPC server options.
type Config struct {
	Name   string
	Addr   string
	Server *grpc.Server
}

// Transport owns one gRPC server runtime.
type Transport struct {
	config Config

	mu       sync.Mutex
	logger   fw.Logger
	listener net.Listener
	server   *grpc.Server
	prepared bool
	running  bool
	stopping atomic.Bool
	stopOnce sync.Once
	stopErr  error
}

// New creates a gRPC transport from config. Network resources are acquired by
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

// Prepare registers module services and application health, then binds the
// listener before any application runner starts.
func (t *Transport) Prepare(ctx context.Context, deps fw.TransportDeps) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if deps.Health == nil {
		return errors.New("grpc transport requires a health evaluator")
	}
	if deps.Logger == nil {
		return errors.New("grpc transport requires a logger")
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	if t.prepared {
		return errors.New("grpc transport is already prepared")
	}

	addr := t.config.Addr
	if addr == "" {
		addr = defaultAddr
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on grpc addr %q: %w", addr, err)
	}
	listenerOwned := true
	defer func() {
		if listenerOwned {
			_ = listener.Close()
		}
	}()

	server := t.config.Server
	if server == nil {
		server = grpc.NewServer()
	}
	for _, module := range deps.Modules {
		if grpcModule, ok := module.(Module); ok {
			grpcModule.RegisterGRPC(server)
		}
	}
	registerHealth(server, deps.Health)

	t.config.Addr = addr
	t.logger = deps.Logger
	t.listener = listener
	t.server = server
	t.prepared = true
	listenerOwned = false
	return nil
}

// Run serves gRPC until Stop is called or the server fails.
func (t *Transport) Run(context.Context) error {
	t.mu.Lock()
	if !t.prepared {
		t.mu.Unlock()
		return errors.New("grpc transport is not prepared")
	}
	t.running = true
	server := t.server
	listener := t.listener
	logger := t.logger
	t.mu.Unlock()

	logger.Info("starting grpc server", "transport", t.Name(), "addr", listener.Addr().String())
	err := server.Serve(listener)
	if t.stopping.Load() || errors.Is(err, grpc.ErrServerStopped) || errors.Is(err, net.ErrClosed) {
		return nil
	}
	return fmt.Errorf("grpc server: %w", err)
}

// Stop gracefully drains gRPC calls and force-stops the server when ctx
// expires. It is safe to call after Prepare even when Run never started.
func (t *Transport) Stop(ctx context.Context) error {
	if ctx == nil {
		return errors.New("grpc stop context is nil")
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
		server.Stop()
		return closeListener(listener)
	}

	done := make(chan struct{})
	go func() {
		server.GracefulStop()
		close(done)
	}()
	select {
	case <-done:
		logger.Info("grpc server stopped gracefully", "transport", t.Name())
		return closeListener(listener)
	case <-ctx.Done():
		logger.Warn("grpc graceful stop timed out, forcing stop", "transport", t.Name())
		server.Stop()
		<-done
		return errors.Join(
			fmt.Errorf("gracefully stop grpc server: %w", ctx.Err()),
			closeListener(listener),
		)
	}
}

func closeListener(listener net.Listener) error {
	if listener == nil {
		return nil
	}
	if err := listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
		return fmt.Errorf("close grpc listener: %w", err)
	}
	return nil
}

var _ fw.Transport = (*Transport)(nil)
