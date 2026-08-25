package fw

import (
	"context"
	"net/http"
	"testing"

	"google.golang.org/grpc"
)

type multiTransportModule struct {
	httpRegistrations int
	grpcRegistrations int
	healthErr         error
}

func (*multiTransportModule) Name() string                   { return "multi" }
func (*multiTransportModule) Init(*Deps) error               { return nil }
func (m *multiTransportModule) Health(context.Context) error { return m.healthErr }
func (*multiTransportModule) Close() error                   { return nil }

func (m *multiTransportModule) RegisterRoutes(Router) {
	m.httpRegistrations++
}

func (m *multiTransportModule) RegisterGRPC(*grpc.Server) {
	m.grpcRegistrations++
}

func TestTransportConfigurationDefaultsAndSelection(t *testing.T) {
	t.Run("worker only", func(t *testing.T) {
		module := &multiTransportModule{}
		app := New(WithLogger(discardLogger{}))
		app.RegisterModules(module)
		if err := app.setup(); err != nil {
			t.Fatalf("setup() error = %v", err)
		}
		app.registerRoutes()
		if err := app.buildGRPCServer(); err != nil {
			t.Fatalf("buildGRPCServer() error = %v", err)
		}
		if app.buildHTTPServer() != nil || app.grpcServer != nil {
			t.Fatal("worker-only app built a transport server")
		}
		if module.httpRegistrations != 0 || module.grpcRegistrations != 0 {
			t.Fatalf("worker-only registrations = HTTP %d, gRPC %d", module.httpRegistrations, module.grpcRegistrations)
		}
	})

	t.Run("http only", func(t *testing.T) {
		module := &multiTransportModule{}
		router := &noopRouter{}
		customServer := &http.Server{ReadHeaderTimeout: 1}
		app := New(WithHTTP(HTTPConfig{Router: router, Server: customServer}), WithLogger(discardLogger{}))
		app.RegisterModules(module)
		if err := app.setup(); err != nil {
			t.Fatalf("setup() error = %v", err)
		}
		app.registerRoutes()
		if err := app.buildGRPCServer(); err != nil {
			t.Fatalf("buildGRPCServer() error = %v", err)
		}
		server := app.buildHTTPServer()
		if app.httpConfig.Addr != defaultHTTPAddr {
			t.Fatalf("HTTP Addr = %q, want %q", app.httpConfig.Addr, defaultHTTPAddr)
		}
		if server != customServer || server.Handler != router || server.Addr != defaultHTTPAddr {
			t.Fatalf("custom HTTP server was not configured by fw: %+v", server)
		}
		if module.httpRegistrations != 1 || module.grpcRegistrations != 0 || app.grpcServer != nil {
			t.Fatalf("HTTP-only registrations = HTTP %d, gRPC %d", module.httpRegistrations, module.grpcRegistrations)
		}
	})

	t.Run("grpc only", func(t *testing.T) {
		module := &multiTransportModule{}
		customServer := grpc.NewServer()
		app := New(WithGRPC(GRPCConfig{Server: customServer}), WithLogger(discardLogger{}))
		app.RegisterModules(module)
		if err := app.setup(); err != nil {
			t.Fatalf("setup() error = %v", err)
		}
		app.registerRoutes()
		if err := app.buildGRPCServer(); err != nil {
			t.Fatalf("buildGRPCServer() error = %v", err)
		}
		if app.grpcConfig.Addr != defaultGRPCAddr {
			t.Fatalf("gRPC Addr = %q, want %q", app.grpcConfig.Addr, defaultGRPCAddr)
		}
		if app.grpcServer != customServer {
			t.Fatal("fw did not use the configured gRPC server")
		}
		if module.httpRegistrations != 0 || module.grpcRegistrations != 1 || app.buildHTTPServer() != nil {
			t.Fatalf("gRPC-only registrations = HTTP %d, gRPC %d", module.httpRegistrations, module.grpcRegistrations)
		}
	})
}

func TestHTTPConfigRequiresRouter(t *testing.T) {
	app := New(WithHTTP(HTTPConfig{}), WithLogger(discardLogger{}))
	if err := app.setup(); err == nil {
		t.Fatal("setup() error = nil, want missing router error")
	}
}
