package service

import (
	"context"

	"github.com/charlesonunze/fw/example/internal/modules/user/domain"
)

// UserService contains the business logic for users.
type UserService struct {
	repo domain.UserRepository
}

// New creates a new UserService.
func New(repo domain.UserRepository) *UserService {
	return &UserService{repo: repo}
}

// Name returns the service registry key.
func (s *UserService) Name() string { return "user.service" }

// CreateUser creates a new user.
func (s *UserService) CreateUser(ctx context.Context, name, email string) (*domain.User, error) {
	u := &domain.User{
		Name:  name,
		Email: email,
	}

	if err := s.repo.Create(ctx, u); err != nil {
		return nil, err
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
