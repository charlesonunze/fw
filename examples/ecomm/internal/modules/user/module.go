package user

import (
	"context"

	"github.com/charlesonunze/fw"
	"github.com/charlesonunze/fw/examples/ecomm/internal/modules/user/api"
	"github.com/charlesonunze/fw/examples/ecomm/internal/modules/user/repo"
	"github.com/charlesonunze/fw/examples/ecomm/internal/modules/user/service"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
)

// UserModule implements both HTTPModule and GRPCModule.
type UserModule struct {
	service      *service.UserService
	handler      *api.UserHandler
	healthServer *health.Server
}

func New() *UserModule { return &UserModule{} }

func (m *UserModule) Name() string { return "user" }

func (m *UserModule) Health(_ context.Context) error { return nil }

func (m *UserModule) Init(deps *fw.Deps) error {
	r := repo.New()
	m.service = service.New(r, deps.Services)
	m.handler = api.New(m.service)
	m.healthServer = health.NewServer()
	deps.Services.Register(m.service)
	return nil
}

func (m *UserModule) RegisterRoutes(r fw.Router) {
	r.Post("/users", m.handler.CreateUser)
	r.Get("/users", m.handler.ListUsers)
	r.Get("/users/{id}", m.handler.GetUser)
}

func (m *UserModule) RegisterGRPC(s *grpc.Server) {
	grpc_health_v1.RegisterHealthServer(s, m.healthServer)
	m.healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)
	m.healthServer.SetServingStatus("user", grpc_health_v1.HealthCheckResponse_SERVING)
}

func (m *UserModule) Close() error {
	if m.healthServer != nil {
		m.healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
		m.healthServer.SetServingStatus("user", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
		m.healthServer.Shutdown()
	}
	return nil
}
