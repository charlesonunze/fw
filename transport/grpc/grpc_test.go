package fwgrpc

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/charlesonunze/fw"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	grpc_health_v1 "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
)

type testLogger struct{}

func (testLogger) Info(string, ...any)  {}
func (testLogger) Error(string, ...any) {}
func (testLogger) Debug(string, ...any) {}
func (testLogger) Warn(string, ...any)  {}
func (l testLogger) With(...any) fw.Logger {
	return l
}

type testModule struct {
	registrations int
}

func (*testModule) Name() string                         { return "user" }
func (*testModule) Register(*fw.Deps) error              { return nil }
func (*testModule) Init(context.Context, *fw.Deps) error { return nil }
func (*testModule) Health(context.Context) error         { return nil }
func (*testModule) Close() error                         { return nil }
func (m *testModule) RegisterGRPC(*grpc.Server)          { m.registrations++ }

func testDeps(modules ...fw.Module) fw.TransportDeps {
	return fw.TransportDeps{
		Modules: modules,
		Logger:  testLogger{},
		Health: func(context.Context) fw.HealthReport {
			return fw.HealthReport{Healthy: true, Modules: map[string]bool{}}
		},
	}
}

func TestPrepareUsesDefaultsAndRegistersGRPCModules(t *testing.T) {
	module := &testModule{}
	transport := New(Config{Addr: "127.0.0.1:0"})
	if err := transport.Prepare(context.Background(), testDeps(module)); err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	t.Cleanup(func() { _ = transport.Stop(context.Background()) })

	if transport.Name() != defaultName {
		t.Fatalf("Name() = %q, want %q", transport.Name(), defaultName)
	}
	if module.registrations != 1 {
		t.Fatalf("gRPC registrations = %d, want 1", module.registrations)
	}
	if _, ok := transport.server.GetServiceInfo()[healthServiceName]; !ok {
		t.Fatal("gRPC health service was not registered")
	}
}

