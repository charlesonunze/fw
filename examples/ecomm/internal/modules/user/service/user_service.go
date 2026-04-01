package service

import (
	"context"
	"fmt"

	"github.com/charlesonunze/fw"
	notificationservice "github.com/charlesonunze/fw/examples/ecomm/internal/modules/notification/service"
	"github.com/charlesonunze/fw/examples/ecomm/internal/modules/user/domain"
)

// UserService contains the business logic for users.
type UserService struct {
	repo     domain.UserRepository
	services *fw.ServiceRegistry
}

// New creates a new UserService.
func New(repo domain.UserRepository, services *fw.ServiceRegistry) *UserService {
	return &UserService{repo: repo, services: services}
}

// Name returns the service registry key.
func (s *UserService) Name() string { return "user.service" }

func (m *UserService) Close() error { return nil }

// CreateUser creates a new user and sends a welcome notification.
func (s *UserService) CreateUser(ctx context.Context, name, email string) (*domain.User, error) {
	u := &domain.User{
		Name:  name,
		Email: email,
	}

	if err := s.repo.Create(ctx, u); err != nil {
		return nil, err
	}

	// Best-effort: don't fail the request if notification is unavailable.
	if notifSvc, err := fw.GetService[*notificationservice.NotificationService](s.services); err == nil {
		_, _ = notifSvc.Send(ctx, u.ID, fmt.Sprintf("Welcome, %s!", u.Name))
	}

	return u, nil
}

// GetUser returns a user by ID.
func (s *UserService) GetUser(ctx context.Context, id string) (*domain.User, error) {
	return s.repo.FindByID(ctx, id)
}

// ListUsers returns all users.
func (s *UserService) ListUsers(ctx context.Context) ([]*domain.User, error) {
	return s.repo.FindAll(ctx)
}
