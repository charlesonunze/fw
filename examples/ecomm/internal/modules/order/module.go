package order

import (
	"context"

	"github.com/charlesonunze/fw"
	"github.com/charlesonunze/fw/examples/ecomm/internal/modules/order/api"
	"github.com/charlesonunze/fw/examples/ecomm/internal/modules/order/repo"
	"github.com/charlesonunze/fw/examples/ecomm/internal/modules/order/service"
)

// OrderModule implements HTTPModule
type OrderModule struct {
	service *service.OrderService
	handler *api.OrderHandler
}

func New() *OrderModule { return &OrderModule{} }

func (m *OrderModule) Name() string { return "order" }

func (m *OrderModule) Health(_ context.Context) error { return nil }

func (m *OrderModule) Close() error { return nil }

func (m *OrderModule) Init(deps *fw.Deps) error {
	r := repo.New()
	m.service = service.New(r, deps.Services)
	m.handler = api.New(m.service)
	deps.Services.Register(m.service)
	return nil
}

func (m *OrderModule) RegisterRoutes(r fw.Router) {
	r.Post("/orders", m.handler.CreateOrder)
	r.Get("/orders/{id}", m.handler.GetOrder)
	r.Get("/orders", m.handler.GetUserOrders)
}
