package fw

import (
	"net/http"

	"google.golang.org/grpc"
)

const (
	defaultHTTPAddr = ":8888"
	defaultGRPCAddr = ":9999"
)

// HTTPConfig enables and configures the HTTP transport.
// Router is required. Addr defaults to ":8888". Middleware runs before
// middleware registered with [App.Use].
// When Server is provided, fw owns its lifecycle and sets its Addr and Handler.
type HTTPConfig struct {
	Addr       string
	Router     Router
	Server     *http.Server
	Middleware []func(http.Handler) http.Handler
}

// GRPCConfig enables and configures the gRPC transport.
// Addr defaults to ":9999". When Server is nil, fw creates one.
// A provided Server remains configurable with interceptors, TLS, and keepalive options.
type GRPCConfig struct {
	Addr   string
	Server *grpc.Server
}

// HTTPModule is implemented by modules that expose an HTTP API.
// When HTTP is configured, fw calls RegisterRoutes on every module that
// implements this interface before starting the shared server.
type HTTPModule interface {
	RegisterRoutes(r Router)
}

// GRPCModule is implemented by modules that expose a gRPC API.
// When gRPC is configured, fw calls RegisterGRPC on every module that
// implements this interface before starting the shared server.
type GRPCModule interface {
	RegisterGRPC(s *grpc.Server)
}
