package kmetrics

import (
	"context"
	"os"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// testReader backs the single SDK MeterProvider installed once for this test
// binary by TestMain below.
//
// kmetrics' instruments (sweeperBatchDuration, outboxQueueDepth, ...) are
// created once at package-init time against the global otel.Meter delegate.
// OTel Go's global package migrates that delegate to a real MeterProvider
// exactly once, on the FIRST otel.SetMeterProvider call in the process,
// binding already-created instruments to it permanently - a second
// otel.SetMeterProvider call in a later test does NOT rebind them (this was
// confirmed empirically: a per-test install/restore pattern silently dropped
// every Record* call after the first test). TestMain installs the manual
// reader exactly once so every test in this package shares one provider;
// tests avoid cross-test data-point collisions by giving each Record* call a
// distinct attribute value (Sum/Histogram instruments are cumulative across
// calls with identical attributes; Gauge instruments report only the latest
// value per attribute set), matching how a real process's kmetrics
// instruments accumulate for the whole process lifetime.
var testReader *metric.ManualReader

func TestMain(m *testing.M) {
	testReader = metric.NewManualReader()
	mp := metric.NewMeterProvider(metric.WithReader(testReader))
	otel.SetMeterProvider(mp)
	os.Exit(m.Run())
}

func collect(t *testing.T) metricdata.ResourceMetrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := testReader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() err = %v", err)
	}
	return rm
}

func findMetric(rm metricdata.ResourceMetrics, name string) (metricdata.Metrics, bool) {
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == name {
				return m, true
			}
		}
	}
	return metricdata.Metrics{}, false
}

// attrsMatch reports whether a data point's attribute.Set contains exactly
// the given key/value pairs (order-independent).
func attrsMatch(set attribute.Set, want ...attribute.KeyValue) bool {
	if set.Len() != len(want) {
		return false
	}
	for _, kv := range want {
		got, ok := set.Value(kv.Key)
		if !ok || got != kv.Value {
			return false
		}
	}
	return true
}

func sumDataPoint(t *testing.T, rm metricdata.ResourceMetrics, name string, attrs ...attribute.KeyValue) (int64, bool) {
	t.Helper()
	m, ok := findMetric(rm, name)
	if !ok {
		return 0, false
	}
	sum, ok := m.Data.(metricdata.Sum[int64])
	if !ok {
		t.Fatalf("metric %q is not a Sum[int64]: %T", name, m.Data)
	}
	for _, dp := range sum.DataPoints {
		if attrsMatch(dp.Attributes, attrs...) {
			return dp.Value, true
		}
	}
	return 0, false
}

func gaugeDataPoint(t *testing.T, rm metricdata.ResourceMetrics, name string, attrs ...attribute.KeyValue) (int64, bool) {
	t.Helper()
	m, ok := findMetric(rm, name)
	if !ok {
		return 0, false
	}
	gauge, ok := m.Data.(metricdata.Gauge[int64])
	if !ok {
		t.Fatalf("metric %q is not a Gauge[int64]: %T", name, m.Data)
	}
	for _, dp := range gauge.DataPoints {
		if attrsMatch(dp.Attributes, attrs...) {
			return dp.Value, true
		}
	}
	return 0, false
}

func histogramCount(t *testing.T, rm metricdata.ResourceMetrics, name string, attrs ...attribute.KeyValue) (uint64, bool) {
	t.Helper()
	m, ok := findMetric(rm, name)
	if !ok {
		return 0, false
	}
	hist, ok := m.Data.(metricdata.Histogram[float64])
	if !ok {
		t.Fatalf("metric %q is not a Histogram[float64]: %T", name, m.Data)
	}
	for _, dp := range hist.DataPoints {
		if attrsMatch(dp.Attributes, attrs...) {
			return dp.Count, true
		}
	}
	return 0, false
}

