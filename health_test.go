package fw

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
)

type healthTestModule struct {
	healthErr error
}

func (*healthTestModule) Name() string                      { return "health" }
func (*healthTestModule) Register(*Deps) error              { return nil }
func (*healthTestModule) Init(context.Context, *Deps) error { return nil }
func (m *healthTestModule) Health(context.Context) error    { return m.healthErr }
func (*healthTestModule) Close() error                      { return nil }

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
	app := New(WithLogger(logger))
	app.RegisterModules(module)

	if initial := app.evaluateHealth(context.Background()); !initial.Healthy {
		t.Fatal("initial health report is degraded")
	}
	if got := len(logger.snapshot()); got != 0 {
		t.Fatalf("initial healthy check logged %d entries, want 0", got)
	}

	module.healthErr = errors.New("database unavailable: password=secret")
	report := app.evaluateHealth(context.Background())
	if report.Healthy || report.Modules[module.Name()] {
		t.Fatalf("health report = %+v, want degraded module", report)
	}

	entries := logger.snapshot()
	assertHealthLog(t, entries, 0, "warn", "module became unhealthy", module.healthErr.Error())

	module.healthErr = errors.New("cache unavailable")
	app.evaluateHealth(context.Background())
	entries = logger.snapshot()
	assertHealthLog(t, entries, 1, "warn", "module health error changed", module.healthErr.Error())

	module.healthErr = nil
	if recovered := app.evaluateHealth(context.Background()); !recovered.Healthy {
		t.Fatalf("recovered health report = %+v", recovered)
	}
	entries = logger.snapshot()
	assertHealthLog(t, entries, 2, "info", "module recovered", "")

	app.evaluateHealth(context.Background())
	if got := len(logger.snapshot()); got != 3 {
		t.Fatalf("log entries after unchanged recovery = %d, want 3", got)
	}
}

func TestHealthTransitionTrackingIsConcurrent(t *testing.T) {
	logger := &healthTestLogger{}
	module := &healthTestModule{healthErr: errors.New("database unavailable")}
	app := New(WithLogger(logger))
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
