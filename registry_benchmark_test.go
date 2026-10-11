package fw

import "testing"

func BenchmarkGetService(b *testing.B) {
	for _, scoped := range []bool{false, true} {
		name := "application"
		registry := NewServiceRegistry()
		providerRegistry := registry
		if scoped {
			name = "module"
			providerRegistry = registry.forModule("user", nil)
			registry = registry.forModule("todo", []ModuleName{"user"})
			if err := registry.forModule("hidden", nil).Register(&registryService{name: "hidden"}); err != nil {
				b.Fatal(err)
			}
		}
		want := &registryService{name: "user"}
		if err := providerRegistry.Register(want, As[registryPublisher]()); err != nil {
			b.Fatal(err)
		}
		b.Run(name+"/concrete", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				got, err := GetService[*registryService](registry)
				if err != nil || got != want {
					b.Fatalf("GetService() = %v, %v; want %v", got, err, want)
				}
			}
		})
		b.Run(name+"/interface", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				got, err := GetService[registryPublisher](registry)
				if err != nil || got != want {
					b.Fatalf("GetService() = %v, %v; want %v", got, err, want)
				}
			}
		})
	}
}
