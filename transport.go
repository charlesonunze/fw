package fw

import (
	"context"
	"reflect"
	"time"
)

const defaultShutdownTimeout = 30 * time.Second

// TransportDeps contains the application facilities available while a
// transport prepares its handlers and listener. Modules must be treated as
// read-only and Health returns a sanitized application health report.
type TransportDeps struct {
	Modules []Module
	Logger  Logger
	Health  func(context.Context) HealthReport
}

// Transport is an optional application runtime such as HTTP or gRPC.
// Prepare must acquire all startup resources synchronously and clean up its
// partial state before returning an error. Run blocks until Stop is called or a
// fatal error occurs. Stop must be idempotent, work after a successful Prepare
// even when Run never started, and force release resources when ctx expires.
type Transport interface {
	Name() string
	Prepare(ctx context.Context, deps TransportDeps) error
	Run(ctx context.Context) error
	Stop(ctx context.Context) error
}

func isNilTransport(transport Transport) bool {
	if transport == nil {
		return true
	}
	value := reflect.ValueOf(transport)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
