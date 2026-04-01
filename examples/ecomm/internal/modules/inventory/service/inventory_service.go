package service

import (
	"context"
	"crypto/rand"
	"fmt"

	"github.com/charlesonunze/fw/examples/ecomm/internal/modules/inventory/domain"
)

// ReserveItem is a request to reserve an item.
type ReserveItem struct {
	ItemID   string
	Quantity int32
}

// InventoryService handles stock management.
type InventoryService struct {
	repo domain.InventoryRepository
}

// New creates a new InventoryService.
func New(repo domain.InventoryRepository) *InventoryService {
	return &InventoryService{repo: repo}
}

// Name returns the service registry key.
func (s *InventoryService) Name() string { return "inventory.service" }

func (s *InventoryService) Close() error { return nil }

// Reserve attempts to lock stock for the given items.
// Returns error if any item has insufficient quantity.
// If successful, decrements quantity and returns a reservation ID.
func (s *InventoryService) Reserve(ctx context.Context, items []ReserveItem) (string, error) {
	// First pass: verify we have enough of everything
	for _, req := range items {
		item, err := s.repo.Get(ctx, req.ItemID)
		if err != nil {
			return "", fmt.Errorf("item %q unavailable: %w", req.ItemID, err)
		}
		if item.Quantity < req.Quantity {
			return "", fmt.Errorf("insufficient stock for item %q: have %d, requested %d", req.ItemID, item.Quantity, req.Quantity)
		}
	}

	// Second pass: decrement quantities
	for _, req := range items {
		item, _ := s.repo.Get(ctx, req.ItemID)
		item.Quantity -= req.Quantity
		if err := s.repo.Update(ctx, item); err != nil {
			return "", err
		}
	}

	return generateReservationID(), nil
}

// Check returns the current stock level for an item.
func (s *InventoryService) Check(ctx context.Context, itemID string) (*domain.Item, error) {
	return s.repo.Get(ctx, itemID)
}

// ListItems returns all items.
func (s *InventoryService) ListItems(ctx context.Context) ([]*domain.Item, error) {
	return s.repo.List(ctx)
}

func generateReservationID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}
