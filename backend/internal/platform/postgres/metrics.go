package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

const meterName = "github.com/vgoats/goatos/backend/db"

// RegisterPoolMetrics wires pgxpool.Stat() into four async gauges, matching
// the naming in docs/observability/OBSERVABILITY_DESIGN.md section 2.1:
//
//	db.client.connections.usage   - connections currently checked out to callers.
//	db.client.connections.idle    - connections sitting idle in the pool.
//	db.client.connections.max     - configured maximum pool size.
//	db.client.connections.pending - connections currently being constructed
//	                                (the closest pgxpool.Stat() gauge to
//	                                "acquire is waiting on more capacity";
//	                                pgxpool only exposes *cumulative* counters
//	                                for actual acquire-wait events, not a
//	                                live waiter gauge).
//
// Safe to call once per process per pool. Uses the OTel global MeterProvider,
// so it degrades to a no-op when observability.SetupTelemetry was never
// activated (local/dev without a collector).
func RegisterPoolMetrics(pool *pgxpool.Pool) error {
	if pool == nil {
		return fmt.Errorf("postgres: cannot register pool metrics for a nil pool")
	}
	meter := otel.Meter(meterName)

	usage, err := meter.Int64ObservableGauge("db.client.connections.usage",
		metric.WithUnit("{connection}"),
		metric.WithDescription("Connections currently checked out to callers."))
	if err != nil {
		return fmt.Errorf("postgres: create db.client.connections.usage: %w", err)
	}
	idle, err := meter.Int64ObservableGauge("db.client.connections.idle",
		metric.WithUnit("{connection}"),
		metric.WithDescription("Connections sitting idle in the pool."))
	if err != nil {
		return fmt.Errorf("postgres: create db.client.connections.idle: %w", err)
	}
	maxConns, err := meter.Int64ObservableGauge("db.client.connections.max",
		metric.WithUnit("{connection}"),
		metric.WithDescription("Configured maximum pool size."))
	if err != nil {
		return fmt.Errorf("postgres: create db.client.connections.max: %w", err)
	}
	pending, err := meter.Int64ObservableGauge("db.client.connections.pending",
		metric.WithUnit("{connection}"),
		metric.WithDescription("Connections currently being constructed (proxy for acquire pressure)."))
	if err != nil {
		return fmt.Errorf("postgres: create db.client.connections.pending: %w", err)
	}

	_, err = meter.RegisterCallback(func(_ context.Context, o metric.Observer) error {
		stat := pool.Stat()
		o.ObserveInt64(usage, int64(stat.AcquiredConns()))
		o.ObserveInt64(idle, int64(stat.IdleConns()))
		o.ObserveInt64(maxConns, int64(stat.MaxConns()))
		o.ObserveInt64(pending, int64(stat.ConstructingConns()))
		return nil
	}, usage, idle, maxConns, pending)
	if err != nil {
		return fmt.Errorf("postgres: register pool stat callback: %w", err)
	}
	return nil
}
