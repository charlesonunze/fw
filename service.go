package fw

import "context"

// Service is the interface that all registered services must implement.
// Name is the unique operational identity used for diagnostics and lifecycle
// logging. Dependency lookup uses exact Go types instead. Health contributes to
// application readiness and must honor context cancellation.
type Service interface {
	Name() string
	Health(ctx context.Context) error
	Close() error
}
