package user

import (
	"github.com/charlesonunze/fw"
	"github.com/charlesonunze/fw/example/internal/modules/user/api"
	"github.com/charlesonunze/fw/example/internal/modules/user/repo"
	"github.com/charlesonunze/fw/example/internal/modules/user/service"
	"github.com/go-chi/chi/v5"
)

// UserModule is the user module.
type UserModule struct {
	service *service.UserService
	handler *api.UserHandler
}

// New creates a new UserModule.
func New() *UserModule {
	return &UserModule{}
}

func (m *UserModule) Name() string { return "user" }

func (m *UserModule) Init(deps *fw.Deps) error {
	r := repo.New()
	m.service = service.New(r)
	m.handler = api.New(m.service)

	deps.Services.Register(m.service)

	return nil
}

func (m *UserModule) RegisterRoutes(r chi.Router) {
	r.Route("/users", func(r chi.Router) {
		r.Post("/", m.handler.CreateUser)
		r.Get("/", m.handler.ListUsers)
		r.Get("/{id}", m.handler.GetUser)
	})
}

func (m *UserModule) Close() error { return nil }
