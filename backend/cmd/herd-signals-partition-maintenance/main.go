// Command herd-signals-partition-maintenance runs the two partition-maintenance functions
// defined in backend/migrations/postgres/000201_herd_signal_packets_partition_maintenance.sql
// against herd_signal_packets:
//
//  1. herd_signal_packets_ensure_future_partitions(days-ahead) -- creates any missing daily
//     partition from today through today+days-ahead, so ingest never hits a missing-partition
//     error.
//  2. herd_signal_packets_prune_expired_partitions(retention-days) -- drops whole daily
//     partitions entirely older than the retention cutoff via DROP TABLE. This is IRREVERSIBLE:
//     the dropped partition's raw packets are gone, not archived.
//
// Intended to run once daily (recommended: a low-traffic hour, well ahead of the runway
// migration 000200 pre-created). This binary is deliberately thin: all the actual logic,
// including the hardcoded-to-herd_signal_packets safety guard, lives in the SQL functions so a
// direct `psql -c "select ..."` invocation from any scheduler has the exact same safety
// properties as this binary. This command exists for structured logging/metrics and for
// environments that already run Go cron jobs via this cmd/ pattern rather than raw SQL in a
// crontab.
//
// Scheduling: dev runs this daily via Cloud Scheduler -> Cloud Run Job, wired in
// infra/envs/dev/cloud_run_jobs.tf (local.kernel_jobs.herd_signals_partition_maintenance) the
// same way every other kernel maintenance job is scheduled. stg declares the Cloud Run Job
// (infra/envs/stg/cloud_run_jobs.tf) but has no Cloud Scheduler wiring for ANY job yet (that
// infra does not exist in this repo for stg today) -- see
// docs/runbooks/herd-signals-partition-retention.md for the manual stg trigger command and the
// operational contract (healthy/unhealthy signals) this job implements.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/kmetrics"
	"github.com/vgoats/goatos/backend/internal/platform/observability"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("herd-signals-partition-maintenance", flag.ContinueOnError)
	daysAhead := fs.Int("days-ahead", intEnv("GOATOS_HERD_SIGNALS_DAYS_AHEAD", 14), "ensure daily partitions exist through today+N days")
	retentionDays := fs.Int("retention-days", intEnv("GOATOS_HERD_SIGNALS_RETENTION_DAYS", 14), "drop daily partitions entirely older than N days (IRREVERSIBLE)")
	dryRun := fs.Bool("dry-run", false, "only run herd_signal_packets_ensure_future_partitions; skip pruning")
	timeout := fs.Duration("timeout", durationEnv("GOATOS_HERD_SIGNALS_PARTITION_MAINTENANCE_TIMEOUT", 2*time.Minute), "overall timeout")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *retentionDays < 1 {
		return fmt.Errorf("retention-days must be >= 1, got %d", *retentionDays)
	}
	if *daysAhead < 0 {
		return fmt.Errorf("days-ahead must be >= 0, got %d", *daysAhead)
	}
	if *timeout <= 0 {
		return fmt.Errorf("timeout must be positive, got %s", timeout)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	shutdown, err := observability.SetupTelemetry(ctx, observability.Config{Service: "herd-signals-partition-maintenance"})
	if err != nil {
		return err
	}
	defer func() { _ = observability.FlushWithTimeout(shutdown, observability.DefaultShutdownTimeout) }()

	logger := observability.New(observability.Config{Service: "herd-signals-partition-maintenance"})

	pgCfg := platformpg.ConfigFromEnv()
	pool, err := platformpg.Connect(ctx, pgCfg)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer pool.Close()

	runStart := time.Now()

	created, err := ensureFuturePartitions(ctx, pool, *daysAhead)
	if err != nil {
		kmetrics.RecordHerdSignalsPartitionMaintenanceRun(ctx, kmetrics.HerdSignalsPartitionMaintenanceOutcomeFailed, time.Since(runStart).Seconds(), created, 0)
		return fmt.Errorf("ensure future partitions: %w", err)
	}
	logger.Info("herd_signals_ensure_future_partitions_complete",
		slog.Int("created", created),
		slog.Int("days_ahead", *daysAhead),
	)
	fmt.Printf("ensure-future-partitions: %d created (days-ahead=%d)\n", created, *daysAhead)

	if *dryRun {
		logger.Info("herd_signals_partition_maintenance_dry_run_skip_prune")
		fmt.Println("dry-run: skipping herd_signal_packets_prune_expired_partitions")
		kmetrics.RecordHerdSignalsPartitionMaintenanceRun(ctx, kmetrics.HerdSignalsPartitionMaintenanceOutcomeDryRun, time.Since(runStart).Seconds(), created, 0)
		return nil
	}

	dropped, err := pruneExpiredPartitions(ctx, pool, *retentionDays)
	if err != nil {
		kmetrics.RecordHerdSignalsPartitionMaintenanceRun(ctx, kmetrics.HerdSignalsPartitionMaintenanceOutcomeFailed, time.Since(runStart).Seconds(), created, dropped)
		return fmt.Errorf("prune expired partitions: %w", err)
	}
	logger.Info("herd_signals_prune_expired_partitions_complete",
		slog.Int("dropped", dropped),
		slog.Int("retention_days", *retentionDays),
	)
	fmt.Printf("prune-expired-partitions: %d dropped (retention-days=%d)\n", dropped, *retentionDays)

	kmetrics.RecordHerdSignalsPartitionMaintenanceRun(ctx, kmetrics.HerdSignalsPartitionMaintenanceOutcomeSucceeded, time.Since(runStart).Seconds(), created, dropped)
	fmt.Printf("herd-signals-partition-maintenance complete created=%d dropped=%d days_ahead=%d retention_days=%d\n", created, dropped, *daysAhead, *retentionDays)
	return nil
}

