package middleware

import (
	"log/slog"

	"github.com/airhost/backend/internal/observability/logctx"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// requestIDHeader follows the de-facto standard so any upstream proxy or
// downstream client can read/propagate it without translation.
// requestIDGinKey is the gin-context key used by RequestIDGin (handlers
// that have *gin.Context handy don't need to fish through the stdlib ctx).
const (
	requestIDHeader = "X-Request-ID"
	requestIDGinKey = "requestID"
)

// RequestID returns a middleware that assigns a per-request UUID and threads
// it through (a) the gin context, (b) the request's stdlib context (via the
// shared logctx package, so services and subscribers can read it without
// importing middleware), and (c) the X-Request-ID response header. An
// incoming X-Request-ID is preserved when present and parseable as a UUID —
// useful when an upstream proxy or the calling client already minted one —
// otherwise a fresh v4 UUID is generated.
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

		// Stash on the request's stdlib context via the shared logctx
		// package so downstream services / subscribers can read via
		// logctx.LoggerFrom without depending on the middleware package.
		ctx := logctx.WithRequestID(c.Request.Context(), id)
		ctx = logctx.WithLogger(ctx, logger)
		c.Request = c.Request.WithContext(ctx)

		c.Next()
	}
}

// RequestIDGin reads the per-request UUID directly from a gin.Context. Use
// this in handlers when you have *gin.Context handy; prefer
// logctx.RequestIDFrom for service-layer code that only sees context.Context.
func RequestIDGin(c *gin.Context) string {
	if v, ok := c.Get(requestIDGinKey); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
