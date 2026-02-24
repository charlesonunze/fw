package service

import (
	"context"
	"fmt"

	"github.com/charlesonunze/fw"
	"github.com/charlesonunze/fw/example/internal/modules/order/domain"
	userservice "github.com/charlesonunze/fw/example/internal/modules/user/service"
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

// CreateOrder creates a new order after verifying the user exists.
func (s *OrderService) CreateOrder(ctx context.Context, userID string, items []domain.OrderItem) (*domain.Order, error) {
	// Look up user service from registry
	userSvc, err := fw.GetService[*userservice.UserService](s.services)
	if err != nil {
		return nil, fmt.Errorf("user service unavailable: %w", err)
	}

	// Verify user exists
	if _, err := userSvc.GetUser(ctx, userID); err != nil {
		return nil, fmt.Errorf("user %q not found: %w", userID, err)
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
