package notification

import (
	"context"

	"github.com/charlesonunze/fw"
	notificationgrpc "github.com/charlesonunze/fw/examples/ecomm/internal/modules/notification/grpc"
	notificationpb "github.com/charlesonunze/fw/examples/ecomm/internal/modules/notification/pb"
	"github.com/charlesonunze/fw/examples/ecomm/internal/modules/notification/repo"
	"github.com/charlesonunze/fw/examples/ecomm/internal/modules/notification/service"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

// NotificationModule is a gRPC-only module.
// Implements GRPCModule but not HTTPModule.
type NotificationModule struct {
	svc    *service.NotificationService
	server *notificationgrpc.Server
}

func New() *NotificationModule { return &NotificationModule{} }

func (m *NotificationModule) Name() string { return "notification" }

func (m *NotificationModule) Init(deps *fw.Deps) error {
	r := repo.New()
	m.svc = service.New(r)
	m.server = notificationgrpc.New(m.svc)
	deps.Services.Register(m.svc)
	return nil
}

// Health implements fw.Module.
func (m *NotificationModule) Health(_ context.Context) error {
	if m.server == nil {
		return context.DeadlineExceeded
	}
	return nil
}

// RegisterGRPC implements fw.GRPCModule.
func (m *NotificationModule) RegisterGRPC(s *grpc.Server) {
	notificationpb.RegisterNotificationServiceServer(s, m.server)
	reflection.Register(s)
}

func (m *NotificationModule) Close() error { return nil }
