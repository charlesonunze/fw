package fw

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
)

func orderModules(modules []Module) ([]Module, map[ModuleName][]ModuleName, error) {
	byName := make(map[ModuleName]Module, len(modules))
	for index, module := range modules {
		if isNilModule(module) {
			return nil, nil, fmt.Errorf("fw: module %d is nil", index)
		}
		name := module.Name()
		if strings.TrimSpace(string(name)) == "" {
			return nil, nil, fmt.Errorf("fw: module %d has an empty name", index)
		}
		if _, exists := byName[name]; exists {
			return nil, nil, fmt.Errorf("fw: module name %q is registered more than once", name)
		}
		byName[name] = module
	}

	names := make([]ModuleName, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	slices.Sort(names)

	importsByName := make(map[ModuleName][]ModuleName, len(modules))
	for _, name := range names {
		module := byName[name]
		imports := append([]ModuleName(nil), module.Imports()...)
		slices.Sort(imports)
		for index, imported := range imports {
			if strings.TrimSpace(string(imported)) == "" {
				return nil, nil, fmt.Errorf("fw: module %q imports an empty module name", name)
			}
			if index > 0 && imports[index-1] == imported {
				return nil, nil, fmt.Errorf("fw: module %q imports module %q more than once", name, imported)
			}
			if _, exists := byName[imported]; !exists {
				return nil, nil, fmt.Errorf("fw: module %q imports unregistered module %q", name, imported)
			}
		}
		importsByName[name] = imports
	}

	const (
		unvisited uint8 = iota
		visiting
		visited
	)
	states := make(map[ModuleName]uint8, len(modules))
	stack := make([]ModuleName, 0, len(modules))
	ordered := make([]Module, 0, len(modules))

	var visit func(ModuleName) error
	visit = func(name ModuleName) error {
		switch states[name] {
		case visited:
			return nil
		case visiting:
			start := slices.Index(stack, name)
			cycle := append(append([]ModuleName(nil), stack[start:]...), name)
			parts := make([]string, len(cycle))
			for index, item := range cycle {
				parts[index] = string(item)
			}
			return fmt.Errorf("fw: module dependency cycle: %s", strings.Join(parts, " -> "))
		}

		states[name] = visiting
		stack = append(stack, name)
		for _, imported := range importsByName[name] {
			if err := visit(imported); err != nil {
				return err
			}
		}
		stack = stack[:len(stack)-1]
		states[name] = visited
		ordered = append(ordered, byName[name])
		return nil
	}

	for _, name := range names {
		if err := visit(name); err != nil {
			return nil, nil, err
		}
	}
	return ordered, importsByName, nil
}

func isNilModule(module Module) bool {
	if module == nil {
		return true
	}
	value := reflect.ValueOf(module)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
