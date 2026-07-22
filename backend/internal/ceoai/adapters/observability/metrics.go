// Package observability holds the leadership-assistant (CEO AI) telemetry
// adapter: the OpenTelemetry metric instruments the orchestrator emits at each
// pipeline boundary, plus the internal step-trace store the admin-only debug
// surface reads.
//
// Metrics are created once, eagerly, against the OTel global Meter
// ("github.com/vgoats/goatos/backend/ceoai"). This is safe even though
// observability.SetupTelemetry usually runs later in main(): the OTel API's
// global package returns delegating instruments that start reporting to the
// real MeterProvider the moment one is installed. When telemetry is never
// enabled (local/dev), every Add/Record call is a no-op.
//
// The metric set matches docs/ceo-ai/ceo-chatbot-purpose-and-build-plan.md and
// docs/observability/CEO_AI_OBSERVABILITY.md:
//
//	assistant_requests_total{tool,status}   counter
//	assistant_latency_ms{tool}              histogram (ms)
//	assistant_tool_rows_returned{tool}      histogram (rows)
//	assistant_sql_rejected_total{reason}    counter
//	assistant_vertex_failover_total         counter
//	assistant_toolbox_errors_total          counter
//	assistant_rate_limit_trips_total        counter
//	assistant_budget_rejections_total       counter
//	assistant_cache_lookups_total{result}   counter (hit|miss -> cache_hit_ratio)
//	assistant_review_corrections_total      counter
//	assistant_injection_blocked_total       counter
//	assistant_breaker_state{dependency}     gauge (0 closed | 1 half_open | 2 open)
//
// Labels are bounded, low-cardinality enums only (never tenant_id, actor,
// request_id, question text, or any free-form value) so metric series stay
// finite. Per-request correlation lives in the step trace / audit row, not in
// metric labels.
package observability

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const meterName = "github.com/vgoats/goatos/backend/ceoai"

// BreakerState is the low-cardinality circuit-breaker gauge value for a
// downstream dependency (Vertex, Cube, Toolbox, Postgres).
type BreakerState int64

const (
	BreakerClosed   BreakerState = 0 // healthy, calls flow
	BreakerHalfOpen BreakerState = 1 // probing after a cooldown
	BreakerOpen     BreakerState = 2 // shedding, fast-failing to the next tier
)

// Metrics is the assistant telemetry facade. Construct once at bootstrap with
// NewMetrics and share the pointer; all methods are goroutine-safe (OTel
// instruments are). A nil *Metrics is a valid no-op so callers that have not
// wired telemetry (tests, degraded boot) never nil-panic.
type Metrics struct {
	requests         metric.Int64Counter
	latencyMS        metric.Float64Histogram
	toolRows         metric.Int64Histogram
	sqlRejected      metric.Int64Counter
	vertexFailover   metric.Int64Counter
	toolboxErrors    metric.Int64Counter
	rateLimitTrips   metric.Int64Counter
	budgetRejections metric.Int64Counter
	cacheLookups     metric.Int64Counter
	reviewCorrect    metric.Int64Counter
	injectionBlocked metric.Int64Counter
	breakerState     metric.Int64Gauge
}

// NewMetrics eagerly creates every instrument against the global Meter. An
// instrument that fails to construct (only possible for a malformed static
// name — a programming error caught in review, never a runtime condition) is
// routed through otel.Handle and left nil; its Record/Add helper degrades to a
// no-op rather than crashing a leadership request over a metric.
func NewMetrics() *Metrics {
	m := otel.Meter(meterName)
	return &Metrics{
		requests:         mustCounter(m, "assistant_requests_total", "1", "Leadership assistant requests by resolved tool and terminal status."),
		latencyMS:        mustHistogram(m, "assistant_latency_ms", "ms", "End-to-end assistant answer latency in milliseconds by tool."),
		toolRows:         mustIntHistogram(m, "assistant_tool_rows_returned", "1", "Rows returned by the read tool grounding an answer, by tool."),
		sqlRejected:      mustCounter(m, "assistant_sql_rejected_total", "1", "SQL-fallback statements rejected by the read-only guard, by reason."),
		vertexFailover:   mustCounter(m, "assistant_vertex_failover_total", "1", "Falls back from the Vertex planner to the deterministic keyword planner."),
		toolboxErrors:    mustCounter(m, "assistant_toolbox_errors_total", "1", "Errors calling the MCP Toolbox tool tier."),
		rateLimitTrips:   mustCounter(m, "assistant_rate_limit_trips_total", "1", "Requests rejected by the per-tenant/user rate limiter."),
		budgetRejections: mustCounter(m, "assistant_budget_rejections_total", "1", "Requests rejected by the per-tenant/user token/cost budget cap."),
		cacheLookups:     mustCounter(m, "assistant_cache_lookups_total", "1", "Response-cache lookups labelled result=hit|miss (source for cache_hit_ratio)."),
		reviewCorrect:    mustCounter(m, "assistant_review_corrections_total", "1", "Answers the runtime review downgraded or corrected before returning."),
		injectionBlocked: mustCounter(m, "assistant_injection_blocked_total", "1", "Prompt-injection / scope-override attempts blocked by the guard."),
		breakerState:     mustGauge(m, "assistant_breaker_state", "1", "Circuit-breaker state per dependency: 0 closed, 1 half_open, 2 open."),
	}
}

