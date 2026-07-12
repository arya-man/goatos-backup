package kmetrics

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
)

var (
	sweeperBatchDuration    = newHistogram("kernel.sweeper.batch.duration", "s", "Duration of one obligation-sweeper protocol-version sweep.")
	sweeperObligationsSwept = newCounter("kernel.sweeper.obligations_swept", "{obligation}", "Obligations created or advanced by a sweeper run.")
	sweeperTasksCreated     = newCounter("kernel.sweeper.tasks_created", "{task}", "SOP batch tasks created by a sweeper run.")
)

// RecordSweeperBatch records one protocol-version sweep's duration plus the
// obligations/tasks it produced. stage distinguishes the sweeper's internal
// phases (e.g. "version", "park_consolidation", "mark_missed") so a single
// histogram covers the whole obligation-sweeper run shape.
func RecordSweeperBatch(ctx context.Context, stage string, durationSeconds float64, obligations, tasksCreated int) {
	recordHistogram(ctx, sweeperBatchDuration, durationSeconds, attribute.String("stage", stage))
	if obligations > 0 {
		addCounter(ctx, sweeperObligationsSwept, int64(obligations), attribute.String("stage", stage))
	}
	if tasksCreated > 0 {
		addCounter(ctx, sweeperTasksCreated, int64(tasksCreated), attribute.String("stage", stage))
	}
}
