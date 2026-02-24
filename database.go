package fw

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// OpenDatabase opens a PostgreSQL connection pool using the given config.
// Returns nil DB if no DSN is configured (allows apps without a database).
func OpenDatabase(cfg DatabaseConfig) (*sql.DB, error) {
	if cfg.DSN == "" {
		return nil, nil
	}

	db, err := sql.Open("pgx", cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("fw: failed to open database: %w", err)
	}

	if cfg.MaxOpenConns > 0 {
		db.SetMaxOpenConns(cfg.MaxOpenConns)
	}

	if cfg.MaxIdleConns > 0 {
		db.SetMaxIdleConns(cfg.MaxIdleConns)
	}

	if cfg.ConnMaxLifetime != "" {
		d, err := time.ParseDuration(cfg.ConnMaxLifetime)
		if err != nil {
			return nil, fmt.Errorf("fw: invalid conn_max_lifetime %q: %w", cfg.ConnMaxLifetime, err)
		}
		db.SetConnMaxLifetime(d)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("fw: failed to ping database: %w", err)
	}

	return db, nil
}
