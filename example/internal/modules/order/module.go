package order

import (
	"github.com/charlesonunze/fw"
	"github.com/charlesonunze/fw/example/internal/modules/order/api"
	"github.com/charlesonunze/fw/example/internal/modules/order/repo"
	"github.com/charlesonunze/fw/example/internal/modules/order/service"
	"github.com/go-chi/chi/v5"
)

// OrderModule is the order module.
type OrderModule struct {
	service *service.OrderService
	handler *api.OrderHandler
}

// New creates a new OrderModule.
func New() *OrderModule {
	return &OrderModule{}
}

func (m *OrderModule) Name() string { return "order" }

func (m *OrderModule) Init(deps *fw.Deps) error {
	r := repo.New()
	m.service = service.New(r, deps.Services)
	m.handler = api.New(m.service)

	deps.Services.Register(m.service)
	return nil
}

func (m *OrderModule) RegisterRoutes(r chi.Router) {
	r.Route("/orders", func(r chi.Router) {
		r.Post("/", m.handler.CreateOrder)
		r.Get("/", m.handler.GetUserOrders)
		r.Get("/{id}", m.handler.GetOrder)
	})
}

func (m *OrderModule) Close() error { return nil }