func TestRecordSweeperBatchRecordsDurationAlwaysAndCountersOnlyWhenPositive(t *testing.T) {
	ctx := context.Background()

	RecordSweeperBatch(ctx, "version", 1.5, 0, 0)
	rm := collect(t)

	if _, ok := histogramCount(t, rm, "kernel.sweeper.batch.duration", attribute.String("stage", "version")); !ok {
		t.Fatal("expected batch duration histogram to record even with zero obligations/tasks")
	}
	if _, ok := sumDataPoint(t, rm, "kernel.sweeper.obligations_swept", attribute.String("stage", "version")); ok {
		t.Fatal("expected no obligations_swept data point when obligations=0")
	}
	if _, ok := sumDataPoint(t, rm, "kernel.sweeper.tasks_created", attribute.String("stage", "version")); ok {
		t.Fatal("expected no tasks_created data point when tasksCreated=0")
	}

	RecordSweeperBatch(ctx, "version", 2.0, 5, 3)
	rm = collect(t)

	obligations, ok := sumDataPoint(t, rm, "kernel.sweeper.obligations_swept", attribute.String("stage", "version"))
	if !ok || obligations != 5 {
		t.Fatalf("obligations_swept = %d, ok=%v, want 5", obligations, ok)
	}
	tasks, ok := sumDataPoint(t, rm, "kernel.sweeper.tasks_created", attribute.String("stage", "version"))
	if !ok || tasks != 3 {
		t.Fatalf("tasks_created = %d, ok=%v, want 3", tasks, ok)
	}
}

func TestRecordOutboxBatchGaugeAlwaysRecordedCountersConditional(t *testing.T) {
	ctx := context.Background()

	RecordOutboxBatch(ctx, 0, 0, 0, 7)
	rm := collect(t)

	claimed, ok := gaugeDataPoint(t, rm, "kernel.outbox.queue_depth")
	if !ok || claimed != 7 {
		t.Fatalf("queue_depth = %d, ok=%v, want 7 (gauge records unconditionally)", claimed, ok)
	}
	if _, ok := sumDataPoint(t, rm, "kernel.outbox.reclaimed"); ok {
		t.Fatal("expected no reclaimed data point when reclaimed=0")
	}
	if _, ok := sumDataPoint(t, rm, "kernel.outbox.retry_scheduled"); ok {
		t.Fatal("expected no retry_scheduled data point when retryScheduled=0")
	}
	if _, ok := sumDataPoint(t, rm, "kernel.outbox.dead_letters"); ok {
		t.Fatal("expected no dead_letters data point when deadLetters=0")
	}

	RecordOutboxBatch(ctx, 2, 4, 1, 9)
	rm = collect(t)

	if v, ok := sumDataPoint(t, rm, "kernel.outbox.reclaimed"); !ok || v != 2 {
		t.Fatalf("reclaimed = %d, ok=%v, want 2", v, ok)
	}
	if v, ok := sumDataPoint(t, rm, "kernel.outbox.retry_scheduled"); !ok || v != 4 {
		t.Fatalf("retry_scheduled = %d, ok=%v, want 4", v, ok)
	}
	if v, ok := sumDataPoint(t, rm, "kernel.outbox.dead_letters"); !ok || v != 1 {
		t.Fatalf("dead_letters = %d, ok=%v, want 1", v, ok)
	}
	if v, ok := gaugeDataPoint(t, rm, "kernel.outbox.queue_depth"); !ok || v != 9 {
		t.Fatalf("queue_depth = %d, ok=%v, want 9 (latest gauge value)", v, ok)
	}
}

func TestRecordOutboxPublishLabelsOutcomeAndEventType(t *testing.T) {
	ctx := context.Background()

	RecordOutboxPublish(ctx, OutboxOutcomeDeadLetter, "vaccination.completed", 0.25)
	rm := collect(t)

	count, ok := histogramCount(t, rm, "kernel.outbox.publish.duration",
		attribute.String("outcome", "dead_letter"),
		attribute.String("event_type", "vaccination.completed"),
	)
	if !ok || count != 1 {
		t.Fatalf("publish duration count=%d ok=%v, want a data point labeled outcome/event_type", count, ok)
	}
}

func TestRecordConsumerHandleAndValidationErrorsAndLag(t *testing.T) {
	ctx := context.Background()

	RecordConsumerHandle(ctx, ConsumerOutcomeDuplicateSkipped, "goat.shifted", 0.01)
	RecordConsumerValidationError(ctx, "goat.shifted")
	RecordConsumerLag(ctx, 3.0)

	rm := collect(t)

	if _, ok := histogramCount(t, rm, "kernel.consumer.handle.duration",
		attribute.String("outcome", "duplicate_skipped"),
		attribute.String("event_type", "goat.shifted"),
	); !ok {
		t.Fatal("expected consumer handle duration data point")
	}
	if v, ok := sumDataPoint(t, rm, "kernel.consumer.validation_errors", attribute.String("event_type", "goat.shifted")); !ok || v != 1 {
		t.Fatalf("validation_errors = %d, ok=%v, want 1", v, ok)
	}
	if v, ok := gaugeDataPoint(t, rm, "kernel.consumer.lag"); !ok || v != 3 {
		t.Fatalf("consumer lag gauge = %d, ok=%v, want 3", v, ok)
	}
}

