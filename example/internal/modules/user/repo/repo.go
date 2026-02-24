package repo

import (
	"context"

	"github.com/charlesonunze/fw/example/internal/modules/user/domain"
)

// UserRepository defines the data access interface for users.
type UserRepository interface {
	Create(ctx context.Context, user *domain.User) error
	FindByID(ctx context.Context, id string) (*domain.User, error)
	FindAll(ctx context.Context) ([]*domain.User, error)
}
