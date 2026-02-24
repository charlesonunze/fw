package fw

import (
	"fmt"
	"sync"
)

// ServiceRegistry is a thread-safe container for services.
// Modules register services by name during Init, and other modules
// look them up by name at call time.
type ServiceRegistry struct {
	mu       sync.RWMutex
	services map[string]any
}

// NewServiceRegistry creates a new empty ServiceRegistry.
func NewServiceRegistry() *ServiceRegistry {
	return &ServiceRegistry{
		services: make(map[string]any),
	}
}

// Register adds a service to the registry using the service's Name() as the key.
// Panics if a service with the same name is already registered.
func (r *ServiceRegistry) Register(svc Service) {
	r.mu.Lock()
	defer r.mu.Unlock()
	name := svc.Name()
	if _, exists := r.services[name]; exists {
		panic(fmt.Sprintf("fw: service `%q` is already registered", name))
	}
	r.services[name] = svc
}

// GetService retrieves a service from the registry using the type's Name() method as the key.
// The type parameter T must implement the Service interface.
// Returns an error if the service is not found or the type assertion fails.
func GetService[T Service](reg *ServiceRegistry) (T, error) {
	var zero T
	name := zero.Name()

	reg.mu.RLock()
	defer reg.mu.RUnlock()

	svc, ok := reg.services[name]
	if !ok {
		return zero, fmt.Errorf("fw: service `%q` not registered", name)
	}

	typed, ok := svc.(T)
	if !ok {
		return zero, fmt.Errorf("fw: service `%q` is not of expected type %T", name, zero)
	}

	return typed, nil
}

// MustGetService retrieves a service from the registry and panics if not found or wrong type.
// Use this when a missing service is a fatal programming error.
func MustGetService[T Service](reg *ServiceRegistry) T {
	svc, err := GetService[T](reg)
	if err != nil {
		panic(err)
	}
	return svc
}
