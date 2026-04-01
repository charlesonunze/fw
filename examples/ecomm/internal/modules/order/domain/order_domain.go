package domain

import "context"

// Order is the domain entity.
type Order struct {
	ID     string      `json:"id"`
	UserID string      `json:"user_id"`
	Items  []OrderItem `json:"items"`
	Total  float64     `json:"total"`
}

// OrderItem represents a line item in an order.
type OrderItem struct {
	Name     string  `json:"name"`
	Quantity int     `json:"quantity"`
	Price    float64 `json:"price"`
}

// OrderRepository defines the data access interface for orders.
type OrderRepository interface {
	Create(ctx context.Context, order *Order) error
	FindByID(ctx context.Context, id string) (*Order, error)
	FindByUserID(ctx context.Context, userID string) ([]*Order, error)
}
