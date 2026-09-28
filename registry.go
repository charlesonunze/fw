package fw

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
)

// RegistrationOption configures a service registration.
type RegistrationOption interface {
	apply(*serviceRegistration) error
}

type registrationOption func(*serviceRegistration) error

func (option registrationOption) apply(registration *serviceRegistration) error {
	return option(registration)
}

type serviceRegistration struct {
	aliases             []reflect.Type
	dependencies        []reflect.Type
	optionalReadiness   bool
	readinessConfigured bool
}

type serviceProvider struct {
	name    string
	service Service
	owner   ModuleName
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

// DependsOn declares an application service lifecycle dependency on the service
// exposed through interface T. Dependencies are validated when the application
// starts, so registration order does not matter. During shutdown, fw calls Stop
// and Close on dependents before their dependencies.
//
// DependsOn does not inject values or sequence Runner readiness. Services must
// still receive dependencies through ordinary Go constructors and be ready for
// use when registered; all Runner contexts are cancelled together.
//
// DependsOn is only valid with App.RegisterService. Module-owned service
// dependencies must be declared through Module.Imports.
func DependsOn[T any]() RegistrationOption {
	target := reflect.TypeFor[T]()
	return registrationOption(func(registration *serviceRegistration) error {
		if target.Kind() != reflect.Interface {
			return fmt.Errorf("fw: DependsOn[%v] requires an interface type", target)
		}
		for _, dependency := range registration.dependencies {
			if dependency == target {
				return fmt.Errorf("fw: dependency type %v declared more than once", target)
			}
		}
		registration.dependencies = append(registration.dependencies, target)
		return nil
	})
}

// OptionalReadiness marks an application service as non-critical to readiness.
// The service is still health checked, included in health reports, and logged
// when its health changes. An unhealthy optional service degrades application
// health without making the application unready.
//
// OptionalReadiness only affects health aggregation. Registration, startup,
// Runner failures, shutdown, and dependency resolution remain required.
// OptionalReadiness is only valid with App.RegisterService.
func OptionalReadiness() RegistrationOption {
	return registrationOption(func(registration *serviceRegistration) error {
		if registration.readinessConfigured {
			return errors.New("fw: readiness behavior configured more than once")
		}
		registration.optionalReadiness = true
		registration.readinessConfigured = true
		return nil
	})
}

// ServiceRegistry is a thread-safe container for services. Names identify
// services operationally, while exact Go types are used for dependency lookup.
// Module lifecycle dependencies receive scoped views backed by the same state.
type ServiceRegistry struct {
	initMu sync.Mutex
	state  *serviceRegistryState
	scope  *serviceScope
}

type serviceRegistryState struct {
	mu        sync.RWMutex
	services  map[string]Service
	providers map[reflect.Type][]serviceProvider
}

type serviceScope struct {
	owner   ModuleName
	imports map[ModuleName]struct{}
}

// NewServiceRegistry creates a new empty ServiceRegistry.
func NewServiceRegistry() *ServiceRegistry {
	return &ServiceRegistry{
		state: newServiceRegistryState(),
	}
}

func newServiceRegistryState() *serviceRegistryState {
	return &serviceRegistryState{
		services:  make(map[string]Service),
		providers: make(map[reflect.Type][]serviceProvider),
	}
}

func (r *ServiceRegistry) sharedState() *serviceRegistryState {
	r.initMu.Lock()
	defer r.initMu.Unlock()
	if r.state == nil {
		r.state = newServiceRegistryState()
	}
	return r.state
}

func (r *ServiceRegistry) forModule(owner ModuleName, imports []ModuleName) *ServiceRegistry {
	allowed := make(map[ModuleName]struct{}, len(imports))
	for _, imported := range imports {
		allowed[imported] = struct{}{}
	}
	return &ServiceRegistry{
		state: r.sharedState(),
		scope: &serviceScope{owner: owner, imports: allowed},
	}
}

// Register adds a service under its concrete type and any interfaces explicitly
// exposed with As. Registration is atomic: validation failures add nothing.
func (r *ServiceRegistry) Register(svc Service, options ...RegistrationOption) error {
	registration, err := buildServiceRegistration(options)
	if err != nil {
		return err
	}
	if len(registration.dependencies) > 0 {
		return errors.New("fw: DependsOn is only valid with App.RegisterService; use Module.Imports for module dependencies")
	}
	if registration.optionalReadiness {
		return errors.New("fw: OptionalReadiness is only valid with App.RegisterService")
	}
	return r.register(svc, registration)
}

func (r *ServiceRegistry) registerApplication(
	svc Service,
	options ...RegistrationOption,
) (serviceRegistration, error) {
	registration, err := buildServiceRegistration(options)
	if err != nil {
		return serviceRegistration{}, err
	}
	if err := r.register(svc, registration); err != nil {
		return serviceRegistration{}, err
	}
	return registration, nil
}

func buildServiceRegistration(options []RegistrationOption) (serviceRegistration, error) {
	registration := serviceRegistration{}
	for _, option := range options {
		if option == nil {
			return serviceRegistration{}, errors.New("fw: registration option cannot be nil")
		}
		if err := option.apply(&registration); err != nil {
			return serviceRegistration{}, err
		}
	}
	return registration, nil
}

func (r *ServiceRegistry) register(svc Service, registration serviceRegistration) error {
	if isNilService(svc) {
		return errors.New("cannot register a nil service")
	}
	name := svc.Name()
	if name == "" {
		return errors.New("service name cannot be empty")
	}

	concrete := reflect.TypeOf(svc)
	for _, alias := range registration.aliases {
		if !concrete.Implements(alias) {
			return fmt.Errorf("fw: service %q (%v) does not implement %v", name, concrete, alias)
		}
	}

	state := r.sharedState()
	state.mu.Lock()
	defer state.mu.Unlock()
	state.ensureMaps()

	if _, exists := state.services[name]; exists {
		return fmt.Errorf("service %q already registered", name)
	}
	for _, alias := range registration.aliases {
		if providers := state.providers[alias]; len(providers) > 0 {
			return fmt.Errorf("fw: provider type %v already exposed by service %q", alias, providers[0].name)
		}
	}

	provider := serviceProvider{name: name, service: svc, owner: r.owner()}
	state.services[name] = svc
	state.providers[concrete] = append(state.providers[concrete], provider)
	for _, alias := range registration.aliases {
		state.providers[alias] = []serviceProvider{provider}
	}
	return nil
}

func (r *ServiceRegistry) providerFor(target reflect.Type) (serviceProvider, bool) {
	state := r.sharedState()
	state.mu.RLock()
	defer state.mu.RUnlock()
	providers := state.providers[target]
	if len(providers) != 1 {
		return serviceProvider{}, false
	}
	return providers[0], true
}

func (r *ServiceRegistry) owner() ModuleName {
	if r.scope == nil {
		return ""
	}
	return r.scope.owner
}

func (r *ServiceRegistry) canAccess(owner ModuleName) bool {
	if r.scope == nil || owner == "" || owner == r.scope.owner {
		return true
	}
	_, allowed := r.scope.imports[owner]
	return allowed
}

func (s *serviceRegistryState) ensureMaps() {
	if s.services == nil {
		s.services = make(map[string]Service)
	}
	if s.providers == nil {
		s.providers = make(map[reflect.Type][]serviceProvider)
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
	state := reg.sharedState()
	state.mu.RLock()
	registered := state.providers[target]
	providers := make([]serviceProvider, 0, len(registered))
	for _, provider := range registered {
		if reg.canAccess(provider.owner) {
			providers = append(providers, provider)
		}
	}
	switch len(providers) {
	case 0:
		state.mu.RUnlock()
		if len(registered) > 0 && reg.scope != nil {
			return zero, fmt.Errorf("fw: module %q cannot access provider type %v from a module it does not import", reg.scope.owner, target)
		}
		return zero, fmt.Errorf("fw: provider type %v is not registered", target)
	case 1:
		service := providers[0].service
		state.mu.RUnlock()
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
		state.mu.RUnlock()
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
