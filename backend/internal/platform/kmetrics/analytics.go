package kmetrics

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
)

var (
	analyticsRollupRunDuration    = newHistogram("kernel.analytics_rollup.run.duration", "s", "Duration of one analytics-rollup Cloud Run Job invocation (GA4/Crashlytics BigQuery aggregation + Postgres upsert).")
	analyticsRollupRowsWritten    = newCounter("kernel.analytics_rollup.rows_written", "{row}", "Rows upserted into analytics.* Postgres tables by one analytics-rollup run.")
	analyticsRollupExportFailures = newCounter("kernel.analytics_rollup.export_failures", "{failure}", "Optional Firebase/BigQuery export failures (Crashlytics, Performance) in an analytics-rollup run; isolated from the committed first-party rollup.")
	analyticsRollupBytesBilled    = newCounter("kernel.analytics_rollup.bytes_billed", "By", "BigQuery bytes billed across all queries in one analytics-rollup run - the cost signal for the GA4->BQ->Postgres rollup per OBSERVABILITY_DESIGN.md section 2.6.")
)

// AnalyticsRollupOutcome labels kernel.analytics_rollup.run.duration.
type AnalyticsRollupOutcome string

const (
	AnalyticsRollupOutcomeSucceeded AnalyticsRollupOutcome = "succeeded"
	AnalyticsRollupOutcomeFailed    AnalyticsRollupOutcome = "failed"
	// AnalyticsRollupOutcomeSkipped covers the GA4-export-not-linked-yet
	// exit-0 path (GOATOS_GA4_EXPORT_DATASET unset).
	AnalyticsRollupOutcomeSkipped AnalyticsRollupOutcome = "skipped"
	// AnalyticsRollupOutcomeDegraded: first-party Postgres summaries committed,
	// an optional Firebase/BigQuery export failed. The job exits 0.
	AnalyticsRollupOutcomeDegraded AnalyticsRollupOutcome = "degraded"
)

// RecordAnalyticsRollupRun records one analytics-rollup job invocation's
// duration, outcome, rows written, and BigQuery bytes billed. bytesBilled is
// the sum of TotalBytesBilled across every BigQuery query the run executed
// (funnel + journey + engagement + optional crash), letting an operator spot
// a runaway/unpartitioned query from the metric alone, without reading logs.
func RecordAnalyticsRollupRun(ctx context.Context, outcome AnalyticsRollupOutcome, durationSeconds float64, rowsWritten, bytesBilled int64) {
	recordHistogram(ctx, analyticsRollupRunDuration, durationSeconds, attribute.String("outcome", string(outcome)))
	if rowsWritten > 0 {
		addCounter(ctx, analyticsRollupRowsWritten, rowsWritten, attribute.String("outcome", string(outcome)))
	}
	if bytesBilled > 0 {
		addCounter(ctx, analyticsRollupBytesBilled, bytesBilled, attribute.String("outcome", string(outcome)))
	}
}

// RecordAnalyticsExportFailure counts one isolated optional export failure by
// provider (crashlytics, performance) so a permission/config break is visible
// without failing the job and re-dispatching the Postgres rollup.
func RecordAnalyticsExportFailure(ctx context.Context, provider string) {
	addCounter(ctx, analyticsRollupExportFailures, 1, attribute.String("provider", provider))
}
