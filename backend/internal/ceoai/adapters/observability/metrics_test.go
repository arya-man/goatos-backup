package observability

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// collectMetrics installs a ManualReader-backed MeterProvider, runs fn against a
// freshly-constructed Metrics, and returns the collected metric set. NewMetrics
// is called AFTER the provider is installed so its eagerly-created instruments
// bind to the real reader (the OTel global delegates regardless, but this keeps
// the test deterministic).
func collectMetrics(t *testing.T, fn func(ctx context.Context, m *Metrics)) metricdata.ResourceMetrics {
	t.Helper()
	reader := metric.NewManualReader()
	mp := metric.NewMeterProvider(metric.WithReader(reader))
	prev := otel.GetMeterProvider()
	otel.SetMeterProvider(mp)
	t.Cleanup(func() { otel.SetMeterProvider(prev) })

	m := NewMetrics()
	fn(context.Background(), m)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	return rm
}

// findMetric returns true if a metric with the given name was emitted.
func findMetric(rm metricdata.ResourceMetrics, name string) bool {
	for _, sm := range rm.ScopeMetrics {
		for _, md := range sm.Metrics {
			if md.Name == name {
				return true
			}
		}
	}
	return false
}

func TestMetrics_AllInstrumentsEmit(t *testing.T) {
	rm := collectMetrics(t, func(ctx context.Context, m *Metrics) {
		m.RecordRequest(ctx, "cube", "ok", 42.0)
		m.RecordToolRows(ctx, "cube", 7)
		m.SQLRejected(ctx, "multi_statement")
		m.VertexFailover(ctx)
		m.ToolboxError(ctx)
		m.RateLimitTrip(ctx)
		m.BudgetRejection(ctx)
		m.CacheLookup(ctx, true)
		m.CacheLookup(ctx, false)
		m.ReviewCorrection(ctx)
		m.InjectionBlocked(ctx)
		m.SetBreakerState(ctx, "vertex", BreakerOpen)
	})

	want := []string{
		"assistant_requests_total",
		"assistant_latency_ms",
		"assistant_tool_rows_returned",
		"assistant_sql_rejected_total",
		"assistant_vertex_failover_total",
		"assistant_toolbox_errors_total",
		"assistant_rate_limit_trips_total",
		"assistant_budget_rejections_total",
		"assistant_cache_lookups_total",
		"assistant_review_corrections_total",
		"assistant_injection_blocked_total",
		"assistant_breaker_state",
	}
	for _, name := range want {
		if !findMetric(rm, name) {
			t.Errorf("metric %q not emitted", name)
		}
	}
}

func TestMetrics_NilReceiverIsNoop(t *testing.T) {
	var m *Metrics // nil
	// Must not panic.
	m.RecordRequest(context.Background(), "cube", "ok", 1)
	m.SetBreakerState(context.Background(), "cube", BreakerClosed)
	m.CacheLookup(context.Background(), true)
}
