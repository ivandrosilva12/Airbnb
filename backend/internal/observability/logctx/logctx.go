// Package logctx threads a per-request slog.Logger and request_id through
// context.Context. Both the HTTP middleware (which mints the request_id) and
// the services/subscribers it eventually calls can read the same id via
// LoggerFrom(ctx).Info(...) so every log line correlates.
//
// Kept in its own package — rather than in interfaces/http/middleware — so
// services and event subscribers can consume it without taking a transport-
// layer dependency.
package logctx

import (
	"context"
	"log/slog"
)

// Private key type prevents string-key collisions across packages.
type ctxKey int

const (
	ctxKeyLogger ctxKey = iota
	ctxKeyRequestID
)

// WithLogger returns a derived context carrying l. Callers (typically the
// HTTP request_id middleware) build a logger pinned with request_id (and
// optionally user_id, route, etc) and pass it down; downstream code reads
// via LoggerFrom and never has to know the keys exist.
func WithLogger(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, ctxKeyLogger, l)
}

// LoggerFrom returns the request-scoped logger, or slog.Default() when ctx
// carries none (e.g. startup, scheduler jobs, or tests). The fall-back keeps
// callers free of nil checks.
func LoggerFrom(ctx context.Context) *slog.Logger {
	if ctx != nil {
		if l, ok := ctx.Value(ctxKeyLogger).(*slog.Logger); ok && l != nil {
			return l
		}
	}
	return slog.Default()
}

// WithRequestID returns a derived context carrying the per-request id.
// Exposed separately from the logger so call sites that just want to thread
// the id (e.g. into an outbound HTTP header or a trace span) don't need to
// fish it out of the logger's attributes.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxKeyRequestID, id)
}

// RequestIDFrom returns the per-request id, or empty string when ctx carries
// none.
func RequestIDFrom(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if id, ok := ctx.Value(ctxKeyRequestID).(string); ok {
		return id
	}
	return ""
}
