package inventory

import (
	"context"

	"github.com/charlesonunze/fw"
	inventoryapi "github.com/charlesonunze/fw/examples/ecomm/internal/modules/inventory/api"
	inventorygrpc "github.com/charlesonunze/fw/examples/ecomm/internal/modules/inventory/grpc"
	inventorypb "github.com/charlesonunze/fw/examples/ecomm/internal/modules/inventory/pb"
	"github.com/charlesonunze/fw/examples/ecomm/internal/modules/inventory/repo"
	"github.com/charlesonunze/fw/examples/ecomm/internal/modules/inventory/service"

	"google.golang.org/grpc"
)

// InventoryModule implements both HTTPModule and GRPCModule.
type InventoryModule struct {
	svc     *service.InventoryService
	handler *inventoryapi.Handler
	server  *inventorygrpc.Server
}

func New() *InventoryModule { return &InventoryModule{} }

func (m *InventoryModule) Name() string { return "inventory" }

func (m *InventoryModule) Health(_ context.Context) error { return nil }

func (m *InventoryModule) Close() error { return nil }

func (m *InventoryModule) Init(deps *fw.Deps) error {
	r := repo.New()
	m.svc = service.New(r)
	m.handler = inventoryapi.New(m.svc)
	m.server = inventorygrpc.New(m.svc)
	deps.Services.Register(m.svc)
	return nil
}

func (m *InventoryModule) RegisterRoutes(r fw.Router) {
	r.Get("/inventory", m.handler.ListInventory)
	r.Get("/inventory/{id}", m.handler.GetItem)
}

func (m *InventoryModule) RegisterGRPC(s *grpc.Server) {
	inventorypb.RegisterInventoryServiceServer(s, m.server)
}
