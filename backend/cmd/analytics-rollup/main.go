// Command analytics-rollup is the Goat OS GA4 -> BigQuery -> Postgres
// analytics rollup job (docs/observability/OBSERVABILITY_DESIGN.md section
// 2.6). It runs once per day (Cloud Scheduler -> Cloud Run Job, see
// infra/envs/stg/analytics_rollup.tf), aggregates exactly one GA4 export day
// partition (plus, optionally, one day of the Crashlytics export) inside
// BigQuery, and upserts compact rollups into Cloud SQL Postgres
// analytics.funnel_daily / journey_daily / engagement_daily / crash_daily.
// Grafana's Postgres datasource then serves every dashboard load from these
// tables - BigQuery is scanned once per day, never once per dashboard view.
//
// Every BigQuery query issued by this job aggregates in SQL (GROUP BY /
// APPROX_QUANTILES) against a single date partition and carries a
// MaxBytesBilled cap (bigquery.go), per the scale/cost rules in this job's
// task brief: no raw event rows are pulled into Go, no N+1 queries, no
// unbounded reads.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"cloud.google.com/go/bigquery"
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
	cfg, err := parseConfig(args)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()

	shutdown, err := observability.SetupTelemetry(ctx, observability.Config{Service: "analytics-rollup"})
	if err != nil {
		return err
	}
	defer func() { _ = observability.FlushWithTimeout(shutdown, observability.DefaultShutdownTimeout) }()

	logger := observability.New(observability.Config{Service: "analytics-rollup"})

	pgCfg := platformpg.ConfigFromEnv()
	pool, err := platformpg.Connect(ctx, pgCfg)
	if err != nil {
		return err
	}
	defer pool.Close()

	runStart := time.Now()
	runID, err := startRollupRun(ctx, pool, cfg.SourceDate)
	if err != nil {
		return fmt.Errorf("start rollup_run audit row: %w", err)
	}

	// GA4 export not linked yet (docs/observability/INFRA.md section 9 is a
	// manual, per-environment Firebase console step). The Cloud Scheduler
	// trigger runs daily regardless of whether that manual step has
	// happened, so this MUST exit 0, not crash-loop the Cloud Run Job.
	if cfg.GA4Dataset == "" {
		logger.Info("ga4_export_not_linked_yet",
			slog.String("source_date", cfg.SourceDate.Format("2006-01-02")),
			slog.String("next_step", "link GA4->BigQuery in the Firebase console, then set GOATOS_GA4_EXPORT_DATASET (see docs/observability/INFRA.md section 9)"),
		)
		if finishErr := finishRollupRun(ctx, pool, runID, "skipped", 0, 0, nil); finishErr != nil {
			return fmt.Errorf("finish rollup_run audit row (skipped): %w", finishErr)
		}
		kmetrics.RecordAnalyticsRollupRun(ctx, kmetrics.AnalyticsRollupOutcomeSkipped, time.Since(runStart).Seconds(), 0, 0)
		return nil
	}

	client, err := bigquery.NewClient(ctx, cfg.GA4BQProject)
	if err != nil {
		runErr := fmt.Errorf("create bigquery client: %w", err)
		_ = finishRollupRun(ctx, pool, runID, "failed", 0, 0, runErr)
		kmetrics.RecordAnalyticsRollupRun(ctx, kmetrics.AnalyticsRollupOutcomeFailed, time.Since(runStart).Seconds(), 0, 0)
		return runErr
	}
	defer client.Close()

	var totalRows, totalBytesBilled int64

	funnelRows, funnelBytes, err := runFunnelRollup(ctx, client, pool, cfg)
	totalBytesBilled += funnelBytes
	if err != nil {
		return failRun(ctx, pool, runID, runStart, totalRows, totalBytesBilled, err)
	}
	totalRows += funnelRows
	logger.Info("funnel_rollup_complete", slog.Int64("rows", funnelRows), slog.Int64("bytes_billed", funnelBytes))

	journeyRows, journeyBytes, err := runJourneyRollup(ctx, client, pool, cfg)
	totalBytesBilled += journeyBytes
	if err != nil {
		return failRun(ctx, pool, runID, runStart, totalRows, totalBytesBilled, err)
	}
	totalRows += journeyRows
	logger.Info("journey_rollup_complete", slog.Int64("rows", journeyRows), slog.Int64("bytes_billed", journeyBytes))

	engagementRows, engagementBytes, dau, sessions, err := runEngagementRollup(ctx, client, pool, cfg)
	totalBytesBilled += engagementBytes
	if err != nil {
		return failRun(ctx, pool, runID, runStart, totalRows, totalBytesBilled, err)
	}
	totalRows += engagementRows
	logger.Info("engagement_rollup_complete", slog.Int64("rows", engagementRows), slog.Int64("bytes_billed", engagementBytes), slog.Int64("dau", dau), slog.Int64("sessions", sessions))

	if cfg.CrashlyticsTable == "" {
		logger.Info("crashlytics_export_not_configured", slog.String("next_step", "set GOATOS_CRASHLYTICS_BQ_TABLE once Crashlytics->BigQuery is linked to enable analytics.crash_daily"))
	}
	crashRows, crashBytes, err := runCrashRollup(ctx, client, pool, cfg, dau, sessions)
	totalBytesBilled += crashBytes
	if err != nil {
		return failRun(ctx, pool, runID, runStart, totalRows, totalBytesBilled, err)
	}
	totalRows += crashRows
	if crashRows > 0 {
		logger.Info("crash_rollup_complete", slog.Int64("rows", crashRows), slog.Int64("bytes_billed", crashBytes))
	}

	if finishErr := finishRollupRun(ctx, pool, runID, "succeeded", totalRows, totalBytesBilled, nil); finishErr != nil {
		return fmt.Errorf("finish rollup_run audit row (succeeded): %w", finishErr)
	}
	kmetrics.RecordAnalyticsRollupRun(ctx, kmetrics.AnalyticsRollupOutcomeSucceeded, time.Since(runStart).Seconds(), totalRows, totalBytesBilled)

	fmt.Printf("analytics rollup complete tenant=%s source_date=%s rows_written=%d bytes_billed=%d\n",
		cfg.TenantID, cfg.SourceDate.Format("2006-01-02"), totalRows, totalBytesBilled)
	return nil
}

// failRun records the run's audit row and metric as failed before
// propagating err, so a mid-run BigQuery/Postgres failure is never silently
// lost - the rollup_run row and kmetrics counter both reflect "failed" even
// though the process then exits non-zero via main().
func failRun(ctx context.Context, pool *pgxpool.Pool, runID int64, runStart time.Time, rowsWritten, bytesBilled int64, runErr error) error {
	_ = finishRollupRun(ctx, pool, runID, "failed", rowsWritten, bytesBilled, runErr)
	kmetrics.RecordAnalyticsRollupRun(ctx, kmetrics.AnalyticsRollupOutcomeFailed, time.Since(runStart).Seconds(), rowsWritten, bytesBilled)
	return runErr
}
