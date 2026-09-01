package fw

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
)

var errStopRequested = errors.New("fw: stop requested")

type appState uint8

const (
	appStateNew appState = iota
	appStateStarting
	appStateRunning
	appStateStopping
	appStateClosed
)

func (s appState) String() string {
	switch s {
	case appStateNew:
		return "new"
	case appStateStarting:
		return "starting"
	case appStateRunning:
		return "running"
	case appStateStopping:
		return "stopping"
	case appStateClosed:
		return "closed"
	default:
		return "unknown"
	}
}

// Start initializes the application, runs configured transports and background
// components, and blocks until ctx is cancelled, Stop is called, or a component
// fails. With no transports configured, Start runs a worker-only application.
// An App can be started only once.
func (a *App) Start(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("fw: start context is nil")
	}

	a.ensureLogger()
	if err := a.beginStart(); err != nil {
		return err
	}

	runtimeCtx, cancelRuntime := context.WithCancelCause(ctx)
	a.installRuntimeCancel(cancelRuntime)
	defer cancelRuntime(nil)

	if trigger, ok := a.pendingShutdown(ctx); ok {
		return a.shutdownStartup(trigger, nil, nil)
	}
	if err := a.setup(); err != nil {
		return a.shutdownStartup(shutdownTrigger{cause: err}, err, nil)
	}

	deps := &Deps{Logger: a.logger, Services: a.services}
	if err := a.registerModules(deps); err != nil {
		return a.shutdownStartup(shutdownTrigger{cause: err}, err, nil)
	}
	if trigger, ok := a.pendingShutdown(ctx); ok {
		return a.shutdownStartup(trigger, nil, nil)
	}

	if err := a.initModules(runtimeCtx, deps); err != nil {
		if trigger, ok := a.pendingShutdown(ctx); ok && cancellationError(runtimeCtx, err) {
			return a.shutdownStartup(trigger, nil, nil)
		}
		return a.shutdownStartup(shutdownTrigger{cause: err}, err, nil)
	}
	if trigger, ok := a.pendingShutdown(ctx); ok {
		return a.shutdownStartup(trigger, nil, nil)
	}

	transportDeps := TransportDeps{
		Modules: append([]Module(nil), a.modules...),
		Logger:  a.logger,
		Health:  a.evaluateHealth,
	}
	transports, err := a.prepareTransports(runtimeCtx, transportDeps)
	if err != nil {
		return a.shutdownStartup(shutdownTrigger{cause: err}, err, transports)
	}
	if trigger, ok := a.pendingShutdown(ctx); ok {
		return a.shutdownStartup(trigger, nil, transports)
	}

	components := a.startComponents(runtimeCtx, transports)
	if trigger, ok := a.pendingShutdown(ctx); ok {
		return a.shutdownRunning(trigger, transports, components)
	}
	a.transition(appStateRunning)
	a.ready.Store(true)

	trigger := a.waitForShutdown(ctx, components.failures)
	return a.shutdownRunning(trigger, transports, components)
}

