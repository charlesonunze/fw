package fw

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestNewAppliesConfigAndCopiesTransports(t *testing.T) {
	logger := discardLogger{}
	first := &lifecycleTransport{}
	second := &lifecycleTransport{}
	transports := []Transport{first}

	app := New(Config{
		Logger:          logger,
		Transports:      transports,
		ShutdownTimeout: time.Second,
	})
	transports[0] = second

	if app.logger != logger {
		t.Fatal("New() did not preserve the configured logger")
	}
	if len(app.transports) != 1 || app.transports[0] != first {
		t.Fatalf("New() transports = %v, want copied first transport", app.transports)
	}
	if app.shutdownTimeout != time.Second {
		t.Fatalf("New() shutdown timeout = %s, want %s", app.shutdownTimeout, time.Second)
	}
}

func TestNewAppliesDefaults(t *testing.T) {
	app := New(Config{})

	if app.logger != nil {
		t.Fatal("New() eagerly created the default logger")
	}
	if app.shutdownTimeout != defaultShutdownTimeout {
		t.Fatalf("New() shutdown timeout = %s, want default %s", app.shutdownTimeout, defaultShutdownTimeout)
	}
	if len(app.transports) != 0 {
		t.Fatalf("New() transports = %v, want worker-only application", app.transports)
	}
}

func TestStartRejectsNegativeShutdownTimeout(t *testing.T) {
	app := New(Config{
		Logger:          discardLogger{},
		ShutdownTimeout: -time.Second,
	})

	err := app.Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), "shutdown timeout cannot be negative") {
		t.Fatalf("Start() error = %v, want negative shutdown timeout error", err)
	}
}