// RecordRequest counts one terminal assistant request and records its latency.
// tool is the resolved tool/tier label (e.g. "cube", "mesha_api", "none");
// status is the terminal outcome (ok|rejected|error|over_budget|degraded).
func (m *Metrics) RecordRequest(ctx context.Context, tool, status string, latencyMS float64) {
	if m == nil {
		return
	}
	attrs := []attribute.KeyValue{attribute.String("tool", norm(tool)), attribute.String("status", norm(status))}
	add(ctx, m.requests, 1, attrs...)
	record(ctx, m.latencyMS, latencyMS, attribute.String("tool", norm(tool)))
}

// RecordToolRows records how many rows a read tool returned to ground an answer.
func (m *Metrics) RecordToolRows(ctx context.Context, tool string, rows int) {
	if m == nil {
		return
	}
	recordInt(ctx, m.toolRows, int64(rows), attribute.String("tool", norm(tool)))
}

// SQLRejected counts one SQL-fallback rejection by the guard, tagged by reason.
func (m *Metrics) SQLRejected(ctx context.Context, reason string) {
	if m == nil {
		return
	}
	add(ctx, m.sqlRejected, 1, attribute.String("reason", norm(reason)))
}

// VertexFailover counts one planner failover to the keyword planner.
func (m *Metrics) VertexFailover(ctx context.Context) {
	if m == nil {
		return
	}
	add(ctx, m.vertexFailover, 1)
}

// ToolboxError counts one MCP Toolbox tier error.
func (m *Metrics) ToolboxError(ctx context.Context) {
	if m == nil {
		return
	}
	add(ctx, m.toolboxErrors, 1)
}

// RateLimitTrip counts one rate-limiter rejection.
func (m *Metrics) RateLimitTrip(ctx context.Context) {
	if m == nil {
		return
	}
	add(ctx, m.rateLimitTrips, 1)
}

// BudgetRejection counts one budget-cap rejection.
func (m *Metrics) BudgetRejection(ctx context.Context) {
	if m == nil {
		return
	}
	add(ctx, m.budgetRejections, 1)
}

// CacheLookup counts one response-cache lookup; hit=true is a cache hit. The
// hit/miss split is the numerator/denominator for the cache_hit_ratio SLI.
func (m *Metrics) CacheLookup(ctx context.Context, hit bool) {
	if m == nil {
		return
	}
	result := "miss"
	if hit {
		result = "hit"
	}
	add(ctx, m.cacheLookups, 1, attribute.String("result", result))
}

// ReviewCorrection counts one answer the runtime review downgraded/corrected.
func (m *Metrics) ReviewCorrection(ctx context.Context) {
	if m == nil {
		return
	}
	add(ctx, m.reviewCorrect, 1)
}

// InjectionBlocked counts one blocked prompt-injection / scope-override attempt.
func (m *Metrics) InjectionBlocked(ctx context.Context) {
	if m == nil {
		return
	}
	add(ctx, m.injectionBlocked, 1)
}

// SetBreakerState records the current circuit-breaker state for a dependency
// (e.g. "vertex", "cube", "toolbox", "postgres").
func (m *Metrics) SetBreakerState(ctx context.Context, dependency string, state BreakerState) {
	if m == nil || m.breakerState == nil {
		return
	}
	m.breakerState.Record(ctx, int64(state), metric.WithAttributes(attribute.String("dependency", norm(dependency))))
}

// --- instrument construction + safe emit helpers (mirror platform/kmetrics) ---

func mustCounter(m metric.Meter, name, unit, desc string) metric.Int64Counter {
	inst, err := m.Int64Counter(name, metric.WithUnit(unit), metric.WithDescription(desc))
	if err != nil {
		otel.Handle(err)
		return nil
	}
	return inst
}

func mustHistogram(m metric.Meter, name, unit, desc string) metric.Float64Histogram {
	inst, err := m.Float64Histogram(name, metric.WithUnit(unit), metric.WithDescription(desc))
	if err != nil {
		otel.Handle(err)
		return nil
	}
	return inst
}

func mustIntHistogram(m metric.Meter, name, unit, desc string) metric.Int64Histogram {
	inst, err := m.Int64Histogram(name, metric.WithUnit(unit), metric.WithDescription(desc))
	if err != nil {
		otel.Handle(err)
		return nil
	}
	return inst
}

func mustGauge(m metric.Meter, name, unit, desc string) metric.Int64Gauge {
	inst, err := m.Int64Gauge(name, metric.WithUnit(unit), metric.WithDescription(desc))
	if err != nil {
		otel.Handle(err)
		return nil
	}
	return inst
}

func add(ctx context.Context, c metric.Int64Counter, incr int64, attrs ...attribute.KeyValue) {
	if c == nil {
		return
	}
	c.Add(ctx, incr, metric.WithAttributes(attrs...))
}

func record(ctx context.Context, h metric.Float64Histogram, value float64, attrs ...attribute.KeyValue) {
	if h == nil {
		return
	}
	h.Record(ctx, value, metric.WithAttributes(attrs...))
}

func recordInt(ctx context.Context, h metric.Int64Histogram, value int64, attrs ...attribute.KeyValue) {
	if h == nil {
		return
	}
	h.Record(ctx, value, metric.WithAttributes(attrs...))
}

// norm collapses an empty label to a stable "unknown" so a metric series is
// never keyed on the empty string.
func norm(v string) string {
	if v == "" {
		return "unknown"
	}
	return v
}
