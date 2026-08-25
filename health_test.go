package fw

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	grpc_health_v1 "google.golang.org/grpc/health/grpc_health_v1"
)

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
	module := &multiTransportModule{}
	app := New(WithLogger(logger))
	app.RegisterModules(module)

	if initial := serveReadiness(app); initial.Code != http.StatusOK {
		t.Fatalf("initial readiness status = %d, want %d", initial.Code, http.StatusOK)
	}
	if got := len(logger.snapshot()); got != 0 {
		t.Fatalf("initial healthy check logged %d entries, want 0", got)
	}

	module.healthErr = errors.New("database unavailable: password=secret")
	recorder := serveReadiness(app)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("readiness status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
	if strings.Contains(recorder.Body.String(), "database unavailable") || strings.Contains(recorder.Body.String(), "secret") {
		t.Fatalf("readiness response exposed module error: %s", recorder.Body.String())
	}

	var response struct {
		Status  string                    `json:"status"`
		Modules map[string]map[string]any `json:"modules"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if response.Status != "degraded" || response.Modules[module.Name()]["status"] != "error" {
		t.Fatalf("readiness response = %+v, want degraded module", response)
	}
	if _, exposed := response.Modules[module.Name()]["error"]; exposed {
		t.Fatalf("readiness response contains error field: %+v", response.Modules[module.Name()])
	}

	entries := logger.snapshot()
	assertHealthLog(t, entries, 0, "warn", "module became unhealthy", module.healthErr.Error())

	grpcServer := &grpcHealthServer{evaluate: app.evaluateHealth}
	grpcResponse, err := grpcServer.Check(context.Background(), &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		t.Fatalf("gRPC Check() error = %v", err)
	}
	if grpcResponse.GetStatus() != grpc_health_v1.HealthCheckResponse_NOT_SERVING {
		t.Fatalf("gRPC health status = %s, want NOT_SERVING", grpcResponse.GetStatus())
	}
	if got := len(logger.snapshot()); got != 1 {
		t.Fatalf("log entries after identical gRPC check = %d, want 1", got)
	}

	module.healthErr = errors.New("cache unavailable")
	serveReadiness(app)
	entries = logger.snapshot()
	assertHealthLog(t, entries, 1, "warn", "module health error changed", module.healthErr.Error())

	module.healthErr = nil
	if recovered := serveReadiness(app); recovered.Code != http.StatusOK {
		t.Fatalf("recovered readiness status = %d, want %d", recovered.Code, http.StatusOK)
	}
	entries = logger.snapshot()
	assertHealthLog(t, entries, 2, "info", "module recovered", "")

	serveReadiness(app)
	if got := len(logger.snapshot()); got != 3 {
		t.Fatalf("log entries after unchanged recovery = %d, want 3", got)
	}
}

func TestHealthTransitionTrackingIsConcurrent(t *testing.T) {
	logger := &healthTestLogger{}
	module := &multiTransportModule{healthErr: errors.New("database unavailable")}
	app := New(WithLogger(logger))
	app.RegisterModules(module)

	const checks = 100
	var unhealthy atomic.Int64
	var wg sync.WaitGroup
	wg.Add(checks)
	for range checks {
		go func() {
			defer wg.Done()
			if !app.evaluateHealth(context.Background()).healthy {
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

func serveReadiness(app *App) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	app.readinessHandler().ServeHTTP(recorder, request)
	return recorder
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
