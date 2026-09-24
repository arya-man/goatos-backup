package postgres

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
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
	return RegisterNamedPoolMetrics(pool, "main")
}

// RegisterNamedPoolMetrics is RegisterPoolMetrics with a db.client.pool.name
// attribute, so the API's main and auth pools are separable on one dashboard.
// Besides the four gauges it exports pgxpool's cumulative acquire counters:
//
//	db.client.connections.acquire.count        - successful acquires.
//	db.client.connections.acquire.empty_count  - acquires that had to WAIT
//	                                             because the pool was empty.
//	db.client.connections.acquire.canceled     - acquires abandoned (ctx done).
//	db.client.connections.acquire.duration     - total seconds spent acquiring.
//
// rate(duration)/rate(count) is mean acquire wait; a rising empty_count is the
// saturation signal the 2026-09-24 incident lacked.
func RegisterNamedPoolMetrics(pool *pgxpool.Pool, name string) error {
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

	acquireCount, err := meter.Int64ObservableCounter("db.client.connections.acquire.count",
		metric.WithUnit("{acquire}"),
		metric.WithDescription("Cumulative successful connection acquires."))
	if err != nil {
		return fmt.Errorf("postgres: create acquire.count: %w", err)
	}
	emptyAcquire, err := meter.Int64ObservableCounter("db.client.connections.acquire.empty_count",
		metric.WithUnit("{acquire}"),
		metric.WithDescription("Cumulative acquires that waited because the pool was empty."))
	if err != nil {
		return fmt.Errorf("postgres: create acquire.empty_count: %w", err)
	}
	canceledAcquire, err := meter.Int64ObservableCounter("db.client.connections.acquire.canceled",
		metric.WithUnit("{acquire}"),
		metric.WithDescription("Cumulative acquires canceled before a connection was obtained."))
	if err != nil {
		return fmt.Errorf("postgres: create acquire.canceled: %w", err)
	}
	acquireDuration, err := meter.Float64ObservableCounter("db.client.connections.acquire.duration",
		metric.WithUnit("s"),
		metric.WithDescription("Cumulative time spent acquiring connections."))
	if err != nil {
		return fmt.Errorf("postgres: create acquire.duration: %w", err)
	}

	attrs := metric.WithAttributes(attribute.String("db.client.pool.name", name))
	_, err = meter.RegisterCallback(func(_ context.Context, o metric.Observer) error {
		stat := pool.Stat()
		o.ObserveInt64(usage, int64(stat.AcquiredConns()), attrs)
		o.ObserveInt64(idle, int64(stat.IdleConns()), attrs)
		o.ObserveInt64(maxConns, int64(stat.MaxConns()), attrs)
		o.ObserveInt64(pending, int64(stat.ConstructingConns()), attrs)
		o.ObserveInt64(acquireCount, stat.AcquireCount(), attrs)
		o.ObserveInt64(emptyAcquire, stat.EmptyAcquireCount(), attrs)
		o.ObserveInt64(canceledAcquire, stat.CanceledAcquireCount(), attrs)
		o.ObserveFloat64(acquireDuration, stat.AcquireDuration().Seconds(), attrs)
		return nil
	}, usage, idle, maxConns, pending, acquireCount, emptyAcquire, canceledAcquire, acquireDuration)
	if err != nil {
		return fmt.Errorf("postgres: register pool stat callback: %w", err)
	}
	return nil
}

// PoolPressureDelta is the acquire activity between two pgxpool.Stat samples.
type PoolPressureDelta struct {
	Acquires      int64
	EmptyAcquires int64
	Canceled      int64
	WaitTotal     time.Duration
}

// poolPressure computes the delta between two cumulative samples.
func poolPressure(prev, cur poolSample) PoolPressureDelta {
	return PoolPressureDelta{
		Acquires:      cur.acquires - prev.acquires,
		EmptyAcquires: cur.empty - prev.empty,
		Canceled:      cur.canceled - prev.canceled,
		WaitTotal:     cur.wait - prev.wait,
	}
}

type poolSample struct {
	acquires, empty, canceled int64
	wait                      time.Duration
}

func samplePool(pool *pgxpool.Pool) poolSample {
	st := pool.Stat()
	return poolSample{acquires: st.AcquireCount(), empty: st.EmptyAcquireCount(), canceled: st.CanceledAcquireCount(), wait: st.AcquireDuration()}
}

// LogPoolAcquirePressure logs one WARN line per interval in which any acquire
// had to wait on an empty pool, with the interval's mean acquire wait. It is
// silent while the pool keeps up, so it costs one Stat() per interval. Runs
// until ctx is done.
func LogPoolAcquirePressure(ctx context.Context, pool *pgxpool.Pool, name string, log *slog.Logger, interval time.Duration) {
	if pool == nil || log == nil {
		return
	}
	if interval <= 0 {
		interval = 30 * time.Second
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		prev := samplePool(pool)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			cur := samplePool(pool)
			d := poolPressure(prev, cur)
			prev = cur
			if d.EmptyAcquires <= 0 && d.Canceled <= 0 {
				continue
			}
			// pgxpool's AcquireDuration is dominated by the acquires that had
			// to wait on an empty pool, so average over those, not all acquires.
			var mean time.Duration
			if d.EmptyAcquires > 0 {
				mean = d.WaitTotal / time.Duration(d.EmptyAcquires)
			}
			st := pool.Stat()
			log.Warn("postgres_pool_acquire_pressure",
				slog.String("pool", name),
				slog.Int64("acquires", d.Acquires),
				slog.Int64("empty_acquires", d.EmptyAcquires),
				slog.Int64("canceled_acquires", d.Canceled),
				slog.Duration("acquire_wait_total", d.WaitTotal),
				slog.Duration("empty_acquire_wait_mean", mean),
				slog.Int("acquired_conns", int(st.AcquiredConns())),
				slog.Int("max_conns", int(st.MaxConns())),
				slog.Duration("interval", interval),
			)
		}
	}()
}

// RecordPoolBootPingFailure counts a non-fatal boot ping failure of a lazily
// connected pool (db.client.pool.boot_ping_failures{db.client.pool.name}).
func RecordPoolBootPingFailure(ctx context.Context, name string) {
	counter, err := otel.Meter(meterName).Int64Counter("db.client.pool.boot_ping_failures",
		metric.WithUnit("{failure}"),
		metric.WithDescription("Lazily connected pools whose boot ping failed; the pool retries on first use."))
	if err != nil {
		return
	}
	counter.Add(ctx, 1, metric.WithAttributes(attribute.String("db.client.pool.name", name)))
}
