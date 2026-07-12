// Package tracecontext provides tiny W3C trace-context propagation helpers
// for carrying a distributed trace across the outbox write -> publish ->
// consume -> obligation boundary, per
// docs/observability/OBSERVABILITY_DESIGN.md section 2.2:
//
//	"propagate trace context through the outbox message (store traceparent
//	 in envelope) so a write and its async publish/consume/obligation land
//	 on one trace."
//
// Scope note: this package implements the shared plumbing (inject/extract on
// a map[string]string carrier, which both Pub/Sub message attributes and a
// JSON headers object satisfy) and is wired into the outbox module itself
// (internal/outbox/app, internal/outbox/adapters/publisher/pubsub,
// internal/domainconsumer/app). It is NOT yet wired into every module's own
// outbox-row INSERT call site (protocol, calendar, vaccination, obligation,
// counts, notification, procurement, ...) - see the TODO in
// internal/outbox/app/service.go for why that was scoped down.
package tracecontext

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

// Inject writes the current span's W3C traceparent/baggage into carrier
// (typically a Pub/Sub attributes map or a headers JSON object decoded to
// map[string]string). No-op when ctx carries no active/valid span.
func Inject(ctx context.Context, carrier map[string]string) {
	if carrier == nil {
		return
	}
	otel.GetTextMapPropagator().Inject(ctx, propagation.MapCarrier(carrier))
}

// Extract rebuilds a context carrying the remote span described by carrier's
// "traceparent" (and "baggage") entries, so the caller can start a child span
// continuing the original trace. Returns ctx unchanged when carrier has no
// valid traceparent entry.
func Extract(ctx context.Context, carrier map[string]string) context.Context {
	if carrier == nil {
		return ctx
	}
	return otel.GetTextMapPropagator().Extract(ctx, propagation.MapCarrier(carrier))
}
