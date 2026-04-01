package repo

import (
	"context"
	"crypto/rand"
	"fmt"
	"sync"

	"github.com/charlesonunze/fw/examples/ecomm/internal/modules/user/domain"
)

// UserInMemoryRepo is a simple in-memory user repository for demonstration.
// In a real app, this would be backed by Postgres.
type UserInMemoryRepo struct {
	mu    sync.RWMutex
	users map[string]*domain.User
}

// New creates a new in-memory user repository.
func New() *UserInMemoryRepo {
	return &UserInMemoryRepo{
		users: make(map[string]*domain.User),
	}
}

func (r *UserInMemoryRepo) Create(_ context.Context, u *domain.User) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	u.ID = generateID()
	r.users[u.ID] = u
	return nil
}

func (r *UserInMemoryRepo) FindByID(_ context.Context, id string) (*domain.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	u, ok := r.users[id]
	if !ok {
		return nil, fmt.Errorf("user %q not found", id)
	}
	return u, nil
}

func (r *UserInMemoryRepo) FindAll(_ context.Context) ([]*domain.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	users := make([]*domain.User, 0, len(r.users))
	for _, u := range r.users {
		users = append(users, u)
	}
	return users, nil
}

func generateID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}
