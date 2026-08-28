package fw

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
)

// RegistrationOption configures the types exposed by a service registration.
type RegistrationOption interface {
	apply(*serviceRegistration) error
}

type registrationOption func(*serviceRegistration) error

func (option registrationOption) apply(registration *serviceRegistration) error {
	return option(registration)
}

type serviceRegistration struct {
	aliases []reflect.Type
}

type serviceProvider struct {
	name    string
	service Service
}

// As exposes a service through the interface T in addition to its concrete type.
// Register returns an error when T is not an interface or the service does not
// implement it.
func As[T any]() RegistrationOption {
	target := reflect.TypeFor[T]()
	return registrationOption(func(registration *serviceRegistration) error {
		if target.Kind() != reflect.Interface {
			return fmt.Errorf("fw: As[%v] requires an interface type", target)
		}
		for _, alias := range registration.aliases {
			if alias == target {
				return fmt.Errorf("fw: provider type %v exposed more than once", target)
			}
		}
		registration.aliases = append(registration.aliases, target)
		return nil
	})
}

// ServiceRegistry is a thread-safe container for services. Names identify
// services operationally, while exact Go types are used for dependency lookup.
type ServiceRegistry struct {
	mu        sync.RWMutex
	services  map[string]Service
	providers map[reflect.Type][]serviceProvider
}

// NewServiceRegistry creates a new empty ServiceRegistry.
func NewServiceRegistry() *ServiceRegistry {
	return &ServiceRegistry{
		services:  make(map[string]Service),
		providers: make(map[reflect.Type][]serviceProvider),
	}
}

// Register adds a service under its concrete type and any interfaces explicitly
// exposed with As. Registration is atomic: validation failures add nothing.
func (r *ServiceRegistry) Register(svc Service, options ...RegistrationOption) error {
	if isNilService(svc) {
		return errors.New("cannot register a nil service")
	}
	name := svc.Name()
	if name == "" {
		return errors.New("service name cannot be empty")
	}

	registration := serviceRegistration{}
	for _, option := range options {
		if option == nil {
			return errors.New("fw: registration option cannot be nil")
		}
		if err := option.apply(&registration); err != nil {
			return err
		}
	}

	concrete := reflect.TypeOf(svc)
	for _, alias := range registration.aliases {
		if !concrete.Implements(alias) {
			return fmt.Errorf("fw: service %q (%v) does not implement %v", name, concrete, alias)
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.ensureMaps()

	if _, exists := r.services[name]; exists {
		return fmt.Errorf("service %q already registered", name)
	}
	for _, alias := range registration.aliases {
		if providers := r.providers[alias]; len(providers) > 0 {
			return fmt.Errorf("fw: provider type %v already exposed by service %q", alias, providers[0].name)
		}
	}

	provider := serviceProvider{name: name, service: svc}
	r.services[name] = svc
	r.providers[concrete] = append(r.providers[concrete], provider)
	for _, alias := range registration.aliases {
		r.providers[alias] = []serviceProvider{provider}
	}
	return nil
}

func (r *ServiceRegistry) ensureMaps() {
	if r.services == nil {
		r.services = make(map[string]Service)
	}
	if r.providers == nil {
		r.providers = make(map[reflect.Type][]serviceProvider)
	}
}

func isNilService(svc Service) bool {
	if svc == nil {
		return true
	}
	value := reflect.ValueOf(svc)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

// GetService retrieves the single service exposed as the exact Go type T.
// Returns an error when no service or multiple concrete services match T.
func GetService[T any](reg *ServiceRegistry) (T, error) {
	var zero T
	if reg == nil {
		return zero, errors.New("fw: service registry is nil")
	}

	target := reflect.TypeFor[T]()
	reg.mu.RLock()
	providers := reg.providers[target]
	switch len(providers) {
	case 0:
		reg.mu.RUnlock()
		return zero, fmt.Errorf("fw: provider type %v is not registered", target)
	case 1:
		service := providers[0].service
		reg.mu.RUnlock()
		provider, ok := any(service).(T)
		if !ok {
			return zero, fmt.Errorf("fw: provider type %v has incompatible value %T", target, service)
		}
		return provider, nil
	default:
		names := make([]string, 0, len(providers))
		for _, provider := range providers {
			names = append(names, provider.name)
		}
		reg.mu.RUnlock()
		return zero, fmt.Errorf(
			"fw: provider type %v is ambiguous across services %s; expose distinct interfaces with fw.As",
			target,
			strings.Join(names, ", "),
		)
	}
}

// MustGetService retrieves a service from the registry and panics on failure.
// Use this when a missing service is a fatal programming error.
func MustGetService[T any](reg *ServiceRegistry) T {
	svc, err := GetService[T](reg)
	if err != nil {
		panic(err)
	}
	return svc
}
