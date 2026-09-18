// Package observability holds the gateway's logging and metrics plumbing: how a
// logger is built, how one travels with a request, and what the service exposes
// on /metrics.
package observability

import (
	"context"
	"log/slog"
	"os"
	"strings"
)

type loggerKey struct{}

// NewLogger writes text when a person is reading the output and JSON when a log
// aggregator is. Anything the level does not recognise falls back to info
// rather than silencing the service.
func NewLogger(production bool, level string) *slog.Logger {
	options := &slog.HandlerOptions{Level: parseLevel(level)}
	if production {
		return slog.New(slog.NewJSONHandler(os.Stdout, options))
	}
	return slog.New(slog.NewTextHandler(os.Stdout, options))
}

func parseLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// ContextWithLogger returns a copy of ctx carrying logger.
func ContextWithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey{}, logger)
}

// LoggerFrom returns the request-scoped logger carried by ctx. It falls back to
// the default logger so a caller outside a request still logs instead of
// panicking on a nil receiver.
func LoggerFrom(ctx context.Context) *slog.Logger {
	if logger, ok := ctx.Value(loggerKey{}).(*slog.Logger); ok {
		return logger
	}
	return slog.Default()
}
