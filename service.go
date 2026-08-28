package fw

// Service is the interface that all registered services must implement.
// Name is the unique operational identity used for diagnostics and lifecycle
// logging. Dependency lookup uses exact Go types instead.
type Service interface {
	Name() string
	Close() error
}
