package fw

// Service is the interface that all registered services must implement.
// The Name method returns the unique key used for registration and lookup
// in the ServiceRegistry.
type Service interface {
	Name() string
	Close() error
}
