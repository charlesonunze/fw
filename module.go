package fw

import "context"

// Module represents a self-contained application module.
// Each module owns its domain logic, data layer, and transport handlers.
//
// To expose HTTP routes, implement HTTPModule.
// To expose gRPC services, implement GRPCModule.
// Both are optional and discovered via type assertion at startup.
type Module interface {
	// Name returns the unique module name (e.g. "user", "order").
	Name() string

	// Init initializes the module with shared app dependencies.
	// This is where you wire your services, repos, and handlers,
	// and register services in the registry for other modules to use.
	//
	// Important: do not call fw.GetService here to look up other modules'
	// services — not all modules may be initialized yet. Resolve dependencies
	// lazily inside your service methods, where all services are guaranteed
	// to be registered.
	Init(deps *Deps) error

	// Health reports whether the module is healthy.
	// fw aggregates results at GET /health/ready.
	// Return nil if healthy, an error describing the problem if not.
	Health(ctx context.Context) error

	// Close gracefully shuts down the module and releases resources.
	Close() error
}
