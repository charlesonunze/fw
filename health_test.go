package fw

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

type healthTestModule struct {
	healthErr error
	check     func(context.Context) error
}

func (*healthTestModule) Name() ModuleName                  { return "health" }
func (*healthTestModule) Imports() []ModuleName             { return nil }
func (*healthTestModule) Register(*Deps) error              { return nil }
func (*healthTestModule) Init(context.Context, *Deps) error { return nil }
func (*healthTestModule) Close() error                      { return nil }

func (m *healthTestModule) Health(ctx context.Context) error {
	if m.check != nil {
		return m.check(ctx)
	}
	return m.healthErr
}

type healthTestService struct {
	name      string
	healthErr error
	check     func(context.Context) error
}

func (s *healthTestService) Name() string {
	if s.name == "" {
		return "postgres"
	}
	return s.name
}
func (s *healthTestService) Health(ctx context.Context) error {
	if s.check != nil {
		return s.check(ctx)
	}
	return s.healthErr
}
func (*healthTestService) Close() error { return nil }

type healthLogEntry struct {
	level string
	msg   string
	args  []any
}

type healthTestLogger struct {
	mu      sync.Mutex
	entries []healthLogEntry
}

func (l *healthTestLogger) Info(msg string, args ...any) {
	l.append("info", msg, args)
}

func (l *healthTestLogger) Error(msg string, args ...any) {
	l.append("error", msg, args)
}

func (l *healthTestLogger) Debug(msg string, args ...any) {
	l.append("debug", msg, args)
}

func (l *healthTestLogger) Warn(msg string, args ...any) {
	l.append("warn", msg, args)
}

func (l *healthTestLogger) With(...any) Logger { return l }

func (l *healthTestLogger) append(level, msg string, args []any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, healthLogEntry{
		level: level,
		msg:   msg,
		args:  append([]any(nil), args...),
	})
}

func (l *healthTestLogger) snapshot() []healthLogEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]healthLogEntry(nil), l.entries...)
}

func TestHealthSanitizesErrorsAndLogsTransitions(t *testing.T) {
	logger := &healthTestLogger{}
	module := &healthTestModule{}
	service := &healthTestService{}
	app := New(Config{Logger: logger})
	if err := app.RegisterService(service); err != nil {
		t.Fatalf("RegisterService() error = %v", err)
	}
	app.RegisterModules(module)

	if initial := app.evaluateHealth(context.Background()); !initial.Healthy {
		t.Fatal("initial health report is degraded")
	}
	if got := len(logger.snapshot()); got != 0 {
		t.Fatalf("initial healthy check logged %d entries, want 0", got)
	}

	module.healthErr = errors.New("database unavailable: password=secret")
	report := app.evaluateHealth(context.Background())
	if report.Healthy || report.Modules[string(module.Name())] {
		t.Fatalf("health report = %+v, want degraded module", report)
	}

	entries := logger.snapshot()
	assertHealthLog(t, entries, 0, "warn", "module became unhealthy", module.healthErr.Error())

	service.healthErr = errors.New("connection failed: password=secret")
	app.evaluateHealth(context.Background())
	entries = logger.snapshot()
	assertHealthLog(t, entries, 1, "warn", "service became unhealthy", service.healthErr.Error())

	module.healthErr = errors.New("cache unavailable")
	app.evaluateHealth(context.Background())
	entries = logger.snapshot()
	assertHealthLog(t, entries, 2, "warn", "module health error changed", module.healthErr.Error())

	module.healthErr = nil
	report = app.evaluateHealth(context.Background())
	if report.Healthy || !report.Modules[string(module.Name())] || report.Services[service.Name()] {
		t.Fatalf("partially recovered health report = %+v", report)
	}
	entries = logger.snapshot()
	assertHealthLog(t, entries, 3, "info", "module recovered", "")

	service.healthErr = nil
	if recovered := app.evaluateHealth(context.Background()); !recovered.Healthy {
		t.Fatalf("recovered health report = %+v", recovered)
	}
	entries = logger.snapshot()
	assertHealthLog(t, entries, 4, "info", "service recovered", "")

	app.evaluateHealth(context.Background())
	if got := len(logger.snapshot()); got != 5 {
		t.Fatalf("log entries after unchanged recovery = %d, want 5", got)
	}
}

func TestHealthTransitionTrackingIsConcurrent(t *testing.T) {
	logger := &healthTestLogger{}
	module := &healthTestModule{healthErr: errors.New("database unavailable")}
	app := New(Config{Logger: logger})
	app.RegisterModules(module)

	const checks = 100
	var unhealthy atomic.Int64
	var wg sync.WaitGroup
	wg.Add(checks)
	for range checks {
		go func() {
			defer wg.Done()
			if !app.evaluateHealth(context.Background()).Healthy {
				unhealthy.Add(1)
			}
		}()
	}
	wg.Wait()

	if got := unhealthy.Load(); got != checks {
		t.Fatalf("unhealthy reports = %d, want %d", got, checks)
	}
	entries := logger.snapshot()
	if len(entries) != 1 {
		t.Fatalf("concurrent transition logs = %d, want 1", len(entries))
	}
	assertHealthLog(t, entries, 0, "warn", "module became unhealthy", module.healthErr.Error())
}

