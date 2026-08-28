package fw

import (
	"strings"
	"testing"
)

type registryService struct {
	name   string
	closed *int
}

func (s *registryService) Name() string {
	return s.name
}

func (*registryService) Publish() string { return "published" }

func (s *registryService) Close() error {
	if s.closed != nil {
		(*s.closed)++
	}
	return nil
}

type registryPublisher interface {
	Publish() string
}

type registryReader interface {
	Read() string
}

type alternateRegistryService struct {
	name string
}

func (s *alternateRegistryService) Name() string  { return s.name }
func (*alternateRegistryService) Publish() string { return "alternate" }
func (*alternateRegistryService) Close() error    { return nil }

func TestServiceRegistryRegisterReturnsErrors(t *testing.T) {
	tests := []struct {
		name    string
		service Service
		want    string
	}{
		{name: "nil interface", want: "nil service"},
		{name: "typed nil", service: (*registryService)(nil), want: "nil service"},
		{name: "empty name", service: &registryService{}, want: "name cannot be empty"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewServiceRegistry()
			err := registry.Register(tt.service)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Register() error = %v, want error containing %q", err, tt.want)
			}
		})
	}

	registry := NewServiceRegistry()
	first := &registryService{name: "postgres"}
	if err := registry.Register(first); err != nil {
		t.Fatalf("first Register() error = %v", err)
	}
	err := registry.Register(&registryService{name: "postgres"})
	if err == nil || !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("duplicate Register() error = %v, want duplicate error", err)
	}
	if got := registry.services["postgres"]; got != first {
		t.Fatalf("registered service = %p, want original %p", got, first)
	}
}

func TestServiceRegistryResolvesConcreteTypeWithoutCallingNameOnNil(t *testing.T) {
	registry := NewServiceRegistry()
	want := &registryService{name: "rabbitmq"}
	if err := registry.Register(want); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	got, err := GetService[*registryService](registry)
	if err != nil {
		t.Fatalf("GetService() error = %v", err)
	}
	if got != want {
		t.Fatalf("GetService() = %p, want %p", got, want)
	}
}

func TestServiceRegistryExposesInterface(t *testing.T) {
	registry := NewServiceRegistry()
	want := &registryService{name: "rabbitmq"}
	if err := registry.Register(want, As[registryPublisher]()); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	got, err := GetService[registryPublisher](registry)
	if err != nil {
		t.Fatalf("GetService() error = %v", err)
	}
	if got != want || got.Publish() != "published" {
		t.Fatalf("GetService() = %#v, want registered publisher", got)
	}
}

func TestServiceRegistryRejectsInvalidInterfaceExposure(t *testing.T) {
	tests := []struct {
		name    string
		option  RegistrationOption
		wantErr string
	}{
		{name: "concrete type", option: As[*registryService](), wantErr: "requires an interface"},
		{name: "unimplemented interface", option: As[registryReader](), wantErr: "does not implement"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewServiceRegistry()
			err := registry.Register(&registryService{name: "rabbitmq"}, tt.option)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Register() error = %v, want error containing %q", err, tt.wantErr)
			}
			if len(registry.services) != 0 || len(registry.providers) != 0 {
				t.Fatalf("failed registration mutated registry: services=%d providers=%d", len(registry.services), len(registry.providers))
			}
		})
	}
}

func TestServiceRegistryRejectsDuplicateInterfaceProvider(t *testing.T) {
	registry := NewServiceRegistry()
	if err := registry.Register(&registryService{name: "rabbitmq"}, As[registryPublisher]()); err != nil {
		t.Fatalf("first Register() error = %v", err)
	}

	err := registry.Register(&alternateRegistryService{name: "nats"}, As[registryPublisher]())
	if err == nil || !strings.Contains(err.Error(), "already exposed") {
		t.Fatalf("duplicate provider Register() error = %v, want duplicate error", err)
	}
	if _, exists := registry.services["nats"]; exists {
		t.Fatal("failed provider registration added service by name")
	}
}

func TestServiceRegistryReportsAmbiguousConcreteType(t *testing.T) {
	registry := NewServiceRegistry()
	if err := registry.Register(&registryService{name: "primary"}); err != nil {
		t.Fatalf("primary Register() error = %v", err)
	}
	if err := registry.Register(&registryService{name: "replica"}); err != nil {
		t.Fatalf("replica Register() error = %v", err)
	}

	_, err := GetService[*registryService](registry)
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("GetService() error = %v, want ambiguity error", err)
	}
}

func TestAppRegisterServiceReturnsErrorsImmediately(t *testing.T) {
	closed := 0
	app := New(WithLogger(discardLogger{}))
	first := &registryService{name: "postgres", closed: &closed}
	if err := app.RegisterService(first, As[registryPublisher]()); err != nil {
		t.Fatalf("first RegisterService() error = %v", err)
	}
	if got := app.services.services["postgres"]; got != first {
		t.Fatalf("immediately registered service = %p, want %p", got, first)
	}

	err := app.RegisterService(&registryService{name: "postgres", closed: &closed})
	if err == nil || !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("duplicate RegisterService() error = %v, want duplicate error", err)
	}
	if len(app.preRegistered) != 1 {
		t.Fatalf("owned application services = %d, want 1", len(app.preRegistered))
	}
	publisher, err := GetService[registryPublisher](app.services)
	if err != nil || publisher != first {
		t.Fatalf("GetService[registryPublisher]() = %#v, %v; want registered service", publisher, err)
	}

	app.closeResources()
	if closed != 1 {
		t.Fatalf("closed services = %d, want only the accepted service", closed)
	}
}
