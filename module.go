package fw

import "context"

// ModuleName uniquely identifies a module in an application dependency graph.
type ModuleName string

// Module represents a self-contained application module.
// Each module owns its domain logic, data layer, and transport handlers.
//
// Optional transport packages define their own module contracts and discover
// them through type assertions during transport preparation.
type Module interface {
	// Name returns the unique module name (e.g. "user", "order").
	Name() ModuleName

	// Imports declares the modules whose services this module can resolve.
	// Imports are direct-only: importing auth does not grant access to modules
	// imported by auth. fw validates and orders the graph before registration.
	Imports() []ModuleName

	// Register constructs and exposes the module's services. fw calls Register
	// in dependency order before calling Init. Application services and services
	// exposed by directly imported modules are already available in deps.
	Register(deps *Deps) error

	// Init performs post-registration setup. All module services are complete and
	// registered before Init begins. The context is cancelled when startup is
	// interrupted or application shutdown begins.
	Init(ctx context.Context, deps *Deps) error

	// Health reports whether the module is healthy. fw aggregates results for
	// the configured HTTP readiness and gRPC health services.
	// It must honor context cancellation. Return nil if healthy, or an error
	// describing the problem if not. Errors are logged on health transitions but
	// are not exposed by built-in health endpoints, so they must not contain secrets.
	Health(ctx context.Context) error

	// Close gracefully shuts down the module and releases resources. It must be
	// safe to call after Register or Init returns an error, and after Register
	// succeeds even if Init is never called.
	Close() error
}

// Runner is implemented by modules and application services that own
// background work. Run must block until ctx is cancelled or a fatal error
// occurs, and must join all work it owns before returning. Returning before
// cancellation stops the whole application. Release shared resources in Close,
// not Run, so dependents can finish using them during shutdown.
type Runner interface {
	Run(ctx context.Context) error
}

// Stopper is implemented when cancellation alone is insufficient to quiesce
// active work. Stop runs before dependencies are stopped or closed and must
// honor ctx. A non-nil error aborts remaining teardown because completion is
// uncertain. If the component also implements Runner, fw waits for Run to return
// before stopping its dependencies. Otherwise, Stop must return nil only after
// all owned work has stopped.
type Stopper interface {
	Stop(ctx context.Context) error
}

// Finalizer is implemented by application services that must remain available
// while all other resources stop and close, such as telemetry exporters.
// Finalize runs after final lifecycle logs and before Close, and must honor ctx.
// Finalizers share a timeout within the overall shutdown budget. Close must
// return promptly; it has no context and cannot be interrupted by fw.
// Declared dependencies needed during finalization must also be Finalizers.
// Finalize and Close are skipped when shutdown aborts with active or uncertain work.
type Finalizer interface {
	Finalize(ctx context.Context) error
}
