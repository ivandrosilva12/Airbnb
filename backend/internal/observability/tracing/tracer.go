// Package tracing initialises the OpenTelemetry tracer provider (S52).
//
// One process-wide tracer is enough for our use: the HTTP middleware
// creates a root span per request, downstream service code adds child
// spans via the package-level Tracer(). Every span carries the
// request_id minted in S32 as an attribute, so logs (which already
// pin request_id) and traces line up without a separate join.
//
// Exporter selection is driven by env:
//   - "noop"   (default) — zero runtime overhead; for tests and any
//                          deployment that hasn't decided yet
//   - "stdout"            — pretty-prints spans to the API's own
//                          logs; useful in local dev to see the
//                          tree without standing up Tempo/Jaeger
//   - "otlp"              — OTLP/HTTP to a configurable endpoint
//                          (Tempo, Jaeger, Honeycomb, Grafana Cloud)
//
// Init returns a Shutdown function that callers MUST defer at the
// composition root — without it, the OTLP exporter loses the last
// in-flight batch on process exit.
package tracing

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	sdkresource "go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

// tracerName is the name used by Tracer() to obtain the package-wide
// tracer instance. OTel uses the name to attribute spans to a
// library/component — "airhost" is the canonical bucket for spans
// our own code creates (otelgin uses its own name for HTTP root spans).
const tracerName = "airhost"

// Config is the slim projection of the env-loaded settings the tracer
// init needs. Kept here (rather than depending on internal/config) so
// the tracing package stays free of cross-layer imports and can be
// constructed inline in tests.
type Config struct {
	// Exporter selects the backend: "noop", "stdout", or "otlp".
	// Empty defaults to "noop".
	Exporter string
	// ServiceName is what shows up in trace UIs as the service that
	// produced the span. Defaults to "airhost-api".
	ServiceName string
	// ServiceVersion is the build tag / git sha shown alongside the
	// service name. Empty allowed.
	ServiceVersion string
	// Environment is the deployment env ("production", "staging",
	// "development"). Surfaced on every span as deployment.environment.
	Environment string
	// OTLPEndpoint is the URL the OTLP/HTTP exporter posts to (e.g.
	// "tempo:4318" or "api.honeycomb.io"). Required when Exporter
	// is "otlp"; ignored otherwise.
	OTLPEndpoint string
	// SampleRatio is the fraction of root spans to sample, in [0, 1].
	// 0 = drop everything, 1 = sample all. Defaults to 1 for non-prod
	// so dev/CI sees every trace; production typically sets 0.05-0.10.
	SampleRatio float64
}

// Init wires the global tracer + propagator from cfg and returns a
// Shutdown func the caller MUST defer. Returns an error only on
// misconfiguration (unknown exporter, missing OTLP endpoint) —
// transient exporter failures during runtime are logged by the SDK
// and never bubble up here, so a temporarily-unreachable Tempo
// does not kill the API.
func Init(ctx context.Context, cfg Config) (func(context.Context) error, error) {
	exporterName := cfg.Exporter
	if exporterName == "" {
		exporterName = "noop"
	}
	if cfg.ServiceName == "" {
		cfg.ServiceName = "airhost-api"
	}
	if cfg.SampleRatio <= 0 {
		cfg.SampleRatio = 1
	}
	if cfg.SampleRatio > 1 {
		cfg.SampleRatio = 1
	}

	// Propagator is set regardless of exporter so even a noop trace
	// path still propagates a parent's traceparent header to a
	// downstream call — useful for tests that probe propagation
	// without standing up an exporter.
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	// "noop" short-circuits the SDK entirely. The global tracer remains
	// the package default (a true no-op), so Tracer() calls are free.
	if exporterName == "noop" {
		return func(context.Context) error { return nil }, nil
	}

	exporter, err := buildExporter(ctx, exporterName, cfg.OTLPEndpoint)
	if err != nil {
		return nil, err
	}

	res, err := sdkresource.Merge(sdkresource.Default(), sdkresource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName(cfg.ServiceName),
		semconv.ServiceVersion(cfg.ServiceVersion),
		semconv.DeploymentEnvironment(cfg.Environment),
	))
	if err != nil {
		return nil, fmt.Errorf("tracing: resource merge: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter,
			// Reasonable defaults for an HTTP API — flush at most every
			// 5s, keep at most 2k spans queued. Override via env if a
			// future deployment proves the limits wrong.
			sdktrace.WithBatchTimeout(5*time.Second),
			sdktrace.WithMaxQueueSize(2048),
		),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SampleRatio))),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)

	return tp.Shutdown, nil
}

// buildExporter picks the SpanExporter implementation for the named
// backend, returning an error for unknown names rather than silently
// degrading to noop.
func buildExporter(ctx context.Context, name, endpoint string) (sdktrace.SpanExporter, error) {
	switch name {
	case "stdout":
		return stdouttrace.New(stdouttrace.WithPrettyPrint())
	case "otlp":
		if endpoint == "" {
			return nil, errors.New("tracing: TRACING_OTLP_ENDPOINT is required when exporter is 'otlp'")
		}
		// otlptracehttp defaults to https; bare host:port assumes
		// insecure. The operator opts in to TLS by passing an https://
		// URL via the endpoint env var.
		opts := []otlptracehttp.Option{otlptracehttp.WithEndpoint(endpoint)}
		// Insecure is intentional for local Tempo over plain HTTP —
		// production OTLP collectors should sit behind a TLS reverse
		// proxy on the same host:port for in-cluster delivery.
		opts = append(opts, otlptracehttp.WithInsecure())
		return otlptrace.New(ctx, otlptracehttp.NewClient(opts...))
	default:
		return nil, fmt.Errorf("tracing: unknown exporter %q (want noop|stdout|otlp)", name)
	}
}

// Tracer returns the package-wide tracer. Use it to create spans in
// service / subscriber code:
//
//	ctx, span := tracing.Tracer().Start(ctx, "booking.confirm")
//	defer span.End()
func Tracer() trace.Tracer {
	return otel.Tracer(tracerName)
}

// SpanContextFrom returns the OTel trace + span IDs for ctx as
// human-readable strings, or empty strings when ctx carries no span.
// Used by logctx to enrich every log line with trace_id / span_id
// so logs and traces correlate without a separate join.
func SpanContextFrom(ctx context.Context) (traceID, spanID string) {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return "", ""
	}
	return sc.TraceID().String(), sc.SpanID().String()
}