func TestPreparePreservesConfiguredServerAndHealthService(t *testing.T) {
	server := grpc.NewServer()
	customHealth := health.NewServer()
	customHealth.SetServingStatus("", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
	grpc_health_v1.RegisterHealthServer(server, customHealth)

	transport := New(Config{Name: "internal-grpc", Addr: "127.0.0.1:0", Server: server})
	if err := transport.Prepare(context.Background(), testDeps()); err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	t.Cleanup(func() { _ = transport.Stop(context.Background()) })

	if transport.Name() != "internal-grpc" || transport.server != server {
		t.Fatalf("configured server was not preserved: name=%q server=%p", transport.Name(), transport.server)
	}
	response, err := customHealth.Check(context.Background(), &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		t.Fatalf("custom Check() error = %v", err)
	}
	if response.GetStatus() != grpc_health_v1.HealthCheckResponse_NOT_SERVING {
		t.Fatalf("custom Check() status = %s, want NOT_SERVING", response.GetStatus())
	}
}

func TestPrepareBindFailureDoesNotMutateServer(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	defer occupied.Close()

	server := grpc.NewServer()
	module := &testModule{}
	transport := New(Config{Addr: occupied.Addr().String(), Server: server})
	if err := transport.Prepare(context.Background(), testDeps(module)); err == nil {
		t.Fatal("Prepare() error = nil, want bind error")
	}
	if module.registrations != 0 {
		t.Fatalf("bind failure registered module %d times", module.registrations)
	}
	if len(server.GetServiceInfo()) != 0 {
		t.Fatalf("bind failure registered services: %v", server.GetServiceInfo())
	}
}

func TestHealthServiceReportsOverallApplicationHealth(t *testing.T) {
	var healthy atomic.Bool
	healthy.Store(true)
	deps := testDeps()
	deps.Health = func(context.Context) fw.HealthReport {
		return fw.HealthReport{Healthy: healthy.Load(), Modules: map[string]bool{"user": healthy.Load()}}
	}

	transport := New(Config{Addr: "127.0.0.1:0"})
	if err := transport.Prepare(context.Background(), deps); err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	runDone := make(chan error, 1)
	go func() { runDone <- transport.Run(context.Background()) }()

	conn, err := grpc.NewClient(
		"passthrough:///"+transport.listener.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient() error = %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	client := grpc_health_v1.NewHealthClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	response, err := client.Check(ctx, &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if response.GetStatus() != grpc_health_v1.HealthCheckResponse_SERVING {
		t.Fatalf("Check() status = %s, want SERVING", response.GetStatus())
	}
	list, err := client.List(ctx, &grpc_health_v1.HealthListRequest{})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(list.GetStatuses()) != 1 || list.GetStatuses()[""].GetStatus() != grpc_health_v1.HealthCheckResponse_SERVING {
		t.Fatalf("List() statuses = %+v, want only overall SERVING status", list.GetStatuses())
	}

	healthy.Store(false)
	response, err = client.Check(ctx, &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		t.Fatalf("degraded Check() error = %v", err)
	}
	if response.GetStatus() != grpc_health_v1.HealthCheckResponse_NOT_SERVING {
		t.Fatalf("degraded Check() status = %s, want NOT_SERVING", response.GetStatus())
	}
	_, err = client.Check(ctx, &grpc_health_v1.HealthCheckRequest{Service: "unknown"})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("unknown service code = %s, want NotFound", status.Code(err))
	}

	watch, err := client.Watch(ctx, &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		t.Fatalf("Watch() error = %v", err)
	}
	if _, err = watch.Recv(); status.Code(err) != codes.Unimplemented {
		t.Fatalf("Watch().Recv() code = %s, want Unimplemented", status.Code(err))
	}

	if err := transport.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if err := <-runDone; err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestStopForcesActiveRPCClosedAfterDeadline(t *testing.T) {
	started := make(chan struct{})
	stopped := make(chan struct{})
	server := grpc.NewServer()
	server.RegisterService(&blockingServiceDesc, &blockingServer{started: started, stopped: stopped})
	transport := New(Config{Addr: "127.0.0.1:0", Server: server})
	if err := transport.Prepare(context.Background(), testDeps()); err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	runDone := make(chan error, 1)
	go func() { runDone <- transport.Run(context.Background()) }()

	conn, err := grpc.NewClient(
		"passthrough:///"+transport.listener.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient() error = %v", err)
	}
	defer conn.Close()
	rpcDone := make(chan error, 1)
	go func() {
		rpcDone <- conn.Invoke(
			context.Background(),
			"/test.Blocker/Block",
			&grpc_health_v1.HealthCheckRequest{},
			&grpc_health_v1.HealthCheckResponse{},
		)
	}()
	waitForSignal(t, started, "gRPC call")

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := transport.Stop(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Stop() error = %v, want deadline exceeded", err)
	}
	waitForSignal(t, stopped, "forced gRPC call cancellation")
	if err := <-runDone; err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	select {
	case <-rpcDone:
	case <-time.After(time.Second):
		t.Fatal("gRPC client remained blocked after forced stop")
	}
}

func TestTransportRejectsInvalidLifecycleCalls(t *testing.T) {
	transport := New(Config{})
	if err := transport.Run(context.Background()); err == nil || !strings.Contains(err.Error(), "not prepared") {
		t.Fatalf("Run() error = %v, want not prepared error", err)
	}
	//lint:ignore SA1012 verifies the transport's public nil-context guard
	if err := transport.Stop(nil); err == nil || !strings.Contains(err.Error(), "context is nil") {
		t.Fatalf("Stop(nil) error = %v, want nil context error", err)
	}

	deps := testDeps()
	deps.Health = nil
	if err := transport.Prepare(context.Background(), deps); err == nil || !strings.Contains(err.Error(), "health evaluator") {
		t.Fatalf("Prepare() error = %v, want health evaluator error", err)
	}
}

type blocker interface {
	Block(context.Context, *grpc_health_v1.HealthCheckRequest) (*grpc_health_v1.HealthCheckResponse, error)
}

type blockingServer struct {
	started  chan struct{}
	stopped  chan struct{}
	stopOnce sync.Once
}

func (s *blockingServer) Block(
	ctx context.Context,
	_ *grpc_health_v1.HealthCheckRequest,
) (*grpc_health_v1.HealthCheckResponse, error) {
	close(s.started)
	<-ctx.Done()
	s.stopOnce.Do(func() { close(s.stopped) })
	return nil, ctx.Err()
}

var blockingServiceDesc = grpc.ServiceDesc{
	ServiceName: "test.Blocker",
	HandlerType: (*blocker)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "Block",
			Handler: func(server any, ctx context.Context, decode func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
				request := &grpc_health_v1.HealthCheckRequest{}
				if err := decode(request); err != nil {
					return nil, err
				}
				if interceptor == nil {
					return server.(blocker).Block(ctx, request)
				}
				info := &grpc.UnaryServerInfo{Server: server, FullMethod: "/test.Blocker/Block"}
				handler := func(ctx context.Context, request any) (any, error) {
					return server.(blocker).Block(ctx, request.(*grpc_health_v1.HealthCheckRequest))
				}
				return interceptor(ctx, request, info, handler)
			},
		},
	},
}

func waitForSignal(t *testing.T, signal <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", name)
	}
}