func TestOptionalReadinessDegradesWithoutBlockingReadiness(t *testing.T) {
	logger := &healthTestLogger{}
	service := &healthTestService{
		name:      "cache",
		healthErr: errors.New("redis unavailable: password=secret"),
	}
	app := New(Config{Logger: logger})
	if err := app.RegisterService(service, OptionalReadiness()); err != nil {
		t.Fatalf("RegisterService() error = %v", err)
	}

	report := app.evaluateHealth(context.Background())
	if !report.Healthy || !report.Degraded || report.Services[service.Name()] {
		t.Fatalf("optional service health report = %+v, want ready and degraded", report)
	}
	entries := logger.snapshot()
	assertHealthLog(t, entries, 0, "warn", "service became unhealthy", service.healthErr.Error())

	service.healthErr = nil
	report = app.evaluateHealth(context.Background())
	if !report.Healthy || report.Degraded || !report.Services[service.Name()] {
		t.Fatalf("recovered optional service health report = %+v, want healthy", report)
	}
	entries = logger.snapshot()
	assertHealthLog(t, entries, 1, "info", "service recovered", "")
}

func TestHealthChecksRequiredComponentsBeforeOptionalServices(t *testing.T) {
	orders := [][]string{
		{"cache", "search", "postgres"},
		{"cache", "postgres", "search"},
		{"search", "cache", "postgres"},
		{"search", "postgres", "cache"},
		{"postgres", "cache", "search"},
		{"postgres", "search", "cache"},
	}
	for _, order := range orders {
		for _, requiredHealthy := range []bool{true, false} {
			t.Run(fmt.Sprintf("%v/requiredHealthy=%t", order, requiredHealthy), func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					var checked []string
					app := New(Config{Logger: &healthTestLogger{}})
					app.RegisterModules(&healthTestModule{check: func(ctx context.Context) error {
						checked = append(checked, "module")
						return ctx.Err()
					}})
					for _, name := range order {
						service := &healthTestService{name: name, check: func(ctx context.Context) error {
							checked = append(checked, name)
							if name == "postgres" {
								if !requiredHealthy {
									return errors.New("postgres unavailable")
								}
								return ctx.Err()
							}
							<-ctx.Done()
							return ctx.Err()
						}}
						var options []RegistrationOption
						if name != "postgres" {
							options = append(options, OptionalReadiness())
						}
						if err := app.RegisterService(service, options...); err != nil {
							t.Fatal(err)
						}
					}

					start := time.Now()
					report := app.evaluateHealth(t.Context())
					if report.Healthy != requiredHealthy || !report.Degraded ||
						!report.Modules["health"] || report.Services["postgres"] != requiredHealthy ||
						report.Services["cache"] || report.Services["search"] || len(report.Services) != 3 {
						t.Fatalf("health report = %+v", report)
					}
					if len(checked) != 3 || !slices.Equal(checked[:2], []string{"module", "postgres"}) {
						t.Fatalf("checks = %v, want module, postgres, then one optional check", checked)
					}
					if elapsed := time.Since(start); elapsed != time.Second {
						t.Fatalf("health check took %s, want shared optional budget of 1s", elapsed)
					}
				})
			})
		}
	}
}

func TestHealthOptionalBudgetLeavesTimeForResponse(t *testing.T) {
	tests := []struct {
		name          string
		callerTimeout time.Duration
		requiredDelay time.Duration
		optionalWait  time.Duration
	}{
		{name: "default", callerTimeout: 10 * time.Second, optionalWait: time.Second},
		{name: "short caller deadline", callerTimeout: 800 * time.Millisecond, optionalWait: 400 * time.Millisecond},
		{name: "slow required check", callerTimeout: 10 * time.Second, requiredDelay: 4 * time.Second, optionalWait: 500 * time.Millisecond},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				app := New(Config{Logger: &healthTestLogger{}})
				app.RegisterModules(&healthTestModule{check: func(ctx context.Context) error {
					time.Sleep(tt.requiredDelay)
					return ctx.Err()
				}})
				service := &healthTestService{name: "cache", check: func(ctx context.Context) error {
					<-ctx.Done()
					return nil // A late nil result must not count as healthy.
				}}
				if err := app.RegisterService(service, OptionalReadiness()); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(t.Context(), tt.callerTimeout)
				defer cancel()

				start := time.Now()
				report := app.evaluateHealth(ctx)
				if !report.Healthy || !report.Degraded || report.Services["cache"] {
					t.Fatalf("health report = %+v, want ready and degraded", report)
				}
				if elapsed := time.Since(start); elapsed != tt.requiredDelay+tt.optionalWait {
					t.Fatalf("health check took %s, want %s", elapsed, tt.requiredDelay+tt.optionalWait)
				}
				if err := ctx.Err(); err != nil {
					t.Fatalf("optional check exhausted caller deadline: %v", err)
				}
			})
		})
	}
}

