package observability

import (
	"context"
	"log/slog"
	"os"
	"strings"

	"github.com/WkT010/nexa-exchange/internal/config"
)

// Structured logging setup for the exchange.
//
// All services emit logs through the standard library slog facade so the
// whole fleet shares one output contract: levelled records, JSON in
// production deployments (machine-parseable by log aggregators) and
// human-readable text in development. Configuration comes from two
// environment variables (surfaced through config.Config):
//
//	LOG_FORMAT  json | text   (default: json outside development, text in dev)
//	LOG_LEVEL   debug | info | warn | error   (default: info)
//
// Output goes to stdout; containers and process supervisors already capture
// it, and writing to stderr would interleave oddly with gin's own writer.

type ctxKey int

const requestIDKey ctxKey = iota

// RequestIDAttr is the structured attribute key used to correlate every log
// line of a single HTTP/WebSocket request.
const RequestIDAttr = "request_id"

// Setup configures the process-wide default slog logger from cfg. It must be
// called once, as early as possible in each main() (after config.Load and
// before any component starts logging).
func Setup(cfg *config.Config) {
	level := parseLevel(cfg.LogLevel)
	opts := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	if useTextFormat(cfg) {
		handler = slog.NewTextHandler(os.Stdout, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	}
	slog.SetDefault(slog.New(handler))
}

// useTextFormat resolves the effective output format: an explicit text
// request wins, everything else (including unknown values) renders as JSON
// in non-development environments and as text during development.
func useTextFormat(cfg *config.Config) bool {
	switch strings.ToLower(strings.TrimSpace(cfg.LogFormat)) {
	case "text":
		return true
	case "json":
		return false
	default:
		return cfg.DevMode
	}
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
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

// ContextWithRequestID returns a context carrying the supplied request id so
// downstream logging can correlate all records of one request.
func ContextWithRequestID(ctx context.Context, requestID string) context.Context {
	if requestID == "" {
		return ctx
	}
	return context.WithValue(ctx, requestIDKey, requestID)
}

// RequestIDFromContext extracts the request id stored by
// ContextWithRequestID, or "" when absent.
func RequestIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if rid, ok := ctx.Value(requestIDKey).(string); ok {
		return rid
	}
	return ""
}

// WithRequestID returns the default logger augmented with the request_id
// attribute found in ctx (no-op attribute when the context carries none).
// Use it inside handlers that already hold a request-scoped context:
//
//	observability.WithRequestID(ctx).Error("order submit failed", "user_id", uid, "err", err)
func WithRequestID(ctx context.Context) *slog.Logger {
	if rid := RequestIDFromContext(ctx); rid != "" {
		return slog.Default().With(RequestIDAttr, rid)
	}
	return slog.Default()
}
