package fw

import (
	"fmt"
	"math/rand/v2"
	"reflect"
	"slices"
	"testing"
)

func BenchmarkServiceOrdering(b *testing.B) {
	for _, shape := range []string{"independent", "star", "chain", "layered", "mixed", "wide_refill"} {
		for _, n := range []int{10, 100, 1000} {
			b.Run(fmt.Sprintf("shape=%s/n=%d", shape, n), func(b *testing.B) {
				edges := make([][]int, n)
				registration := make([]int, n)
				rng := rand.New(rand.NewPCG(73, 196))
				for i := range n {
					registration[i] = n - 1 - i
					switch shape {
					case "star":
						if i > 0 {
							edges[i] = []int{0}
						}
					case "chain":
						if i > 0 {
							edges[i] = []int{i - 1}
						}
					case "layered":
						if i >= 10 {
							previous := (i/10 - 1) * 10
							edges[i] = []int{previous + i%10, previous + (i+1)%10}
						}
					case "mixed":
						if i > 0 {
							for range 3 {
								dependency := rng.IntN(i)
								if !slices.Contains(edges[i], dependency) {
									edges[i] = append(edges[i], dependency)
								}
							}
						}
					case "wide_refill":
						// Earlier chain nodes keep becoming eligible while independent services wait.
						if i < n/2 {
							registration[i] = n/2 - 1 - i
							if i > 0 {
								edges[i] = []int{i - 1}
							}
						} else {
							registration[i] = i
						}
					}
				}
				if shape == "mixed" {
					registration = rng.Perm(n)
				}
				fixture := newServiceOrderingFixture(edges, registration)
				var got []Service
				var err error
				b.ReportAllocs()
				for b.Loop() {
					got, err = orderApplicationServices(fixture.services, fixture.dependencies, fixture.registry)
					if err != nil {
						b.Fatal(err)
					}
				}
				if !slices.Equal(got, fixture.want) {
					b.Fatalf("order = %v, want %v", serviceNames(got), serviceNames(fixture.want))
				}
			})
		}
	}
}

type serviceOrderingFixture struct {
	services     []Service
	dependencies map[string][]reflect.Type
	registry     *ServiceRegistry
	want         []Service
}

func newServiceOrderingFixture(edges [][]int, registration []int) serviceOrderingFixture {
	fixture := serviceOrderingFixture{
		dependencies: make(map[string][]reflect.Type),
		registry:     NewServiceRegistry(),
	}
	services := make([]Service, len(edges))
	keys := make([]reflect.Type, len(edges))
	for i := range edges {
		services[i] = &graphService{name: fmt.Sprintf("service-%04d", i)}
		// Synthetic keys isolate graph ordering from public interface registration.
		keys[i] = reflect.ArrayOf(i+1, reflect.TypeFor[byte]())
		fixture.registry.state.providers[keys[i]] = []serviceProvider{{name: services[i].Name(), service: services[i]}}
	}
	for i, dependencies := range edges {
		for _, dependency := range dependencies {
			fixture.dependencies[services[i].Name()] = append(fixture.dependencies[services[i].Name()], keys[dependency])
		}
	}
	for _, i := range registration {
		fixture.services = append(fixture.services, services[i])
	}

	// Independent oracle: repeatedly scan registration order for the first eligible service.
	done := make([]bool, len(edges))
	for len(fixture.want) < len(services) {
		found := false
		for _, i := range registration {
			if done[i] || slices.ContainsFunc(edges[i], func(dependency int) bool { return !done[dependency] }) {
				continue
			}
			fixture.want = append(fixture.want, services[i])
			done[i], found = true, true
			break
		}
		if !found {
			panic("cyclic service ordering fixture")
		}
	}
	return fixture
}
