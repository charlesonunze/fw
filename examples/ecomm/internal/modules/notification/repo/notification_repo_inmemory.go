package repo

import (
	"context"
	"sync"

	"github.com/charlesonunze/fw/examples/ecomm/internal/modules/notification/domain"
)

// InMemoryNotificationRepo stores notifications in memory.
type InMemoryNotificationRepo struct {
	mu    sync.Mutex
	store []*domain.Notification
}

// New creates a new in-memory notification repository.
func New() *InMemoryNotificationRepo {
	return &InMemoryNotificationRepo{}
}

func (r *InMemoryNotificationRepo) Save(_ context.Context, n *domain.Notification) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.store = append(r.store, n)
	return nil
}
