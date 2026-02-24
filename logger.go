package fw

import (
	"log/slog"
	"os"
	"strings"
)

// NewLogger creates a structured JSON logger with the given level string.
// Valid levels: "debug", "info", "warn", "error". Defaults to "info".
func NewLogger(level string) *slog.Logger {
	var lvl slog.Level

	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}

	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: lvl,
	})

	return slog.New(handler)
}
