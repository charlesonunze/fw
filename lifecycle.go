package fw

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"sync/atomic"

	"google.golang.org/grpc"
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
		return a.shutdownStartup(trigger, nil)
	}
	if err := a.setup(); err != nil {
		return a.shutdownStartup(shutdownTrigger{cause: err}, err)
	}

	deps := &Deps{Logger: a.logger, Services: a.services}
	if err := a.registerModules(deps); err != nil {
		return a.shutdownStartup(shutdownTrigger{cause: err}, err)
	}
	if trigger, ok := a.pendingShutdown(ctx); ok {
		return a.shutdownStartup(trigger, nil)
	}

	if err := a.initModules(runtimeCtx, deps); err != nil {
		if trigger, ok := a.pendingShutdown(ctx); ok && cancellationError(runtimeCtx, err) {
			return a.shutdownStartup(trigger, nil)
		}
		return a.shutdownStartup(shutdownTrigger{cause: err}, err)
	}
	if trigger, ok := a.pendingShutdown(ctx); ok {
		return a.shutdownStartup(trigger, nil)
	}

	a.registerRoutes()
	if err := a.buildGRPCServer(); err != nil {
		return a.shutdownStartup(shutdownTrigger{cause: err}, err)
	}

	transports, err := a.prepareTransports()
	if err != nil {
		return a.shutdownStartup(shutdownTrigger{cause: err}, err)
	}
	if trigger, ok := a.pendingShutdown(ctx); ok {
		if closeErr := transports.closeListeners(); closeErr != nil {
			return a.shutdownStartup(trigger, closeErr)
		}
		return a.shutdownStartup(trigger, nil)
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

func (a *App) shutdownStartup(trigger shutdownTrigger, startupErr error) error {
	a.transition(appStateStopping)
	a.ready.Store(false)
	a.cancelRuntime(trigger.cause)

	ctx, cancel := shutdownContext(trigger.shutdownContext)
	defer cancel()

	// Application-service runners have not started yet. Their resources are
	// released by the close phase, while modules whose Init was attempted get a
	// chance to quiesce partial startup work.
	stopErr := a.stopModules(ctx)
	return a.finish(errors.Join(startupErr, stopErr))
}

func (a *App) shutdownRunning(trigger shutdownTrigger, transports *preparedTransports, components *componentGroup) error {
	a.transition(appStateStopping)
	a.ready.Store(false)
	components.stopping.Store(true)

	ctx, cancel := shutdownContext(trigger.shutdownContext)
	defer cancel()

	transportDone := make(chan error, 1)
	go func() {
		transportDone <- a.shutdownServersWithContext(ctx, transports.httpServer)
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

type preparedTransports struct {
	httpServer   *http.Server
	httpListener net.Listener
	grpcListener net.Listener
}

func (a *App) prepareTransports() (*preparedTransports, error) {
	transports := &preparedTransports{httpServer: a.buildHTTPServer()}
	if transports.httpServer != nil {
		listener, err := net.Listen("tcp", a.httpConfig.Addr)
		if err != nil {
			return nil, fmt.Errorf("fw: failed to listen on HTTP addr %q: %w", a.httpConfig.Addr, err)
		}
		transports.httpListener = listener
	}

	if a.grpcServer != nil {
		listener, err := net.Listen("tcp", a.grpcConfig.Addr)
		if err != nil {
			closeErr := transports.closeListeners()
			return nil, errors.Join(
				fmt.Errorf("fw: failed to listen on gRPC addr %q: %w", a.grpcConfig.Addr, err),
				closeErr,
			)
		}
		transports.grpcListener = listener
	}
	return transports, nil
}

func (t *preparedTransports) closeListeners() error {
	var closeErrors []error
	if t.httpListener != nil {
		if err := t.httpListener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			closeErrors = append(closeErrors, fmt.Errorf("fw: close HTTP listener: %w", err))
		}
	}
	if t.grpcListener != nil {
		if err := t.grpcListener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			closeErrors = append(closeErrors, fmt.Errorf("fw: close gRPC listener: %w", err))
		}
	}
	return errors.Join(closeErrors...)
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

func (a *App) startComponents(ctx context.Context, transports *preparedTransports) *componentGroup {
	components := newComponentGroup(len(a.registeredModules) + len(a.preRegistered) + 2)

	if transports.httpListener != nil {
		components.transportWG.Add(1)
		go func() {
			defer components.transportWG.Done()
			a.logger.Info("starting http server", "addr", a.httpConfig.Addr)
			err := transports.httpServer.Serve(transports.httpListener)
			components.recordTransportResult("http", err, errors.Is(err, http.ErrServerClosed))
		}()
	}
	if transports.grpcListener != nil {
		components.transportWG.Add(1)
		go func() {
			defer components.transportWG.Done()
			a.logger.Info("starting grpc server", "addr", a.grpcConfig.Addr)
			err := a.grpcServer.Serve(transports.grpcListener)
			components.recordTransportResult("grpc", err, errors.Is(err, grpc.ErrServerStopped))
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

func (g *componentGroup) recordTransportResult(name string, err error, expectedStop bool) {
	if g.stopping.Load() && (err == nil || expectedStop) {
		return
	}
	if err == nil || expectedStop {
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

func (a *App) shutdownServersWithContext(ctx context.Context, httpServer *http.Server) error {
	var shutdowns sync.WaitGroup
	errorsCh := make(chan error, 2)
	if httpServer != nil {
		shutdowns.Add(1)
		go func() {
			defer shutdowns.Done()
			if err := a.shutdownHTTPServer(ctx, httpServer); err != nil {
				errorsCh <- err
			}
		}()
	}

	if a.grpcServer != nil {
		shutdowns.Add(1)
		go func() {
			defer shutdowns.Done()
			if err := a.shutdownGRPCServer(ctx); err != nil {
				errorsCh <- err
			}
		}()
	}
	shutdowns.Wait()
	close(errorsCh)

	var shutdownErrors []error
	for err := range errorsCh {
		shutdownErrors = append(shutdownErrors, err)
	}
	return errors.Join(shutdownErrors...)
}

func (a *App) shutdownHTTPServer(ctx context.Context, server *http.Server) error {
	if err := server.Shutdown(ctx); err != nil {
		a.logger.Warn("HTTP graceful shutdown failed, forcing close", "error", err)
		closeErr := server.Close()
		if closeErr != nil && !errors.Is(closeErr, http.ErrServerClosed) {
			a.logger.Error("HTTP server force close error", "error", closeErr)
			closeErr = fmt.Errorf("fw: force close HTTP server: %w", closeErr)
		} else {
			closeErr = nil
		}
		return errors.Join(fmt.Errorf("fw: gracefully stop HTTP server: %w", err), closeErr)
	}
	a.logger.Info("HTTP server stopped gracefully")
	return nil
}

func (a *App) shutdownGRPCServer(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		a.grpcServer.GracefulStop()
		close(done)
	}()
	select {
	case <-done:
		a.logger.Info("gRPC server stopped gracefully")
		return nil
	case <-ctx.Done():
		a.logger.Warn("gRPC graceful stop timed out, forcing stop")
		a.grpcServer.Stop()
		<-done
		return fmt.Errorf("fw: gracefully stop gRPC server: %w", ctx.Err())
	}
}
