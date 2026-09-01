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
	// another module's Init may not have run yet. The context is cancelled when
	// startup is interrupted or application shutdown begins.
	Init(ctx context.Context, deps *Deps) error

	// Health reports whether the module is healthy. fw aggregates results for
	// the configured HTTP readiness and gRPC health services.
	// Return nil if healthy, or an error describing the problem if not. Errors
	// are logged on health transitions but are not exposed by built-in health
	// endpoints, so they must not contain secrets.
	Health(ctx context.Context) error

	// Close gracefully shuts down the module and releases resources. It must be
	// safe to call after Register or Init returns an error, and after Register
	// succeeds even if Init is never called.
	Close() error
}

// Runner is implemented by modules and application services that own
// background work. Run must block until ctx is cancelled or a fatal error
// occurs. Returning before cancellation stops the whole application.
type Runner interface {
	Run(ctx context.Context) error
}

// Stopper is implemented when cancellation alone is insufficient to quiesce
// active work. Stop runs before dependencies are closed and must honor ctx.
type Stopper interface {
	Stop(ctx context.Context) error
}
