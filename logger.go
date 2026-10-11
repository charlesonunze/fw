package fw

import (
	"log/slog"
	"os"
)

// Logger is the logging interface fw passes to modules.
// The default implementation wraps log/slog.
// Set Config.Logger to an adapter for zap, zerolog, or another logger.
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

func newDefaultLogger() Logger {
	l := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	return &slogLogger{l: l}
}

func (l *slogLogger) Info(msg string, args ...any) { l.l.Info(msg, args...) }

func (l *slogLogger) Error(msg string, args ...any) { l.l.Error(msg, args...) }

func (l *slogLogger) Debug(msg string, args ...any) { l.l.Debug(msg, args...) }

func (l *slogLogger) Warn(msg string, args ...any) { l.l.Warn(msg, args...) }

func (l *slogLogger) With(args ...any) Logger { return &slogLogger{l: l.l.With(args...)} }
