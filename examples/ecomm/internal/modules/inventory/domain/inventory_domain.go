package domain

import "context"

// Item represents an inventory item.
type Item struct {
	ID       string
	Quantity int32
}

// InventoryRepository defines the persistence contract for inventory.
type InventoryRepository interface {
	Get(ctx context.Context, itemID string) (*Item, error)
	List(ctx context.Context) ([]*Item, error)
	Update(ctx context.Context, item *Item) error
	Create(ctx context.Context, item *Item) error
}
