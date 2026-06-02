package middleware

import (
	"context"
	"log/slog"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// Header / context keys for the per-request identifier. The header name
// follows the de-facto standard so any upstream proxy or downstream client
// can read or propagate it without translation. The context key is private
// (custom type) so callers must go through the helpers below — that prevents
// accidental string collisions across packages.
const (
	requestIDHeader = "X-Request-ID"
	requestIDGinKey = "requestID"
)

type ctxKey int

const (
	ctxKeyRequestID ctxKey = iota
	ctxKeyLogger
)

// RequestID returns a middleware that assigns a per-request UUID and threads
// it through (a) the gin context, (b) the request's stdlib context, and (c)
// the X-Request-ID response header. An incoming X-Request-ID is preserved
// when present and parseable as a UUID — useful when an upstream proxy or
// the calling client already minted one — otherwise a fresh v4 UUID is
// generated.
//
// A request-scoped slog.Logger is also stashed on the context so handlers
// and services can call LoggerFrom(ctx).Info(...) and have every line tagged
// with the same request_id. Old slog.Info(...) calls keep working unchanged;
// they just won't carry the request_id until they migrate.
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(requestIDHeader)
		if _, err := uuid.Parse(id); err != nil {
			id = uuid.New().String()
		}
		c.Set(requestIDGinKey, id)
		c.Writer.Header().Set(requestIDHeader, id)

		// Build a request-scoped logger that pins request_id on every line.
		logger := slog.Default().With("request_id", id)

		// Stash on the request's stdlib context so handlers reaching into
		// c.Request.Context() — and services they call — see both values.
		ctx := context.WithValue(c.Request.Context(), ctxKeyRequestID, id)
		ctx = context.WithValue(ctx, ctxKeyLogger, logger)
		c.Request = c.Request.WithContext(ctx)

		c.Next()
	}
}

// RequestIDFrom returns the per-request UUID assigned by RequestID, or empty
// string if the middleware did not run on this request (e.g. operational
// endpoints mounted outside the api group).
func RequestIDFrom(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKeyRequestID).(string); ok {
		return v
	}
	return ""
}

// LoggerFrom returns the request-scoped logger with request_id pinned, or
// slog.Default() if RequestID did not run.
func LoggerFrom(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(ctxKeyLogger).(*slog.Logger); ok {
		return l
	}
	return slog.Default()
}

// RequestIDGin reads the per-request UUID directly from a gin.Context. Use
// this in handlers when you have *gin.Context handy; prefer RequestIDFrom for
// service-layer code that only sees context.Context.
func RequestIDGin(c *gin.Context) string {
	if v, ok := c.Get(requestIDGinKey); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
