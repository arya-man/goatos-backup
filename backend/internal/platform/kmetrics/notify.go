package kmetrics

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
)

var (
	notifySendDuration = newHistogram("kernel.notify.send.duration", "s", "Duration of one notification-dispatcher channel send attempt.")
	notifyFailures     = newCounter("kernel.notify.failures", "{notification}", "Notification sends that failed (will retry unless attempts are exhausted).")
	notifyExhausted    = newCounter("kernel.notify.exhausted", "{notification}", "Notification sends that exhausted all delivery attempts.")
)

// RecordNotifySend records one channel send's duration. channel is a
// bounded-cardinality label (sms/email/push/webhook/...), never a raw
// recipient address.
func RecordNotifySend(ctx context.Context, channel string, durationSeconds float64) {
	recordHistogram(ctx, notifySendDuration, durationSeconds, attribute.String("channel", channel))
}

// RecordNotifyFailure increments the per-channel failure counter for a send
// that will be retried.
func RecordNotifyFailure(ctx context.Context, channel string) {
	addCounter(ctx, notifyFailures, 1, attribute.String("channel", channel))
}

// RecordNotifyExhausted increments the per-channel exhausted counter for a
// send that used its last delivery attempt.
func RecordNotifyExhausted(ctx context.Context, channel string) {
	addCounter(ctx, notifyExhausted, 1, attribute.String("channel", channel))
}
