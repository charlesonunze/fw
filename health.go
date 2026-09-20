package fw

import (
	"context"
	"sync"
	"time"
)

const healthCheckTimeout = 5 * time.Second

type componentHealthState struct {
	errorMessage string
}

type healthComponent struct {
	kind string
	name string
}

// HealthReport is the sanitized application health state exposed to transports.
// Module and application-service errors are omitted and logged only when their
// health state changes.
type HealthReport struct {
	Healthy  bool
	Modules  map[string]bool
	Services map[string]bool
}

type healthEvaluator struct {
	mu     sync.Mutex
	states map[healthComponent]componentHealthState
}

func newHealthEvaluator() *healthEvaluator {
	return &healthEvaluator{states: make(map[healthComponent]componentHealthState)}
}

func (a *App) evaluateHealth(ctx context.Context) HealthReport {
	ctx, cancel := context.WithTimeout(ctx, healthCheckTimeout)
	defer cancel()

	report := HealthReport{
		Healthy:  a.ready.Load(),
		Modules:  make(map[string]bool, len(a.modules)),
		Services: make(map[string]bool, len(a.preRegistered)),
	}
	for _, module := range a.modules {
		name := string(module.Name())
		err := module.Health(ctx)
		healthy := err == nil
		report.Modules[name] = healthy
		if !healthy {
			report.Healthy = false
		}
		a.health.record(a.logger, "module", name, err)
	}
	for _, service := range a.preRegistered {
		name := service.Name()
		err := service.Health(ctx)
		healthy := err == nil
		report.Services[name] = healthy
		if !healthy {
			report.Healthy = false
		}
		a.health.record(a.logger, "service", name, err)
	}
	return report
}

func (e *healthEvaluator) record(logger Logger, kind, name string, healthErr error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	component := healthComponent{kind: kind, name: name}
	previous, observed := e.states[component]
	current := componentHealthState{}
	if healthErr != nil {
		current.errorMessage = healthErr.Error()
	}

	switch {
	case healthErr != nil && (!observed || previous.errorMessage == ""):
		logger.Warn(kind+" became unhealthy", kind, name, "error", healthErr)
	case healthErr != nil && previous.errorMessage != current.errorMessage:
		logger.Warn(kind+" health error changed", kind, name, "error", healthErr)
	case healthErr == nil && observed && previous.errorMessage != "":
		logger.Info(kind+" recovered", kind, name)
	}

	e.states[component] = current
}
