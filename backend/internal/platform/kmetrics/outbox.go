package kmetrics

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
)

var (
	outboxPublishDuration = newHistogram("kernel.outbox.publish.duration", "s", "Duration of a single outbox message publish attempt.")
	outboxQueueDepth      = newGauge("kernel.outbox.queue_depth", "{message}", "Outbox rows claimed in the most recent relay batch (proxy for queue depth).")
	outboxDeadLetters     = newCounter("kernel.outbox.dead_letters", "{message}", "Outbox messages moved to dead-letter after exhausting publish attempts.")
	outboxReclaimed       = newCounter("kernel.outbox.reclaimed", "{message}", "Outbox messages reclaimed from a stale publishing lease.")
	outboxRetryScheduled  = newCounter("kernel.outbox.retry_scheduled", "{message}", "Outbox messages scheduled for a retried publish attempt.")
	// outboxFailed counts the OTHER terminal state. dead_letter had a counter;
	// 'failed' -- which is where an invalid envelope and a permanent publish
	// failure both land, is never retried, and is never dead-lettered -- had
	// none. That is the exact class that produced 12 permanently undeliverable
	// weighing events with nothing counting them.
	outboxFailed = newCounter("kernel.outbox.failed", "{message}", "Outbox messages marked permanently failed (invalid envelope or non-retryable publish failure). Never retried, never dead-lettered.")
)

// OutboxPublishOutcome labels the outbox.publish.duration histogram so a
// single instrument covers success/failure/retry/dead-letter without
// exploding into four separate histograms.
type OutboxPublishOutcome string

const (
	OutboxOutcomePublished  OutboxPublishOutcome = "published"
	OutboxOutcomeRetry      OutboxPublishOutcome = "retry_scheduled"
	OutboxOutcomeFailed     OutboxPublishOutcome = "failed"
	OutboxOutcomeDeadLetter OutboxPublishOutcome = "dead_letter"
)

// RecordOutboxPublish records one publish attempt's duration and outcome.
// eventType is a bounded-cardinality label (schema-defined event types, not
// free-form data) per the cardinality guard in
// docs/observability/OBSERVABILITY_DESIGN.md section 3.
func RecordOutboxPublish(ctx context.Context, outcome OutboxPublishOutcome, eventType string, durationSeconds float64) {
	recordHistogram(ctx, outboxPublishDuration, durationSeconds,
		attribute.String("outcome", string(outcome)),
		attribute.String("event_type", eventType),
	)
}

// RecordOutboxBatch records the per-run outbox relay counts from
// domain.RunResult (internal/outbox/domain/types.go) - reclaimed leases,
// retry-scheduled, and dead-lettered messages - plus the claimed count as a
// point-in-time queue-depth proxy (the claimed batch size is the closest
// signal to "how much work was waiting" the relay's polling model exposes;
// a true queue depth requires a COUNT(*) query which the design intentionally
// avoids on the hot path).
// RecordOutboxFailed counts messages that reached the terminal 'failed' state.
func RecordOutboxFailed(ctx context.Context, count int) {
	if count > 0 {
		addCounter(ctx, outboxFailed, int64(count))
	}
}

func RecordOutboxBatch(ctx context.Context, reclaimed, retryScheduled, deadLetters, claimed int) {
	if reclaimed > 0 {
		addCounter(ctx, outboxReclaimed, int64(reclaimed))
	}
	if retryScheduled > 0 {
		addCounter(ctx, outboxRetryScheduled, int64(retryScheduled))
	}
	if deadLetters > 0 {
		addCounter(ctx, outboxDeadLetters, int64(deadLetters))
	}
	recordGauge(ctx, outboxQueueDepth, int64(claimed))
}
