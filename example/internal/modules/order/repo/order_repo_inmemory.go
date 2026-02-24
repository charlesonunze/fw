package repo

import (
	"context"
	"crypto/rand"
	"fmt"
	"sync"

	"github.com/charlesonunze/fw/example/internal/modules/order/domain"
)

// OrderInMemoryRepo is a simple in-memory order repository for demonstration.
type OrderInMemoryRepo struct {
	mu     sync.RWMutex
	orders map[string]*domain.Order
}

// New creates a new in-memory order repository.
func New() *OrderInMemoryRepo {
	return &OrderInMemoryRepo{
		orders: make(map[string]*domain.Order),
	}
}

func (r *OrderInMemoryRepo) Create(_ context.Context, order *domain.Order) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	order.ID = generateID()
	r.orders[order.ID] = order
	return nil
}

func (r *OrderInMemoryRepo) FindByID(_ context.Context, id string) (*domain.Order, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	order, ok := r.orders[id]
	if !ok {
		return nil, fmt.Errorf("order %q not found", id)
	}
	return order, nil
}

func (r *OrderInMemoryRepo) FindByUserID(_ context.Context, userID string) ([]*domain.Order, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var orders []*domain.Order
	for _, o := range r.orders {
		if o.UserID == userID {
			orders = append(orders, o)
		}
	}
	return orders, nil
}

func generateID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}
