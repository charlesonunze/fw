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

	// Register constructs and exposes the module's services. fw calls Register
	// on every module before calling Init on any module. Application services
	// registered with App.RegisterService are already available in deps. Resolve
	// services owned by other modules during Init, not Register.
	Register(deps *Deps) error

	// Init resolves services exposed by other modules and completes the module's
	// wiring. Every module service has been registered before Init is called, but
	// another module's Init may not have run yet.
	Init(deps *Deps) error

	// Health reports whether the module is healthy.
	// fw aggregates results at GET /health/ready.
	// Return nil if healthy, an error describing the problem if not.
	Health(ctx context.Context) error

	// Close gracefully shuts down the module and releases resources. It must be
	// safe to call after Register or Init returns an error, and after Register
	// succeeds even if Init is never called.
	Close() error
}
