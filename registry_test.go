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
	if s == nil {
		return "registry.service"
	}
	return s.name
}

func (s *registryService) Close() error {
	if s.closed != nil {
		(*s.closed)++
	}
	return nil
}

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

func TestAppRegisterServiceReturnsErrorsImmediately(t *testing.T) {
	closed := 0
	app := New(WithLogger(discardLogger{}))
	first := &registryService{name: "postgres", closed: &closed}
	if err := app.RegisterService(first); err != nil {
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

	app.closeResources()
	if closed != 1 {
		t.Fatalf("closed services = %d, want only the accepted service", closed)
	}
}
