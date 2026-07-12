package tracecontext

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// withRealPropagator installs a real W3C TraceContext+Baggage propagator for
// the duration of one test and restores whatever propagator was active
// before. Package tests must not rely on the process default: unlike the
// global TracerProvider/MeterProvider (which bind lazily-created instruments
// to the first real provider installed and ignore later swaps), the global
// TextMapPropagator is a plain value fetched fresh on every
// otel.GetTextMapPropagator() call, so it is safe to set/restore per test.
// Without this, Inject/Extract are no-ops against the default no-op
// propagator regardless of whether ctx carries a valid span.
func withRealPropagator(t *testing.T) {
	t.Helper()
	prev := otel.GetTextMapPropagator()
	t.Cleanup(func() { otel.SetTextMapPropagator(prev) })
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
}

// injectableContext returns a context carrying a valid, recording span so
// Inject has something real to propagate. It starts a span against an
// explicit SDK TracerProvider (not the global default, which is a no-op that
// never produces a valid SpanContext) so tests don't need to touch the
// process-wide otel.SetTracerProvider.
func injectableContext(t *testing.T) context.Context {
	t.Helper()
	tp := sdktrace.NewTracerProvider()
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	ctx, span := tp.Tracer("tracecontext_test").Start(context.Background(), "test-span")
	t.Cleanup(func() { span.End() })
	return ctx
}

func TestInjectExtractRoundTripsTraceparent(t *testing.T) {
	withRealPropagator(t)
	ctx := injectableContext(t)

	carrier := map[string]string{}
	Inject(ctx, carrier)

	traceparent, ok := carrier["traceparent"]
	if !ok || traceparent == "" {
		t.Fatalf("Inject did not write a traceparent entry: %#v", carrier)
	}

	extracted := Extract(context.Background(), carrier)

	wantTraceID := traceIDFromContext(ctx)
	gotTraceID := traceIDFromContext(extracted)
	if wantTraceID == "" || gotTraceID == "" {
		t.Fatalf("expected non-empty trace IDs, got original=%q extracted=%q", wantTraceID, gotTraceID)
	}
	if wantTraceID != gotTraceID {
		t.Fatalf("round-tripped trace ID = %q, want original %q", gotTraceID, wantTraceID)
	}
}

func TestInjectNilCarrierIsSafe(t *testing.T) {
	withRealPropagator(t)
	ctx := injectableContext(t)
	// Must not panic.
	Inject(ctx, nil)
}

func TestInjectNoActiveSpanIsNoop(t *testing.T) {
	withRealPropagator(t)
	carrier := map[string]string{}
	Inject(context.Background(), carrier)
	if len(carrier) != 0 {
		t.Fatalf("expected no carrier entries without an active span, got: %#v", carrier)
	}
}

func TestExtractNilCarrierReturnsSameContext(t *testing.T) {
	ctx := context.Background()
	got := Extract(ctx, nil)
	if got != ctx {
		t.Fatal("Extract(ctx, nil) should return ctx unchanged")
	}
}

func TestExtractEmptyCarrierIsSafe(t *testing.T) {
	withRealPropagator(t)
	ctx := context.Background()
	got := Extract(ctx, map[string]string{})
	if traceIDFromContext(got) != "" {
		t.Fatalf("expected no remote span from an empty carrier, got trace id %q", traceIDFromContext(got))
	}
}

func TestExtractMalformedTraceparentIsSafe(t *testing.T) {
	withRealPropagator(t)
	ctx := context.Background()
	carrier := map[string]string{"traceparent": "not-a-valid-traceparent"}
	// Must not panic; a malformed entry simply fails to produce a valid
	// remote span context.
	got := Extract(ctx, carrier)
	if traceIDFromContext(got) != "" {
		t.Fatalf("expected malformed traceparent to be ignored, got trace id %q", traceIDFromContext(got))
	}
}

// traceIDFromContext returns the hex trace ID of the span embedded in ctx, or
// "" if ctx carries no valid span context.
func traceIDFromContext(ctx context.Context) string {
	sc := oteltrace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return ""
	}
	return sc.TraceID().String()
}
