package fwgrpc

import (
	"context"

	"github.com/charlesonunze/fw"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	grpc_health_v1 "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
)

const healthServiceName = "grpc.health.v1.Health"

type healthServer struct {
	grpc_health_v1.UnimplementedHealthServer
	evaluate func(context.Context) fw.HealthReport
}

func registerHealth(server *grpc.Server, evaluate func(context.Context) fw.HealthReport) {
	if _, registered := server.GetServiceInfo()[healthServiceName]; registered {
		return
	}
	grpc_health_v1.RegisterHealthServer(server, &healthServer{evaluate: evaluate})
}

func (s *healthServer) Check(ctx context.Context, request *grpc_health_v1.HealthCheckRequest) (*grpc_health_v1.HealthCheckResponse, error) {
	if request.GetService() != "" {
		return nil, status.Error(codes.NotFound, "unknown service")
	}
	return &grpc_health_v1.HealthCheckResponse{Status: s.status(ctx)}, nil
}

func (s *healthServer) List(ctx context.Context, _ *grpc_health_v1.HealthListRequest) (*grpc_health_v1.HealthListResponse, error) {
	return &grpc_health_v1.HealthListResponse{
		Statuses: map[string]*grpc_health_v1.HealthCheckResponse{
			"": {Status: s.status(ctx)},
		},
	}, nil
}

func (s *healthServer) status(ctx context.Context) grpc_health_v1.HealthCheckResponse_ServingStatus {
	if !s.evaluate(ctx).Healthy {
		return grpc_health_v1.HealthCheckResponse_NOT_SERVING
	}
	return grpc_health_v1.HealthCheckResponse_SERVING
}
