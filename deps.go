package fw

import (
	"database/sql"
	"log/slog"
)

// Deps holds shared dependencies that are passed to every module during Init.
type Deps struct {
	// DB is the database connection pool.
	DB *sql.DB

	// Logger is the structured logger.
	Logger *slog.Logger

	// Config holds the application configuration loaded from YAML.
	Config *Config

	// Events is the in-memory event bus for async communication.
	Events *EventBus

	// Services is the service registry for cross-module dependencies.
	Services *ServiceRegistry
}