// ensureFuturePartitions calls herd_signal_packets_ensure_future_partitions and returns the
// number of partitions actually created (idempotent no-op partitions do not count).
func ensureFuturePartitions(ctx context.Context, pool *pgxpool.Pool, daysAhead int) (int, error) {
	rows, err := pool.Query(ctx,
		`SELECT partition_name, created FROM public.herd_signal_packets_ensure_future_partitions($1)`,
		daysAhead,
	)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	created := 0
	for rows.Next() {
		var name string
		var wasCreated bool
		if err := rows.Scan(&name, &wasCreated); err != nil {
			return created, fmt.Errorf("scan ensure-partitions row: %w", err)
		}
		if wasCreated {
			created++
			fmt.Printf("created partition %s\n", name)
		}
	}
	if err := rows.Err(); err != nil {
		return created, err
	}
	return created, nil
}

// pruneExpiredPartitions calls herd_signal_packets_prune_expired_partitions and returns the
// number of partitions dropped. Each drop is IRREVERSIBLE -- the raw packets in that partition
// are gone, not archived (docs/modules/herd-signals-system-design.md Section 3.2).
func pruneExpiredPartitions(ctx context.Context, pool *pgxpool.Pool, retentionDays int) (int, error) {
	rows, err := pool.Query(ctx,
		`SELECT partition_name, range_start, range_end FROM public.herd_signal_packets_prune_expired_partitions($1)`,
		retentionDays,
	)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	dropped := 0
	for rows.Next() {
		var name string
		var start, end time.Time
		if err := rows.Scan(&name, &start, &end); err != nil {
			return dropped, fmt.Errorf("scan prune row: %w", err)
		}
		dropped++
		fmt.Printf("dropped partition %s [%s, %s)\n", name, start.Format("2006-01-02"), end.Format("2006-01-02"))
	}
	if err := rows.Err(); err != nil {
		return dropped, err
	}
	return dropped, nil
}

func getenv(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}

func intEnv(key string, fallback int) int {
	raw := getenv(key)
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}

func durationEnv(key string, fallback time.Duration) time.Duration {
	raw := getenv(key)
	if raw == "" {
		return fallback
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}
