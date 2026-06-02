package middleware

import (
	"github.com/airhost/backend/internal/observability/logctx"
	"github.com/airhost/backend/internal/observability/tracing"
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
)

// Tracing returns the otelgin root-span middleware for the given
// service name (S52). It creates a per-request span with the
// standard HTTP attributes (method, route, status_code) and
// consumes/propagates the traceparent header so a caller's trace
// context becomes this span's parent.
//
// The middleware does NOT also enrich the logger — that's
// TracingLoggerEnrich's job. They're split because otelgin internally
// calls c.Next(), so a wrapper that runs work after otelgin would
// run AFTER every downstream handler. Registering them as siblings
// in the right order is the safe pattern: Tracing → TracingLoggerEnrich
// → rest of chain.
//
// MUST be registered BEFORE TracingLoggerEnrich, which MUST be
// registered AFTER RequestID (so the request-id logger exists in the
// context when the enrich step runs).
func Tracing(service string) gin.HandlerFunc {
	return otelgin.Middleware(service)
}

// TracingLoggerEnrich pulls the trace/span IDs from the context
// (populated by Tracing) and pins them on the request-scoped logger
// alongside request_id. Every log line emitted from a handler or
// service via LoggerFrom(ctx) then carries both correlation ids,
// so operators can pivot from a log to its trace and vice versa
// without an external join.
//
// A no-op when the global tracer is the noop tracer — SpanContextFrom
// returns empty strings, and the middleware just calls c.Next() with
// no allocation.
func TracingLoggerEnrich() gin.HandlerFunc {
	return func(c *gin.Context) {
		traceID, spanID := tracing.SpanContextFrom(c.Request.Context())
		if traceID == "" {
			c.Next()
			return
		}
		ctx := c.Request.Context()
		logger := logctx.LoggerFrom(ctx).With("trace_id", traceID, "span_id", spanID)
		ctx = logctx.WithLogger(ctx, logger)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}
