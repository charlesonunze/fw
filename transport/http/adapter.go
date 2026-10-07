package fwhttp

import (
	"errors"
	"fmt"
	"net/http"
	"reflect"

	"github.com/charlesonunze/fw"
)

// Adapter connects a native router to the HTTP transport. It owns route
// registration, not server configuration or lifecycle. RegisterModules must
// reject incompatible RegisterRoutes signatures before registering any routes.
type Adapter interface {
	http.Handler
	RegisterModules([]fw.Module) error
	RegisterHealth(live, ready http.HandlerFunc)
}

// RegisterModules validates and registers native HTTP modules for router R.
// Modules without RegisterRoutes are ignored. Adapters use this helper to
// reject a mismatched router without invoking methods through reflection.
func RegisterModules[R any](modules []fw.Module, router R) error {
	type httpModule interface{ RegisterRoutes(R) }
	for _, module := range modules {
		if module == nil {
			return errors.New("http module cannot be nil")
		}
		if _, ok := module.(httpModule); ok {
			continue
		}
		if method, ok := reflect.TypeOf(module).MethodByName("RegisterRoutes"); ok {
			return fmt.Errorf("module %q: incompatible RegisterRoutes signature %v; expected RegisterRoutes(%v)",
				module.Name(), method.Type, reflect.TypeFor[R]())
		}
	}
	for _, module := range modules {
		if httpModule, ok := module.(httpModule); ok {
			httpModule.RegisterRoutes(router)
		}
	}
	return nil
}
