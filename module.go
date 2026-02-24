package fw

import "github.com/go-chi/chi/v5"

// Module represents a self-contained application module.
// Each module owns its domain logic, data layer, and transport handlers.
type Module interface {
	// Name returns the unique module name (e.g., "user", "order").
	Name() string

	// Init initializes the module with shared app dependencies.
	// This is where you create your service, repo, handlers,
	// and register services in the registry for other modules to use.
	Init(deps *Deps) error

	// RegisterRoutes mounts HTTP routes onto the given router.
	// The module is responsible for defining its own route prefixes.
	RegisterRoutes(r chi.Router)

	// Close gracefully shuts down the module and releases resources.
	Close() error
}
