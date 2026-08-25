package fw

import (
	"context"
	"errors"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	grpc_health_v1 "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

func TestGRPCHealthAggregatesModules(t *testing.T) {
	module := &multiTransportModule{}
	server := grpc.NewServer()
	app := New(WithGRPC(GRPCConfig{Server: server}), WithLogger(discardLogger{}))
	app.RegisterModules(module)
	if err := app.setup(); err != nil {
		t.Fatalf("setup() error = %v", err)
	}
	if err := app.buildGRPCServer(); err != nil {
		t.Fatalf("buildGRPCServer() error = %v", err)
	}
	if _, ok := server.GetServiceInfo()[grpcHealthServiceName]; !ok {
		t.Fatal("standard gRPC health service was not registered")
	}

	listener := bufconn.Listen(1024 * 1024)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient() error = %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	client := grpc_health_v1.NewHealthClient(conn)

	response, err := client.Check(context.Background(), &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if response.GetStatus() != grpc_health_v1.HealthCheckResponse_SERVING {
		t.Fatalf("Check() status = %s, want SERVING", response.GetStatus())
	}
	list, err := client.List(context.Background(), &grpc_health_v1.HealthListRequest{})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(list.GetStatuses()) != 1 || list.GetStatuses()[""].GetStatus() != grpc_health_v1.HealthCheckResponse_SERVING {
		t.Fatalf("List() statuses = %+v, want only overall SERVING status", list.GetStatuses())
	}

	module.healthErr = errors.New("database unavailable")
	response, err = client.Check(context.Background(), &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		t.Fatalf("degraded Check() error = %v", err)
	}
	if response.GetStatus() != grpc_health_v1.HealthCheckResponse_NOT_SERVING {
		t.Fatalf("degraded Check() status = %s, want NOT_SERVING", response.GetStatus())
	}

	_, err = client.Check(context.Background(), &grpc_health_v1.HealthCheckRequest{Service: "unknown"})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("unknown service code = %s, want NotFound", status.Code(err))
	}

	watch, err := client.Watch(context.Background(), &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		t.Fatalf("Watch() error = %v", err)
	}
	_, err = watch.Recv()
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("Watch().Recv() code = %s, want Unimplemented", status.Code(err))
	}
}

func TestGRPCHealthPreservesConfiguredHealthService(t *testing.T) {
	server := grpc.NewServer()
	customHealth := health.NewServer()
	customHealth.SetServingStatus("", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
	grpc_health_v1.RegisterHealthServer(server, customHealth)

	app := New(WithGRPC(GRPCConfig{Server: server}), WithLogger(discardLogger{}))
	if err := app.setup(); err != nil {
		t.Fatalf("setup() error = %v", err)
	}
	if err := app.buildGRPCServer(); err != nil {
		t.Fatalf("buildGRPCServer() error = %v", err)
	}
	response, err := customHealth.Check(context.Background(), &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		t.Fatalf("custom Check() error = %v", err)
	}
	if response.GetStatus() != grpc_health_v1.HealthCheckResponse_NOT_SERVING {
		t.Fatalf("custom Check() status = %s, want NOT_SERVING", response.GetStatus())
	}
}
