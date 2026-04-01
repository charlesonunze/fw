package repo

import (
	"context"
	"fmt"
	"sync"

	"github.com/charlesonunze/fw/examples/ecomm/internal/modules/inventory/domain"
)

// InMemoryInventoryRepo stores inventory in memory.
type InMemoryInventoryRepo struct {
	mu    sync.RWMutex
	items map[string]*domain.Item
}

// New creates a new in-memory inventory repository with some seed data.
func New() *InMemoryInventoryRepo {
	return &InMemoryInventoryRepo{
		items: map[string]*domain.Item{
			"widget-a": {ID: "widget-a", Quantity: 100},
			"widget-b": {ID: "widget-b", Quantity: 50},
			"gadget-x": {ID: "gadget-x", Quantity: 25},
		},
	}
}

func (r *InMemoryInventoryRepo) Get(_ context.Context, itemID string) (*domain.Item, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	item, ok := r.items[itemID]
	if !ok {
		return nil, fmt.Errorf("item %q not found", itemID)
	}
	return item, nil
}

func (r *InMemoryInventoryRepo) List(_ context.Context) ([]*domain.Item, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	items := make([]*domain.Item, 0, len(r.items))
	for _, item := range r.items {
		items = append(items, item)
	}
	return items, nil
}

func (r *InMemoryInventoryRepo) Update(_ context.Context, item *domain.Item) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.items[item.ID] = item
	return nil
}

func (r *InMemoryInventoryRepo) Create(_ context.Context, item *domain.Item) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.items[item.ID]; ok {
		return fmt.Errorf("item %q already exists", item.ID)
	}
	r.items[item.ID] = item
	return nil
}
