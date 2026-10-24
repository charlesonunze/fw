package fw

import (
	"container/heap"
	"fmt"
	"reflect"
	"slices"
	"strings"
)

func orderApplicationServices(
	services []Service,
	dependenciesByName map[string][]reflect.Type,
	registry *ServiceRegistry,
) ([]Service, error) {
	if len(services) == 0 {
		return nil, nil
	}

	byName := make(map[string]Service, len(services))
	registrationIndex := make(map[string]int, len(services))
	for index, service := range services {
		name := service.Name()
		byName[name] = service
		registrationIndex[name] = index
	}

	dependencies := make(map[string][]string, len(services))
	for _, service := range services {
		name := service.Name()
		seen := make(map[string]struct{}, len(dependenciesByName[name]))
		for _, target := range dependenciesByName[name] {
			provider, ok := registry.providerFor(target)
			if !ok || provider.owner != "" {
				return nil, fmt.Errorf(
					"fw: application service %q depends on unregistered provider type %v",
					name,
					target,
				)
			}
			if provider.name == name {
				return nil, fmt.Errorf(
					"fw: application service %q depends on itself through provider type %v",
					name,
					target,
				)
			}
			if _, finalizer := service.(Finalizer); finalizer {
				if _, dependencyFinalizer := provider.service.(Finalizer); !dependencyFinalizer {
					return nil, fmt.Errorf(
						"fw: finalizer application service %q depends on non-finalizer service %q through provider type %v",
						name,
						provider.name,
						target,
					)
				}
			}
			if _, exists := seen[provider.name]; exists {
				continue
			}
			seen[provider.name] = struct{}{}
			dependencies[name] = append(dependencies[name], provider.name)
		}
		slices.SortFunc(dependencies[name], func(left, right string) int {
			return registrationIndex[left] - registrationIndex[right]
		})
	}

	const (
		unvisited uint8 = iota
		visiting
		visited
	)
	states := make(map[string]uint8, len(services))
	stack := make([]string, 0, len(services))

	var visit func(string) error
	visit = func(name string) error {
		switch states[name] {
		case visited:
			return nil
		case visiting:
			start := slices.Index(stack, name)
			cycle := append(append([]string(nil), stack[start:]...), name)
			return fmt.Errorf(
				"fw: application service dependency cycle: %s",
				strings.Join(cycle, " -> "),
			)
		}

		states[name] = visiting
		stack = append(stack, name)
		for _, dependency := range dependencies[name] {
			if err := visit(dependency); err != nil {
				return err
			}
		}
		stack = stack[:len(stack)-1]
		states[name] = visited
		return nil
	}

	for _, service := range services {
		if err := visit(service.Name()); err != nil {
			return nil, err
		}
	}

	indegree := make(map[string]int, len(services))
	dependents := make(map[string][]string, len(services))
	for _, service := range services {
		name := service.Name()
		indegree[name] = len(dependencies[name])
		for _, dependency := range dependencies[name] {
			dependents[dependency] = append(dependents[dependency], name)
		}
	}
	for dependency := range dependents {
		slices.SortFunc(dependents[dependency], func(left, right string) int {
			return registrationIndex[left] - registrationIndex[right]
		})
	}

	available := serviceHeap{
		services:  make([]Service, 0, len(services)),
		positions: registrationIndex,
	}
	for _, service := range services {
		if indegree[service.Name()] == 0 {
			available.services = append(available.services, service)
		}
	}
	heap.Init(&available)
	ordered := make([]Service, 0, len(services))
	for available.Len() > 0 {
		service := heap.Pop(&available).(Service)
		ordered = append(ordered, service)
		for _, dependent := range dependents[service.Name()] {
			indegree[dependent]--
			if indegree[dependent] == 0 {
				heap.Push(&available, byName[dependent])
			}
		}
	}
	return ordered, nil
}

// serviceHeap chooses the earliest registered service among eligible services.
type serviceHeap struct {
	services  []Service
	positions map[string]int
}

func (h serviceHeap) Len() int { return len(h.services) }

func (h serviceHeap) Less(i, j int) bool {
	return h.positions[h.services[i].Name()] < h.positions[h.services[j].Name()]
}

func (h serviceHeap) Swap(i, j int) {
	h.services[i], h.services[j] = h.services[j], h.services[i]
}

func (h *serviceHeap) Push(value any) {
	h.services = append(h.services, value.(Service))
}

func (h *serviceHeap) Pop() any {
	last := len(h.services) - 1
	service := h.services[last]
	h.services[last] = nil
	h.services = h.services[:last]
	return service
}
