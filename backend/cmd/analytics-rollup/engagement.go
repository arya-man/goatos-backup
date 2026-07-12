package main

import (
	"context"
	"fmt"
	"time"

	"cloud.google.com/go/bigquery"
	"github.com/jackc/pgx/v5/pgxpool"
)

// engagementQuerySQL computes DAU, session count, and average session
// duration for one day partition in one aggregation query. A "session" is
// grouped by (user_pseudo_id, ga_session_id); sessions with a NULL
// ga_session_id (events GA4 fires outside any session, e.g. some
// app-lifecycle events) are excluded from the session/avg-duration figures
// but still count toward DAU via the outer COUNT(DISTINCT user_pseudo_id).
const engagementQuerySQL = "WITH sessions AS (\n" +
	"  SELECT\n" +
	"    user_pseudo_id,\n" +
	"    (SELECT value.int_value FROM UNNEST(event_params) WHERE key = 'ga_session_id') AS session_id,\n" +
	"    MIN(event_timestamp) AS start_ts,\n" +
	"    MAX(event_timestamp) AS end_ts\n" +
	"  FROM `%s.%s.events_*`\n" +
	"  WHERE _TABLE_SUFFIX = @event_date_suffix\n" +
	"  GROUP BY user_pseudo_id, session_id\n" +
	")\n" +
	"SELECT\n" +
	"  COUNT(DISTINCT user_pseudo_id) AS dau,\n" +
	"  COUNTIF(session_id IS NOT NULL) AS sessions,\n" +
	"  CAST(IFNULL(AVG(IF(session_id IS NOT NULL, (end_ts - start_ts) / 1000, NULL)), 0) AS INT64) AS avg_session_ms\n" +
	"FROM sessions"

type engagementRow struct {
	DAU          int64 `bigquery:"dau"`
	Sessions     int64 `bigquery:"sessions"`
	AvgSessionMs int64 `bigquery:"avg_session_ms"`
}

// runEngagementRollup aggregates one day of DAU/session/avg-session-duration
// from the GA4 export, upserts analytics.engagement_daily, then recomputes
// wau_approx purely from Postgres history (no additional BigQuery query -
// see the wau_approx doc comment below). It also returns the day's raw
// dau/sessions counts so the optional crash rollup can approximate
// crash-free-user/session percentages against the same day's GA4 activity
// (see crash.go).
func runEngagementRollup(ctx context.Context, client *bigquery.Client, pool *pgxpool.Pool, cfg config) (rowsWritten int64, bytesBilled int64, dau int64, sessions int64, err error) {
	sql := fmt.Sprintf(engagementQuerySQL, cfg.GA4BQProject, cfg.GA4Dataset)
	it, bytesBilled, err := runAggregationQuery(ctx, client, cfg.BQLocation, cfg.MaxBytesBilled, sql,
		bigquery.QueryParameter{Name: "event_date_suffix", Value: cfg.eventDateSuffix()},
	)
	if err != nil {
		return 0, bytesBilled, 0, 0, fmt.Errorf("engagement bigquery query: %w", err)
	}

	var row engagementRow
	fetchErr := it.Next(&row)
	if fetchErr != nil && !isDone(fetchErr) {
		return 0, bytesBilled, 0, 0, fmt.Errorf("read engagement row: %w", fetchErr)
	}

	now := time.Now()
	_, execErr := pool.Exec(ctx, `
		INSERT INTO analytics.engagement_daily (
			tenant_id, event_date, dau, sessions, avg_session_ms, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (tenant_id, event_date) DO UPDATE SET
			dau            = EXCLUDED.dau,
			sessions       = EXCLUDED.sessions,
			avg_session_ms = EXCLUDED.avg_session_ms,
			updated_at     = EXCLUDED.updated_at
	`, cfg.TenantID, cfg.SourceDate, row.DAU, row.Sessions, row.AvgSessionMs, now)
	if execErr != nil {
		return 0, bytesBilled, 0, 0, fmt.Errorf("upsert analytics.engagement_daily: %w", execErr)
	}

	// wau_approx: a trailing 7-day SUM of daily-active-user counts already
	// stored in this same Postgres table, NOT a true distinct-7-day-active
	// count. Computing a real weekly-unique figure would require scanning
	// 7 GA4 date partitions in one BigQuery query, which breaks this job's
	// hard single-partition-per-run cost rule (see
	// docs/observability/OBSERVABILITY_DESIGN.md section 2.6 and this
	// package's bigquery.go). Summing daily actives double-counts a
	// returning user across days, so this is a labeled approximation (the
	// column name says "_approx") - a real trailing-unique WAU would need
	// either a separate, explicitly-approved 7-partition BigQuery query or
	// a BigQuery-side incremental HLL sketch column, neither of which is in
	// this pass's scope.
	_, wauErr := pool.Exec(ctx, `
		UPDATE analytics.engagement_daily e
		SET wau_approx = sub.total
		FROM (
			SELECT COALESCE(SUM(dau), 0) AS total
			FROM analytics.engagement_daily
			WHERE tenant_id = $1 AND event_date BETWEEN $2::date - INTERVAL '6 days' AND $2::date
		) sub
		WHERE e.tenant_id = $1 AND e.event_date = $2::date
	`, cfg.TenantID, cfg.SourceDate)
	if wauErr != nil {
		return 0, bytesBilled, 0, 0, fmt.Errorf("update analytics.engagement_daily wau_approx: %w", wauErr)
	}

	return 1, bytesBilled, row.DAU, row.Sessions, nil
}
