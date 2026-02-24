package api

import (
	"errors"
	"net/http"

	"github.com/charlesonunze/fw/example/internal/modules/order/domain"
	"github.com/charlesonunze/fw/example/internal/modules/order/service"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
)

// CreateOrderRequest is the API request for creating an order.
type CreateOrderRequest struct {
	UserID string             `json:"user_id"`
	Items  []domain.OrderItem `json:"items"`
}

// Bind validates the request after decoding.
func (req *CreateOrderRequest) Bind(r *http.Request) error {
	if req.UserID == "" {
		return errors.New("user_id is required")
	}
	if len(req.Items) == 0 {
		return errors.New("at least one item is required")
	}
	return nil
}

// OrderHandler handles HTTP requests for the order module.
type OrderHandler struct {
	service *service.OrderService
}

// New creates a new OrderHandler.
func New(svc *service.OrderService) *OrderHandler {
	return &OrderHandler{service: svc}
}

// CreateOrder handles POST /orders
func (h *OrderHandler) CreateOrder(w http.ResponseWriter, r *http.Request) {
	var req CreateOrderRequest
	if err := render.Bind(r, &req); err != nil {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, map[string]string{"error": err.Error()})
		return
	}

	order, err := h.service.CreateOrder(r.Context(), req.UserID, req.Items)
	if err != nil {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, map[string]string{"error": err.Error()})
		return
	}

	render.Status(r, http.StatusCreated)
	render.JSON(w, r, order)
}

// GetOrder handles GET /orders/{id}
func (h *OrderHandler) GetOrder(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	order, err := h.service.GetOrder(r.Context(), id)
	if err != nil {
		render.Status(r, http.StatusNotFound)
		render.JSON(w, r, map[string]string{"error": "order not found"})
		return
	}

	render.JSON(w, r, order)
}

// GetUserOrders handles GET /orders?user_id=xxx
func (h *OrderHandler) GetUserOrders(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, map[string]string{"error": "user_id query param required"})
		return
	}

	orders, err := h.service.GetUserOrders(r.Context(), userID)
	if err != nil {
		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, map[string]string{"error": "failed to list orders"})
		return
	}

	render.JSON(w, r, orders)
}
