// Package observability provides structured logging, metrics, and tracing setup.
package observability

import (
	"context"
	"log/slog"
	"os"
)

// Logger returns a configured JSON-structured slog.Logger.
func NewLogger(level, environment string) *slog.Logger {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{
		Level: lvl,
	}

	var handler slog.Handler
	if environment == "development" {
		handler = slog.NewTextHandler(os.Stdout, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	}
	return slog.New(handler)
}

type ctxKey string

const loggerKey ctxKey = "logger"
const requestIDKey ctxKey = "request_id"

// WithLogger puts a logger into the context.
func WithLogger(ctx context.Context, log *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey, log)
}

// Logger retrieves the logger from context, falling back to the default.
func Logger(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(loggerKey).(*slog.Logger); ok {
		return l
	}
	return slog.Default()
}

// WithRequestID attaches a request_id to the context and logger.
func WithRequestID(ctx context.Context, rid string) context.Context {
	ctx = context.WithValue(ctx, requestIDKey, rid)
	log := Logger(ctx).With("request_id", rid)
	return WithLogger(ctx, log)
}

// RequestID retrieves the request_id from context.
func RequestID(ctx context.Context) string {
	if v, ok := ctx.Value(requestIDKey).(string); ok {
		return v
	}
	return ""
}