func TestRecordNotifySendFailureExhaustedLabelChannel(t *testing.T) {
	ctx := context.Background()

	RecordNotifySend(ctx, "sms", 0.5)
	RecordNotifyFailure(ctx, "sms")
	RecordNotifyExhausted(ctx, "sms")

	rm := collect(t)

	if _, ok := histogramCount(t, rm, "kernel.notify.send.duration", attribute.String("channel", "sms")); !ok {
		t.Fatal("expected notify send duration data point")
	}
	if v, ok := sumDataPoint(t, rm, "kernel.notify.failures", attribute.String("channel", "sms")); !ok || v != 1 {
		t.Fatalf("notify failures = %d, ok=%v, want 1", v, ok)
	}
	if v, ok := sumDataPoint(t, rm, "kernel.notify.exhausted", attribute.String("channel", "sms")); !ok || v != 1 {
		t.Fatalf("notify exhausted = %d, ok=%v, want 1", v, ok)
	}
}

func TestRecordCloudTasksEnqueueAndIdempotentCollision(t *testing.T) {
	ctx := context.Background()

	RecordCloudTasksEnqueue(ctx, 0.1)
	RecordCloudTasksIdempotentCollision(ctx)

	rm := collect(t)

	if _, ok := histogramCount(t, rm, "kernel.cloudtasks.enqueue.duration"); !ok {
		t.Fatal("expected cloud tasks enqueue duration data point")
	}
	if v, ok := sumDataPoint(t, rm, "kernel.cloudtasks.idempotent_collisions"); !ok || v != 1 {
		t.Fatalf("idempotent_collisions = %d, ok=%v, want 1", v, ok)
	}
}

func TestRecordAnalyticsRollupRunCountersOnlyWhenPositive(t *testing.T) {
	ctx := context.Background()

	RecordAnalyticsRollupRun(ctx, AnalyticsRollupOutcomeSkipped, 0.05, 0, 0)
	rm := collect(t)

	if _, ok := histogramCount(t, rm, "kernel.analytics_rollup.run.duration", attribute.String("outcome", "skipped")); !ok {
		t.Fatal("expected run duration data point even when rows/bytes are zero")
	}
	if _, ok := sumDataPoint(t, rm, "kernel.analytics_rollup.rows_written", attribute.String("outcome", "skipped")); ok {
		t.Fatal("expected no rows_written data point when rowsWritten=0")
	}
	if _, ok := sumDataPoint(t, rm, "kernel.analytics_rollup.bytes_billed", attribute.String("outcome", "skipped")); ok {
		t.Fatal("expected no bytes_billed data point when bytesBilled=0")
	}

	RecordAnalyticsRollupRun(ctx, AnalyticsRollupOutcomeSucceeded, 12.0, 42, 1024)
	rm = collect(t)

	if v, ok := sumDataPoint(t, rm, "kernel.analytics_rollup.rows_written", attribute.String("outcome", "succeeded")); !ok || v != 42 {
		t.Fatalf("rows_written = %d, ok=%v, want 42", v, ok)
	}
	if v, ok := sumDataPoint(t, rm, "kernel.analytics_rollup.bytes_billed", attribute.String("outcome", "succeeded")); !ok || v != 1024 {
		t.Fatalf("bytes_billed = %d, ok=%v, want 1024", v, ok)
	}
}

func TestAddCounterRecordHistogramRecordGaugeNilInstrumentsAreNoop(t *testing.T) {
	// Guards the nil-instrument short-circuit every kmetrics wrapper relies
	// on (newCounter/newHistogram/newGauge log-and-degrade to nil on a
	// creation error instead of panicking the process). Must not panic.
	ctx := context.Background()
	addCounter(ctx, nil, 1, attribute.String("k", "v"))
	recordHistogram(ctx, nil, 1.0, attribute.String("k", "v"))
	recordGauge(ctx, nil, 1, attribute.String("k", "v"))
}

func TestLogInstrumentationErrorNilSafety(t *testing.T) {
	// Must not panic on either nil logger or nil error.
	LogInstrumentationError(nil, nil)
}
