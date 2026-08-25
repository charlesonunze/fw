package fw

import (
	"context"

	"google.golang.org/grpc/codes"
	grpc_health_v1 "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
)

const grpcHealthServiceName = "grpc.health.v1.Health"

type grpcHealthServer struct {
	grpc_health_v1.UnimplementedHealthServer
	evaluate func(context.Context) healthReport
}

func (a *App) registerGRPCHealth() {
	if _, registered := a.grpcServer.GetServiceInfo()[grpcHealthServiceName]; registered {
		return
	}
	grpc_health_v1.RegisterHealthServer(a.grpcServer, &grpcHealthServer{
		evaluate: a.evaluateHealth,
	})
}

func (s *grpcHealthServer) Check(ctx context.Context, req *grpc_health_v1.HealthCheckRequest) (*grpc_health_v1.HealthCheckResponse, error) {
	if req.GetService() != "" {
		return nil, status.Error(codes.NotFound, "unknown service")
	}
	return &grpc_health_v1.HealthCheckResponse{Status: s.status(ctx)}, nil
}

func (s *grpcHealthServer) List(ctx context.Context, _ *grpc_health_v1.HealthListRequest) (*grpc_health_v1.HealthListResponse, error) {
	return &grpc_health_v1.HealthListResponse{
		Statuses: map[string]*grpc_health_v1.HealthCheckResponse{
			"": {Status: s.status(ctx)},
		},
	}, nil
}

func (s *grpcHealthServer) status(ctx context.Context) grpc_health_v1.HealthCheckResponse_ServingStatus {
	if !s.evaluate(ctx).healthy {
		return grpc_health_v1.HealthCheckResponse_NOT_SERVING
	}
	return grpc_health_v1.HealthCheckResponse_SERVING
}
