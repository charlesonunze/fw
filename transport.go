package fw

import "google.golang.org/grpc"

// HTTPModule is implemented by modules that expose an HTTP API.
// fw calls RegisterRoutes on every module that implements this interface.
type HTTPModule interface {
	RegisterRoutes(r Router)
}

// GRPCModule is implemented by modules that expose a gRPC API.
// fw starts a shared gRPC server and calls RegisterGRPC on every module
// that implements this interface. The gRPC server is only started if at
// least one registered module implements GRPCModule.
type GRPCModule interface {
	RegisterGRPC(s *grpc.Server)
}
