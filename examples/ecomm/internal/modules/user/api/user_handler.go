package api

import (
	"errors"
	"net/http"

	"github.com/charlesonunze/fw/examples/ecomm/internal/modules/user/service"

	chi "github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
)

// CreateUserRequest is the API request for creating a user.
type CreateUserRequest struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// Bind validates the request after decoding.
func (req *CreateUserRequest) Bind(r *http.Request) error {
	if req.Name == "" {
		return errors.New("name is required")
	}
	if req.Email == "" {
		return errors.New("email is required")
	}
	return nil
}

// UserHandler handles HTTP requests for the user module.
type UserHandler struct {
	service *service.UserService
}

// New creates a new UserHandler.
func New(svc *service.UserService) *UserHandler {
	return &UserHandler{service: svc}
}

// CreateUser handles POST /users
func (h *UserHandler) CreateUser(w http.ResponseWriter, r *http.Request) {
	var req CreateUserRequest
	if err := render.Bind(r, &req); err != nil {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, map[string]string{"error": err.Error()})
		return
	}

	u, err := h.service.CreateUser(r.Context(), req.Name, req.Email)
	if err != nil {
		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, map[string]string{"error": "failed to create user"})
		return
	}

	render.Status(r, http.StatusCreated)
	render.JSON(w, r, u)
}

// GetUser handles GET /users/{id}
func (h *UserHandler) GetUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	u, err := h.service.GetUser(r.Context(), id)
	if err != nil {
		render.Status(r, http.StatusNotFound)
		render.JSON(w, r, map[string]string{"error": "user not found"})
		return
	}

	render.JSON(w, r, u)
}

// ListUsers handles GET /users
func (h *UserHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.service.ListUsers(r.Context())
	if err != nil {
		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, map[string]string{"error": "failed to list users"})
		return
	}

	render.JSON(w, r, users)
}