// Stop requests graceful shutdown and waits for the Start call to finish it.
// It is safe to call concurrently and repeatedly. The first Stop context sets
// the shutdown deadline; later callers only control how long they wait.
func (a *App) Stop(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("fw: stop context is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	a.lifecycleMu.Lock()
	switch a.state {
	case appStateNew:
		a.lifecycleMu.Unlock()
		return fmt.Errorf("fw: application has not been started")
	case appStateClosed:
		err := a.shutdownErr
		a.lifecycleMu.Unlock()
		return err
	}

	if !a.stopRequested && a.state != appStateStopping {
		state := a.state
		a.stopRequested = true
		a.stopContext = ctx
		close(a.stopCh)
		if state == appStateStarting && a.runtimeCancel != nil {
			a.runtimeCancel(errStopRequested)
		}
	}
	stopped := a.stopped
	a.lifecycleMu.Unlock()

	select {
	case <-stopped:
		a.lifecycleMu.Lock()
		err := a.shutdownErr
		a.lifecycleMu.Unlock()
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (a *App) beginStart() error {
	a.lifecycleMu.Lock()
	if a.state != appStateNew {
		state := a.state
		a.lifecycleMu.Unlock()
		return fmt.Errorf("fw: application cannot start from %s state", state)
	}
	previous := a.state
	a.state = appStateStarting
	a.lifecycleMu.Unlock()

	a.ready.Store(false)
	a.logTransition(previous, appStateStarting)
	return nil
}

func (a *App) installRuntimeCancel(cancel context.CancelCauseFunc) {
	a.lifecycleMu.Lock()
	a.runtimeCancel = cancel
	stopRequested := a.stopRequested
	a.lifecycleMu.Unlock()
	if stopRequested {
		cancel(errStopRequested)
	}
}

func (a *App) transition(next appState) {
	a.lifecycleMu.Lock()
	previous := a.state
	a.state = next
	a.lifecycleMu.Unlock()
	a.logTransition(previous, next)
}

func (a *App) logTransition(previous, next appState) {
	if previous == next {
		return
	}
	a.logger.Info("application state changed", "from", previous.String(), "to", next.String())
}

type shutdownTrigger struct {
	cause           error
	shutdownContext context.Context
}

func (a *App) pendingShutdown(parent context.Context) (shutdownTrigger, bool) {
	select {
	case <-a.stopCh:
		a.lifecycleMu.Lock()
		ctx := a.stopContext
		a.lifecycleMu.Unlock()
		return shutdownTrigger{cause: errStopRequested, shutdownContext: ctx}, true
	default:
	}

	select {
	case <-parent.Done():
		return shutdownTrigger{cause: context.Cause(parent)}, true
	default:
		return shutdownTrigger{}, false
	}
}

func (a *App) waitForShutdown(parent context.Context, failures <-chan error) shutdownTrigger {
	select {
	case <-a.stopCh:
		a.lifecycleMu.Lock()
		ctx := a.stopContext
		a.lifecycleMu.Unlock()
		return shutdownTrigger{cause: errStopRequested, shutdownContext: ctx}
	case <-parent.Done():
		return shutdownTrigger{cause: context.Cause(parent)}
	case err := <-failures:
		return shutdownTrigger{cause: err}
	}
}

func (a *App) shutdownStartup(trigger shutdownTrigger, startupErr error, transports []Transport) error {
	a.transition(appStateStopping)
	a.ready.Store(false)
	a.cancelRuntime(trigger.cause)

	ctx, cancel := shutdownContext(trigger.shutdownContext)
	defer cancel()

	// Runners have not started yet. Prepared transports and modules whose Init
	// was attempted still get a chance to release partial startup work.
	stopErr := errors.Join(a.stopTransports(ctx, transports), a.stopModules(ctx))
	return a.finish(errors.Join(startupErr, stopErr))
}

func (a *App) shutdownRunning(trigger shutdownTrigger, transports []Transport, components *componentGroup) error {
	a.transition(appStateStopping)
	a.ready.Store(false)
	components.stopping.Store(true)

	ctx, cancel := shutdownContext(trigger.shutdownContext)
	defer cancel()

	transportDone := make(chan error, 1)
	go func() {
		transportDone <- a.stopTransports(ctx, transports)
	}()

	a.cancelRuntime(trigger.cause)
	moduleStopErr := a.stopModules(ctx)
	moduleRunErr := waitForComponents(ctx, components.modulesDone, "module runners")
	transportErr := <-transportDone
	transportRunErr := waitForComponents(ctx, components.transportsDone, "transports")
	serviceStopErr := a.stopApplicationServices(ctx)
	serviceRunErr := waitForComponents(ctx, components.servicesDone, "application service runners")

	return a.finish(errors.Join(
		components.failureError(),
		moduleStopErr,
		moduleRunErr,
		transportErr,
		transportRunErr,
		serviceStopErr,
		serviceRunErr,
	))
}

func (a *App) cancelRuntime(cause error) {
	a.lifecycleMu.Lock()
	cancel := a.runtimeCancel
	a.lifecycleMu.Unlock()
	if cancel != nil {
		cancel(cause)
	}
}

func shutdownContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(parent, defaultShutdownTimeout)
}

func cancellationError(ctx context.Context, err error) bool {
	if ctx.Err() == nil {
		return false
	}
	return errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, context.Cause(ctx))
}

func (a *App) finish(shutdownErr error) error {
	result := errors.Join(shutdownErr, a.closeResources())

	a.lifecycleMu.Lock()
	previous := a.state
	a.state = appStateClosed
	a.shutdownErr = result
	a.stopContext = nil
	a.runtimeCancel = nil
	close(a.stopped)
	a.lifecycleMu.Unlock()
	a.logTransition(previous, appStateClosed)
	return result
}

func (a *App) stopModules(ctx context.Context) error {
	var stopErrors []error
	for i := len(a.initializedModules) - 1; i >= 0; i-- {
		module := a.initializedModules[i]
		stopper, ok := module.(Stopper)
		if !ok {
			continue
		}
		a.logger.Info("stopping module", "module", module.Name())
		if err := stopper.Stop(ctx); err != nil {
			a.logger.Error("module stop error", "module", module.Name(), "error", err)
			stopErrors = append(stopErrors, fmt.Errorf("fw: stop module %q: %w", module.Name(), err))
		}
	}
	return errors.Join(stopErrors...)
}

func (a *App) stopApplicationServices(ctx context.Context) error {
	var stopErrors []error
	for i := len(a.preRegistered) - 1; i >= 0; i-- {
		service := a.preRegistered[i]
		stopper, ok := service.(Stopper)
		if !ok {
			continue
		}
		a.logger.Info("stopping service", "service", service.Name())
		if err := stopper.Stop(ctx); err != nil {
			a.logger.Error("service stop error", "service", service.Name(), "error", err)
			stopErrors = append(stopErrors, fmt.Errorf("fw: stop application service %q: %w", service.Name(), err))
		}
	}
	return errors.Join(stopErrors...)
}

func waitForComponents(ctx context.Context, done <-chan struct{}, name string) error {
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("fw: wait for %s: %w", name, ctx.Err())
	}
}

