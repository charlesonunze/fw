package service

import (
	"context"
	"fmt"

	"github.com/charlesonunze/fw"
	inventoryservice "github.com/charlesonunze/fw/examples/ecomm/internal/modules/inventory/service"
	notificationservice "github.com/charlesonunze/fw/examples/ecomm/internal/modules/notification/service"
	"github.com/charlesonunze/fw/examples/ecomm/internal/modules/order/domain"
	userservice "github.com/charlesonunze/fw/examples/ecomm/internal/modules/user/service"
)

// OrderService contains the business logic for orders.
type OrderService struct {
	repo     domain.OrderRepository
	services *fw.ServiceRegistry
}

// New creates a new OrderService.
func New(repo domain.OrderRepository, services *fw.ServiceRegistry) *OrderService {
	return &OrderService{repo: repo, services: services}
}

// Name returns the service registry key.
func (s *OrderService) Name() string { return "order.service" }

func (m *OrderService) Close() error { return nil }

// CreateOrder creates a new order after verifying the user exists and reserving inventory.
func (s *OrderService) CreateOrder(ctx context.Context, userID string, items []domain.OrderItem) (*domain.Order, error) {
	// Look up user service from registry
	userSvc, err := fw.GetService[*userservice.UserService](s.services)
	if err != nil {
		return nil, fmt.Errorf("user service unavailable: %w", err)
	}

	// Verify user exists
	user, err := userSvc.GetUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("user %q not found: %w", userID, err)
	}

	// Reserve inventory before creating the order
	invSvc, err := fw.GetService[*inventoryservice.InventoryService](s.services)
	if err == nil {
		reserveItems := make([]inventoryservice.ReserveItem, len(items))
		for i, item := range items {
			reserveItems[i] = inventoryservice.ReserveItem{
				ItemID:   item.Name, // using item name as ID for demo
				Quantity: int32(item.Quantity),
			}
		}
		if _, err := invSvc.Reserve(ctx, reserveItems); err != nil {
			return nil, fmt.Errorf("inventory reservation failed: %w", err)
		}
	}

	// Calculate total
	var total float64
	for _, item := range items {
		total += item.Price * float64(item.Quantity)
	}

	order := &domain.Order{
		UserID: userID,
		Items:  items,
		Total:  total,
	}

	if err := s.repo.Create(ctx, order); err != nil {
		return nil, err
	}

	// Best-effort: notify the user their order was placed.
	if notifSvc, err := fw.GetService[*notificationservice.NotificationService](s.services); err == nil {
		msg := fmt.Sprintf("Hi %s, your order #%s has been placed (total: $%.2f).", user.Name, order.ID, order.Total)
		_, _ = notifSvc.Send(ctx, userID, msg)
	}

	return order, nil
}

// GetOrder returns an order by ID.
func (s *OrderService) GetOrder(ctx context.Context, id string) (*domain.Order, error) {
	return s.repo.FindByID(ctx, id)
}

// GetUserOrders returns all orders for a user.
func (s *OrderService) GetUserOrders(ctx context.Context, userID string) ([]*domain.Order, error) {
	return s.repo.FindByUserID(ctx, userID)
}
