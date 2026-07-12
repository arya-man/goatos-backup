package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"cloud.google.com/go/bigquery"
	"github.com/jackc/pgx/v5/pgxpool"
)

// journeyQuerySQL computes, for one day partition, the per-(user, session)
// elapsed time between the funnel's first and last step events, then
// APPROX_QUANTILES for p50/p90/p99 plus completion/drop-off counts - all in
// one aggregation query against the single day partition. event_timestamp is
// microseconds since epoch in the GA4 export schema; dividing by 1000 yields
// milliseconds.
const journeyQuerySQL = "WITH steps AS (\n" +
	"  SELECT\n" +
	"    user_pseudo_id,\n" +
	"    (SELECT value.int_value FROM UNNEST(event_params) WHERE key = 'ga_session_id') AS session_id,\n" +
	"    event_name,\n" +
	"    event_timestamp\n" +
	"  FROM `%s.%s.events_*`\n" +
	"  WHERE _TABLE_SUFFIX = @event_date_suffix\n" +
	"    AND event_name IN (@first_step_event, @last_step_event)\n" +
	"),\n" +
	"spans AS (\n" +
	"  SELECT\n" +
	"    user_pseudo_id,\n" +
	"    session_id,\n" +
	"    MIN(IF(event_name = @first_step_event, event_timestamp, NULL)) AS start_ts,\n" +
	"    MAX(IF(event_name = @last_step_event, event_timestamp, NULL)) AS end_ts\n" +
	"  FROM steps\n" +
	"  GROUP BY user_pseudo_id, session_id\n" +
	")\n" +
	"SELECT\n" +
	"  APPROX_QUANTILES(IF(start_ts IS NOT NULL AND end_ts IS NOT NULL AND end_ts >= start_ts, CAST((end_ts - start_ts) / 1000 AS INT64), NULL), 100)[OFFSET(50)] AS p50_ms,\n" +
	"  APPROX_QUANTILES(IF(start_ts IS NOT NULL AND end_ts IS NOT NULL AND end_ts >= start_ts, CAST((end_ts - start_ts) / 1000 AS INT64), NULL), 100)[OFFSET(90)] AS p90_ms,\n" +
	"  APPROX_QUANTILES(IF(start_ts IS NOT NULL AND end_ts IS NOT NULL AND end_ts >= start_ts, CAST((end_ts - start_ts) / 1000 AS INT64), NULL), 100)[OFFSET(99)] AS p99_ms,\n" +
	"  COUNTIF(start_ts IS NOT NULL AND end_ts IS NOT NULL AND end_ts >= start_ts) AS completions,\n" +
	"  COUNTIF(start_ts IS NOT NULL AND (end_ts IS NULL OR end_ts < start_ts)) AS drop_offs\n" +
	"FROM spans"

// defaultJourneyKey names the single end-to-end journey rolled up today:
// funnel-start to funnel-end. Additional named journeys (e.g.
// "scan_to_submit") can reuse runJourneyRollup with a different (first,
// last) event pair once product wants more granular journeys.
const defaultJourneyKey = "login_to_submit"

type journeyRow struct {
	P50Ms       bigquery.NullInt64 `bigquery:"p50_ms"`
	P90Ms       bigquery.NullInt64 `bigquery:"p90_ms"`
	P99Ms       bigquery.NullInt64 `bigquery:"p99_ms"`
	Completions int64              `bigquery:"completions"`
	DropOffs    int64              `bigquery:"drop_offs"`
}

// runJourneyRollup aggregates the login->submit journey's duration
// percentiles and completion/drop-off counts for one day and upserts into
// analytics.journey_daily.
func runJourneyRollup(ctx context.Context, client *bigquery.Client, pool *pgxpool.Pool, cfg config) (rowsWritten int64, bytesBilled int64, err error) {
	if len(cfg.FunnelSteps) < 2 {
		return 0, 0, errors.New("journey rollup requires at least 2 funnel steps")
	}
	firstStep := cfg.FunnelSteps[0]
	lastStep := cfg.FunnelSteps[len(cfg.FunnelSteps)-1]

	sql := fmt.Sprintf(journeyQuerySQL, cfg.GA4BQProject, cfg.GA4Dataset)
	it, bytesBilled, err := runAggregationQuery(ctx, client, cfg.BQLocation, cfg.MaxBytesBilled, sql,
		bigquery.QueryParameter{Name: "event_date_suffix", Value: cfg.eventDateSuffix()},
		bigquery.QueryParameter{Name: "first_step_event", Value: firstStep.EventName},
		bigquery.QueryParameter{Name: "last_step_event", Value: lastStep.EventName},
	)
	if err != nil {
		return 0, bytesBilled, fmt.Errorf("journey bigquery query: %w", err)
	}

	var row journeyRow
	fetchErr := it.Next(&row)
	if fetchErr != nil && !isDone(fetchErr) {
		return 0, bytesBilled, fmt.Errorf("read journey row: %w", fetchErr)
	}

	now := time.Now()
	_, execErr := pool.Exec(ctx, `
		INSERT INTO analytics.journey_daily (
			tenant_id, event_date, journey_key, p50_ms, p90_ms, p99_ms, completions, drop_offs, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (tenant_id, event_date, journey_key) DO UPDATE SET
			p50_ms      = EXCLUDED.p50_ms,
			p90_ms      = EXCLUDED.p90_ms,
			p99_ms      = EXCLUDED.p99_ms,
			completions = EXCLUDED.completions,
			drop_offs   = EXCLUDED.drop_offs,
			updated_at  = EXCLUDED.updated_at
	`, cfg.TenantID, cfg.SourceDate, defaultJourneyKey, nullInt64OrZero(row.P50Ms), nullInt64OrZero(row.P90Ms), nullInt64OrZero(row.P99Ms), row.Completions, row.DropOffs, now)
	if execErr != nil {
		return 0, bytesBilled, fmt.Errorf("upsert analytics.journey_daily: %w", execErr)
	}

	return 1, bytesBilled, nil
}

func nullInt64OrZero(v bigquery.NullInt64) int64 {
	if !v.Valid {
		return 0
	}
	return v.Int64
}