func (a *App) prepareTransports(ctx context.Context, deps TransportDeps) ([]Transport, error) {
	prepared := make([]Transport, 0, len(a.transports))
	for _, transport := range a.transports {
		a.logger.Info("preparing transport", "transport", transport.Name())
		if err := transport.Prepare(ctx, deps); err != nil {
			return prepared, fmt.Errorf("fw: prepare transport %q: %w", transport.Name(), err)
		}
		prepared = append(prepared, transport)
	}
	return prepared, nil
}

type componentGroup struct {
	failures chan error
	stopping atomic.Bool

	mu             sync.Mutex
	failureErrors  []error
	moduleWG       sync.WaitGroup
	serviceWG      sync.WaitGroup
	transportWG    sync.WaitGroup
	modulesDone    chan struct{}
	servicesDone   chan struct{}
	transportsDone chan struct{}
}

func newComponentGroup(capacity int) *componentGroup {
	if capacity < 1 {
		capacity = 1
	}
	return &componentGroup{
		failures:       make(chan error, capacity),
		modulesDone:    make(chan struct{}),
		servicesDone:   make(chan struct{}),
		transportsDone: make(chan struct{}),
	}
}

func (a *App) startComponents(ctx context.Context, transports []Transport) *componentGroup {
	components := newComponentGroup(len(a.registeredModules) + len(a.preRegistered) + len(transports))

	for _, transport := range transports {
		transport := transport
		components.transportWG.Add(1)
		go func() {
			defer components.transportWG.Done()
			a.logger.Info("starting transport", "transport", transport.Name())
			err := transport.Run(ctx)
			components.recordTransportResult(ctx, transport.Name(), err)
		}()
	}

	for _, module := range a.registeredModules {
		runner, ok := module.(Runner)
		if !ok {
			continue
		}
		components.startRunner(ctx, "module", module.Name(), runner, &components.moduleWG)
	}
	for _, service := range a.preRegistered {
		runner, ok := service.(Runner)
		if !ok {
			continue
		}
		components.startRunner(ctx, "application service", service.Name(), runner, &components.serviceWG)
	}

	go func() {
		components.moduleWG.Wait()
		close(components.modulesDone)
	}()
	go func() {
		components.serviceWG.Wait()
		close(components.servicesDone)
	}()
	go func() {
		components.transportWG.Wait()
		close(components.transportsDone)
	}()
	return components
}

func (g *componentGroup) startRunner(ctx context.Context, kind, name string, runner Runner, wg *sync.WaitGroup) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		err := runner.Run(ctx)
		if context.Cause(ctx) != nil && (err == nil || cancellationError(ctx, err)) {
			return
		}
		if err == nil {
			err = errors.New("stopped unexpectedly")
		}
		g.recordFailure(fmt.Errorf("fw: %s runner %q: %w", kind, name, err))
	}()
}

func (g *componentGroup) recordTransportResult(ctx context.Context, name string, err error) {
	if g.stopping.Load() && (err == nil || cancellationError(ctx, err)) {
		return
	}
	if context.Cause(ctx) != nil && (err == nil || cancellationError(ctx, err)) {
		return
	}
	if err == nil {
		err = errors.New("stopped unexpectedly")
	}
	g.recordFailure(fmt.Errorf("fw: %s transport: %w", name, err))
}

func (g *componentGroup) recordFailure(err error) {
	g.mu.Lock()
	g.failureErrors = append(g.failureErrors, err)
	g.mu.Unlock()
	g.failures <- err
}

func (g *componentGroup) failureError() error {
	g.mu.Lock()
	errs := append([]error(nil), g.failureErrors...)
	g.mu.Unlock()
	return errors.Join(errs...)
}

func (a *App) stopTransports(ctx context.Context, transports []Transport) error {
	if len(transports) == 0 {
		return nil
	}

	errorsCh := make(chan error, len(transports))
	var stops sync.WaitGroup
	stops.Add(len(transports))
	for _, transport := range transports {
		transport := transport
		go func() {
			defer stops.Done()
			a.logger.Info("stopping transport", "transport", transport.Name())
			if err := transport.Stop(ctx); err != nil {
				errorsCh <- fmt.Errorf("fw: stop transport %q: %w", transport.Name(), err)
			}
		}()
	}
	stops.Wait()
	close(errorsCh)

	var stopErrors []error
	for err := range errorsCh {
		stopErrors = append(stopErrors, err)
	}
	return errors.Join(stopErrors...)
}
