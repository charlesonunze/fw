package api

import (
	"net/http"

	"github.com/charlesonunze/fw/examples/ecomm/internal/modules/inventory/service"

	"github.com/go-chi/render"
)

// Handler handles HTTP requests for inventory.
type Handler struct {
	svc *service.InventoryService
}

// New creates a new inventory HTTP handler.
func New(svc *service.InventoryService) *Handler {
	return &Handler{svc: svc}
}

// ListInventory lists all items and their stock levels.
func (h *Handler) ListInventory(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.ListItems(r.Context())
	if err != nil {
		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, map[string]string{"error": err.Error()})
		return
	}

	result := make([]map[string]interface{}, len(items))
	for i, item := range items {
		result[i] = map[string]interface{}{
			"id":       item.ID,
			"quantity": item.Quantity,
		}
	}

	render.JSON(w, r, result)
}

// GetItem gets the stock level for a single item.
func (h *Handler) GetItem(w http.ResponseWriter, r *http.Request) {
	itemID := r.PathValue("id")
	if itemID == "" {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, map[string]string{"error": "missing item id"})
		return
	}

	item, err := h.svc.Check(r.Context(), itemID)
	if err != nil {
		render.Status(r, http.StatusNotFound)
		render.JSON(w, r, map[string]string{"error": err.Error()})
		return
	}

	render.JSON(w, r, map[string]interface{}{
		"id":       item.ID,
		"quantity": item.Quantity,
	})
}
