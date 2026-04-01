package transform

import "github.com/charlesonunze/fw/examples/ecomm/internal/modules/order/domain"

// OrderResponse is the API response representation of an Order.
type OrderResponse struct {
	ID     string             `json:"id"`
	UserID string             `json:"user_id"`
	Items  []domain.OrderItem `json:"items"`
	Total  float64            `json:"total"`
}

// ToOrderResponse converts a domain Order to an API response.
func ToOrderResponse(o *domain.Order) OrderResponse {
	return OrderResponse{
		ID:     o.ID,
		UserID: o.UserID,
		Items:  o.Items,
		Total:  o.Total,
	}
}

// ToOrderResponseList converts a slice of domain Orders to API responses.
func ToOrderResponseList(orders []*domain.Order) []OrderResponse {
	out := make([]OrderResponse, len(orders))
	for i, o := range orders {
		out[i] = ToOrderResponse(o)
	}
	return out
}
