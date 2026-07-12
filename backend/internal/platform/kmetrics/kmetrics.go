// Package kmetrics provides typed OpenTelemetry instruments for the Goat OS
// operational kernel stages (outbox, domain-event consumer, obligation
// sweeper, notification dispatcher, Cloud Tasks enqueue), per
// docs/observability/OBSERVABILITY_DESIGN.md section 2.1.
//
// Instruments are created once, eagerly, against the OTel global Meter
// ("goatos/kernel"). This is safe even though observability.SetupTelemetry
// usually runs later, in main(): the OTel API's global package returns
// delegating instruments that automatically start reporting to the real
// MeterProvider once one is installed via otel.SetMeterProvider - callers
// never need to re-create instruments after telemetry setup. When telemetry
// is never enabled (local/dev), every Record/Add call below is a no-op.
//
// All duration instruments record seconds (float64) per OTel semantic
// convention for histograms measuring time.
package kmetrics

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const meterName = "github.com/vgoats/goatos/backend/kernel"

var meter = otel.Meter(meterName)

// newCounter, newHistogram, and newGauge log-and-degrade to a nil instrument
// on creation error (only possible for a malformed static instrument name,
// which is a programming error caught in review/tests, never a runtime
// condition) rather than panicking a production process over a metric.
func newCounter(name, unit, desc string) metric.Int64Counter {
	inst, err := meter.Int64Counter(name, metric.WithUnit(unit), metric.WithDescription(desc))
	if err != nil {
		otel.Handle(err)
		return nil
	}
	return inst
}

func newHistogram(name, unit, desc string) metric.Float64Histogram {
	inst, err := meter.Float64Histogram(name, metric.WithUnit(unit), metric.WithDescription(desc))
	if err != nil {
		otel.Handle(err)
		return nil
	}
	return inst
}

func newGauge(name, unit, desc string) metric.Int64Gauge {
	inst, err := meter.Int64Gauge(name, metric.WithUnit(unit), metric.WithDescription(desc))
	if err != nil {
		otel.Handle(err)
		return nil
	}
	return inst
}

func addCounter(ctx context.Context, c metric.Int64Counter, incr int64, attrs ...attribute.KeyValue) {
	if c == nil {
		return
	}
	c.Add(ctx, incr, metric.WithAttributes(attrs...))
}

func recordHistogram(ctx context.Context, h metric.Float64Histogram, value float64, attrs ...attribute.KeyValue) {
	if h == nil {
		return
	}
	h.Record(ctx, value, metric.WithAttributes(attrs...))
}

func recordGauge(ctx context.Context, g metric.Int64Gauge, value int64, attrs ...attribute.KeyValue) {
	if g == nil {
		return
	}
	g.Record(ctx, value, metric.WithAttributes(attrs...))
}

// LogInstrumentationError is a small seam tests/mains can use to observe
// instrument-creation failures without pulling in otel.Handle's default
// (stderr-only) behavior. Currently unused in production code paths but kept
// so a future caller can route otel.SetErrorHandler through slog instead of
// duplicating the wiring - see observability.SetupTelemetry, which already
// installs a slog-backed otel.ErrorHandler.
func LogInstrumentationError(log *slog.Logger, err error) {
	if log == nil || err == nil {
		return
	}
	log.Warn("kmetrics_instrument_error", slog.String("error", err.Error()))
}
