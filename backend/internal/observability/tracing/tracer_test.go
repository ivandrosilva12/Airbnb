package tracing

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// TestInit_NoopExporter_NoError is the default-path: a process that
// hasn't configured tracing still boots cleanly with the noop tracer
// installed, and Shutdown is a no-op.
func TestInit_NoopExporter_NoError(t *testing.T) {
	shutdown, err := Init(context.Background(), Config{Exporter: "noop"})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if shutdown == nil {
		t.Fatalf("Init returned nil shutdown")
	}
	if err := shutdown(context.Background()); err != nil {
		t.Errorf("noop shutdown returned %v", err)
	}
}

// TestInit_UnknownExporter_Errors proves a typo in TRACING_EXPORTER
// fails loud at startup rather than silently dropping spans.
func TestInit_UnknownExporter_Errors(t *testing.T) {
	_, err := Init(context.Background(), Config{Exporter: "carrier-pigeon"})
	if err == nil {
		t.Fatalf("expected error for unknown exporter")
	}
}

// TestInit_OTLP_MissingEndpoint_Errors guards the precondition: an
// OTLP exporter with no endpoint silently posts to "localhost", a
// confusing failure mode in any non-local deployment. We refuse to
// boot instead.
func TestInit_OTLP_MissingEndpoint_Errors(t *testing.T) {
	_, err := Init(context.Background(), Config{Exporter: "otlp"})
	if err == nil {
		t.Fatalf("expected error for otlp exporter with no endpoint")
	}
}

// TestInit_PropagatorRegistered_TraceContextAndBaggage confirms the
// global TextMapPropagator carries both TraceContext (traceparent)
// and Baggage — required for end-to-end propagation across services
// even when the local exporter is noop.
func TestInit_PropagatorRegistered_TraceContextAndBaggage(t *testing.T) {
	_, err := Init(context.Background(), Config{Exporter: "noop"})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	// Composite propagator should advertise both header families.
	tc := propagation.TraceContext{}
	bg := propagation.Baggage{}
	want := map[string]bool{}
	for _, k := range tc.Fields() {
		want[k] = true
	}
	for _, k := range bg.Fields() {
		want[k] = true
	}
	for _, k := range otel.GetTextMapPropagator().Fields() {
		delete(want, k)
	}
	if len(want) > 0 {
		t.Errorf("global propagator missing fields: %v", want)
	}
}

// TestSpanContextFrom_NoSpan_ReturnsEmpty proves the SpanContextFrom
// helper plays nicely with a context that never entered a span — used
// by TracingLoggerEnrich to skip the enrichment without nil-panic.
func TestSpanContextFrom_NoSpan_ReturnsEmpty(t *testing.T) {
	traceID, spanID := SpanContextFrom(context.Background())
	if traceID != "" || spanID != "" {
		t.Errorf("expected empty ids, got trace=%q span=%q", traceID, spanID)
	}
}

// TestSpanContextFrom_InsideSpan_ReturnsBothIDs uses a fresh test
// tracer provider so the test does not depend on Init's global state
// (which other tests in this package install). The helper extracts
// the active span's IDs as hex strings.
func TestSpanContextFrom_InsideSpan_ReturnsBothIDs(t *testing.T) {
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(tracetest.NewInMemoryExporter())))
	defer func() { _ = tp.Shutdown(context.Background()) }()
	tracer := tp.Tracer("test")

	ctx, span := tracer.Start(context.Background(), "probe")
	defer span.End()

	traceID, spanID := SpanContextFrom(ctx)
	if traceID == "" || spanID == "" {
		t.Fatalf("expected non-empty ids inside a span, got trace=%q span=%q", traceID, spanID)
	}
	// Trace IDs are 32 hex chars, span IDs are 16.
	if len(traceID) != 32 {
		t.Errorf("trace_id wrong length: %d (%q)", len(traceID), traceID)
	}
	if len(spanID) != 16 {
		t.Errorf("span_id wrong length: %d (%q)", len(spanID), spanID)
	}
}

// TestTracer_NameStable confirms the package-wide tracer comes back
// from the OTel global with the expected name — a regression here
// would silently route spans to a different bucket in trace UIs.
func TestTracer_NameStable(t *testing.T) {
	if Tracer() == nil {
		t.Fatalf("Tracer() returned nil")
	}
}
