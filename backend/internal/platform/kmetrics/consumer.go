package kmetrics

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
)

var (
	consumerHandleDuration   = newHistogram("kernel.consumer.handle.duration", "s", "Duration of one domain-event-consumer message handle.")
	consumerLag              = newGauge("kernel.consumer.lag", "s", "Age of a consumed domain event at the time it was handled (publish-to-handle latency).")
	consumerValidationErrors = newCounter("kernel.consumer.validation_errors", "{message}", "Domain event envelopes that failed schema validation.")
)

// ConsumerOutcome labels consumer.handle.duration.
type ConsumerOutcome string

const (
	ConsumerOutcomeProcessed        ConsumerOutcome = "processed"
	ConsumerOutcomeDuplicateSkipped ConsumerOutcome = "duplicate_skipped"
	ConsumerOutcomeFailed           ConsumerOutcome = "failed"
)

// RecordConsumerHandle records one message handle's duration and outcome.
func RecordConsumerHandle(ctx context.Context, outcome ConsumerOutcome, eventType string, durationSeconds float64) {
	recordHistogram(ctx, consumerHandleDuration, durationSeconds,
		attribute.String("outcome", string(outcome)),
		attribute.String("event_type", eventType),
	)
}

// RecordConsumerValidationError increments the validation-error counter for
// an envelope that failed schema validation before dispatch.
func RecordConsumerValidationError(ctx context.Context, eventType string) {
	addCounter(ctx, consumerValidationErrors, 1, attribute.String("event_type", eventType))
}

// RecordConsumerLag records the age of a message (publish time to handle
// time) when the caller has a reliable publish timestamp to diff against.
func RecordConsumerLag(ctx context.Context, lagSeconds float64) {
	recordGauge(ctx, consumerLag, int64(lagSeconds))
}
