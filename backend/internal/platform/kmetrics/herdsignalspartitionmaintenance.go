package kmetrics

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
)

var (
	herdSignalsPartitionMaintenanceRunDuration       = newHistogram("kernel.herd_signals_partition_maintenance.run.duration", "s", "Duration of one herd-signals-partition-maintenance Cloud Run Job invocation (ensure-future-partitions + prune-expired-partitions against herd_signal_packets).")
	herdSignalsPartitionMaintenancePartitionsCreated = newCounter("kernel.herd_signals_partition_maintenance.partitions_created", "{partition}", "Daily herd_signal_packets partitions created by one run. Should be a small, steady number (1/day in steady state) -- a sustained zero means the run has stopped happening and the pre-created runway (000200) is being consumed without replenishment.")
	herdSignalsPartitionMaintenancePartitionsDropped = newCounter("kernel.herd_signals_partition_maintenance.partitions_dropped", "{partition}", "Daily herd_signal_packets partitions DROPped (irreversible) by one run. Should be a small, steady number (~1/day in steady state) once the table is older than the retention window -- a sustained zero past that point means pruning has stopped and raw packets are accumulating unbounded.")
)

// HerdSignalsPartitionMaintenanceOutcome labels kernel.herd_signals_partition_maintenance.run.duration.
type HerdSignalsPartitionMaintenanceOutcome string

const (
	HerdSignalsPartitionMaintenanceOutcomeSucceeded HerdSignalsPartitionMaintenanceOutcome = "succeeded"
	HerdSignalsPartitionMaintenanceOutcomeFailed    HerdSignalsPartitionMaintenanceOutcome = "failed"
	// HerdSignalsPartitionMaintenanceOutcomeDryRun covers the -dry-run path, which only
	// ensures future partitions and deliberately skips pruning.
	HerdSignalsPartitionMaintenanceOutcomeDryRun HerdSignalsPartitionMaintenanceOutcome = "dry_run"
)

// RecordHerdSignalsPartitionMaintenanceRun records one herd-signals-partition-maintenance job
// invocation's duration, outcome, partitions created, and partitions dropped. This is the
// operator-facing signal that the retention job is actually running: an operator (or an alert)
// watching herd_signals_partition_maintenance.run.duration for a gap, or
// .partitions_created/.partitions_dropped for a sustained zero once the table is past its
// retention window, is how a silently-stopped-scheduling regression gets caught before the
// DEFAULT partition starts absorbing rows or raw packets grow unbounded (see
// docs/runbooks/herd-signals-partition-retention.md).
func RecordHerdSignalsPartitionMaintenanceRun(ctx context.Context, outcome HerdSignalsPartitionMaintenanceOutcome, durationSeconds float64, partitionsCreated, partitionsDropped int) {
	recordHistogram(ctx, herdSignalsPartitionMaintenanceRunDuration, durationSeconds, attribute.String("outcome", string(outcome)))
	if partitionsCreated > 0 {
		addCounter(ctx, herdSignalsPartitionMaintenancePartitionsCreated, int64(partitionsCreated), attribute.String("outcome", string(outcome)))
	}
	if partitionsDropped > 0 {
		addCounter(ctx, herdSignalsPartitionMaintenancePartitionsDropped, int64(partitionsDropped), attribute.String("outcome", string(outcome)))
	}
}
