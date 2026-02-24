package fw

// Service is the interface that application services should implement.
// The Name method returns a unique identifier used for service registration
// and lookup in the ServiceRegistry.
type Service interface {
	Name() string
}