func TestHealthStopsChecksOnCancellation(t *testing.T) {
	for _, stage := range []string{"before", "module", "postgres", "cache"} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var checked []string
			check := func(name string) func(context.Context) error {
				return func(context.Context) error {
					checked = append(checked, name)
					if name == stage {
						cancel()
					}
					return nil
				}
			}
			app := New(Config{Logger: &healthTestLogger{}})
			app.RegisterModules(&healthTestModule{check: check("module")})
			if err := app.RegisterService(&healthTestService{name: "cache", check: check("cache")}, OptionalReadiness()); err != nil {
				t.Fatal(err)
			}
			if err := app.RegisterService(&healthTestService{name: "postgres", check: check("postgres")}); err != nil {
				t.Fatal(err)
			}
			if stage == "before" {
				cancel()
			}

			report := app.evaluateHealth(ctx)
			if report.Healthy != (stage == "cache") || !report.Degraded || report.Services["cache"] {
				t.Fatalf("health report after cancellation during %s = %+v", stage, report)
			}
			all := []string{"module", "postgres", "cache"}
			want := all[:slices.Index(all, stage)+1]
			if !slices.Equal(checked, want) {
				t.Fatalf("checks = %v, want %v", checked, want)
			}
		})
	}
}

func TestHealthRequiredTimeoutSkipsRemainingChecks(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		app := New(Config{Logger: &healthTestLogger{}})
		app.RegisterModules(&healthTestModule{check: func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		}})
		for _, name := range []string{"cache", "postgres"} {
			service := &healthTestService{name: name, check: func(context.Context) error {
				t.Fatalf("%s checked after required timeout", name)
				return nil
			}}
			var options []RegistrationOption
			if name == "cache" {
				options = append(options, OptionalReadiness())
			}
			if err := app.RegisterService(service, options...); err != nil {
				t.Fatal(err)
			}
		}
		start := time.Now()
		report := app.evaluateHealth(t.Context())
		if report.Healthy || !report.Degraded || report.Modules["health"] ||
			report.Services["postgres"] || report.Services["cache"] || len(report.Services) != 2 {
			t.Fatalf("timed-out health report = %+v", report)
		}
		if elapsed := time.Since(start); elapsed != 5*time.Second {
			t.Fatalf("health check took %s, want overall budget of 5s", elapsed)
		}
	})
}

func TestHealthOptionalTimeoutLogsOnlyTransitions(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		logger := &healthTestLogger{}
		app := New(Config{Logger: logger})
		service := &healthTestService{name: "cache", check: func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		}}
		if err := app.RegisterService(service, OptionalReadiness()); err != nil {
			t.Fatal(err)
		}
		for range 2 {
			app.evaluateHealth(t.Context())
		}
		entries := logger.snapshot()
		if len(entries) != 1 {
			t.Fatalf("timeout log entries = %d, want 1", len(entries))
		}
		assertHealthLog(t, entries, 0, "warn", "service became unhealthy", context.DeadlineExceeded.Error())

		service.check = nil
		for range 2 {
			if report := app.evaluateHealth(t.Context()); !report.Healthy || report.Degraded {
				t.Fatalf("recovered report = %+v", report)
			}
		}
		entries = logger.snapshot()
		if len(entries) != 2 {
			t.Fatalf("recovery log entries = %d, want 2", len(entries))
		}
		assertHealthLog(t, entries, 1, "info", "service recovered", "")
	})
}

func assertHealthLog(t *testing.T, entries []healthLogEntry, index int, level, msg, healthErr string) {
	t.Helper()
	if len(entries) <= index {
		t.Fatalf("log entry %d missing from %+v", index, entries)
	}
	entry := entries[index]
	if entry.level != level || entry.msg != msg {
		t.Fatalf("log entry %d = %s %q, want %s %q", index, entry.level, entry.msg, level, msg)
	}
	if healthErr == "" {
		return
	}
	for i := 0; i+1 < len(entry.args); i += 2 {
		key, ok := entry.args[i].(string)
		if ok && key == "error" && fmt.Sprint(entry.args[i+1]) == healthErr {
			return
		}
	}
	t.Fatalf("log entry %d does not contain error %q: %+v", index, healthErr, entry.args)
}
