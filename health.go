package fw

import (
	"context"
	"sync"
	"time"
)

const healthCheckTimeout = 5 * time.Second

type moduleHealthState struct {
	errorMessage string
}

type healthReport struct {
	healthy bool
	modules map[string]bool
}

type healthEvaluator struct {
	mu     sync.Mutex
	states map[string]moduleHealthState
}

func newHealthEvaluator() *healthEvaluator {
	return &healthEvaluator{states: make(map[string]moduleHealthState)}
}

func (a *App) evaluateHealth(ctx context.Context) healthReport {
	ctx, cancel := context.WithTimeout(ctx, healthCheckTimeout)
	defer cancel()

	report := healthReport{
		healthy: true,
		modules: make(map[string]bool, len(a.modules)),
	}
	for _, module := range a.modules {
		err := module.Health(ctx)
		healthy := err == nil
		report.modules[module.Name()] = healthy
		if !healthy {
			report.healthy = false
		}
		a.health.record(a.logger, module.Name(), err)
	}
	return report
}

func (e *healthEvaluator) record(logger Logger, module string, healthErr error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	previous, observed := e.states[module]
	current := moduleHealthState{}
	if healthErr != nil {
		current.errorMessage = healthErr.Error()
	}

	switch {
	case healthErr != nil && (!observed || previous.errorMessage == ""):
		logger.Warn("module became unhealthy", "module", module, "error", healthErr)
	case healthErr != nil && previous.errorMessage != current.errorMessage:
		logger.Warn("module health error changed", "module", module, "error", healthErr)
	case healthErr == nil && observed && previous.errorMessage != "":
		logger.Info("module recovered", "module", module)
	}

	e.states[module] = current
}
