package fw

import (
	"log/slog"
	"os"
	"strings"
)

// Logger is the logging interface fw passes to modules.
// The default implementation wraps log/slog.
// Swap it out with WithLogger() to use zap, zerolog, etc.
type Logger interface {
	Info(msg string, args ...any)
	Error(msg string, args ...any)
	Debug(msg string, args ...any)
	Warn(msg string, args ...any)
	With(args ...any) Logger
}

type slogLogger struct {
	l *slog.Logger
}

func newDefaultLogger(level string) Logger {
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
	l := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl}))
	return &slogLogger{l: l}
}

func (l *slogLogger) Info(msg string, args ...any) { l.l.Info(msg, args...) }

func (l *slogLogger) Error(msg string, args ...any) { l.l.Error(msg, args...) }

func (l *slogLogger) Debug(msg string, args ...any) { l.l.Debug(msg, args...) }

func (l *slogLogger) Warn(msg string, args ...any) { l.l.Warn(msg, args...) }

func (l *slogLogger) With(args ...any) Logger { return &slogLogger{l: l.l.With(args...)} }
