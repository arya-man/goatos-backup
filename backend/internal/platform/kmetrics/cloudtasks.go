package kmetrics

import (
	"context"
)

var (
	cloudTasksEnqueueDuration     = newHistogram("kernel.cloudtasks.enqueue.duration", "s", "Duration of one Cloud Tasks CreateTask call.")
	cloudTasksIdempotentCollision = newCounter("kernel.cloudtasks.idempotent_collisions", "{task}", "Cloud Tasks CreateTask calls that hit an existing task name (idempotent no-op).")
)

// RecordCloudTasksEnqueue records one CreateTask call's duration.
func RecordCloudTasksEnqueue(ctx context.Context, durationSeconds float64) {
	recordHistogram(ctx, cloudTasksEnqueueDuration, durationSeconds)
}

// RecordCloudTasksIdempotentCollision increments the counter for a
// CreateTask call that returned codes.AlreadyExists - the intended
// idempotent-replay path (internal/platform/taskqueue.EnqueueJSONPost treats
// it as success), tracked here so it stays visible instead of vanishing
// silently.
func RecordCloudTasksIdempotentCollision(ctx context.Context) {
	addCounter(ctx, cloudTasksIdempotentCollision, 1)
}
