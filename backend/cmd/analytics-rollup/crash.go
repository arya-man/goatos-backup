package main

import (
	"context"
	"fmt"
	"time"

	"cloud.google.com/go/bigquery"
	"github.com/jackc/pgx/v5/pgxpool"
)

// crashQuerySQL aggregates one day of the Crashlytics BigQuery export into
// per-app_version fatal/nonfatal counts and a top-5 issue list.
//
// UNVERIFIED SCHEMA ASSUMPTION (flagged per task instructions - no Context7
// access this session, matching the same "Invalid API key" limitation noted
// in docs/observability/INFRA.md section 11): this assumes the commonly
// documented Firebase Crashlytics BigQuery export columns
// (application.display_version, is_fatal, issue.id, issue.title,
// event_timestamp). Confirm these exact column/struct names against the
// live export table (`bq show --schema <project>:<dataset>.<table>`) before
// enabling GOATOS_CRASHLYTICS_BQ_TABLE in a real deployment - a mismatch
// fails the query with a clear "column not found" BigQuery error rather
// than silently returning wrong data, but it will fail the run.
//
// This query filters on DATE(event_timestamp, 'Asia/Kolkata') rather than a
// _TABLE_SUFFIX, because (unlike GA4's day-sharded events_* tables) the
// Crashlytics export is normally a single continuously-appended table per
// app. If the live table is date-partitioned on event_timestamp, BigQuery's
// partition pruning still applies to this filter; if it is not partitioned
// at all, MaxBytesBilled remains the safety net against an unbounded scan -
// tune GOATOS_ANALYTICS_BQ_MAX_BYTES accordingly once real export volume is
// known.
const crashQuerySQL = "WITH crashes AS (\n" +
	"  SELECT\n" +
	"    IFNULL(application.display_version, '') AS app_version,\n" +
	"    is_fatal,\n" +
	"    issue.id AS issue_id,\n" +
	"    issue.title AS issue_title\n" +
	"  FROM `%s`\n" +
	"  WHERE DATE(event_timestamp, 'Asia/Kolkata') = @event_date\n" +
	"),\n" +
	"issue_counts AS (\n" +
	"  SELECT app_version, issue_id, ANY_VALUE(issue_title) AS issue_title, COUNT(*) AS issue_count\n" +
	"  FROM crashes\n" +
	"  GROUP BY app_version, issue_id\n" +
	"),\n" +
	"top_issues AS (\n" +
	"  SELECT app_version, ARRAY_AGG(STRUCT(issue_id, issue_title, issue_count) ORDER BY issue_count DESC LIMIT 5) AS top_issues\n" +
	"  FROM issue_counts\n" +
	"  GROUP BY app_version\n" +
	")\n" +
	"SELECT\n" +
	"  c.app_version,\n" +
	"  COUNTIF(c.is_fatal) AS fatal_count,\n" +
	"  COUNTIF(NOT c.is_fatal) AS nonfatal_count,\n" +
	"  TO_JSON_STRING(ANY_VALUE(t.top_issues)) AS top_issues_json\n" +
	"FROM crashes c\n" +
	"LEFT JOIN top_issues t USING (app_version)\n" +
	"GROUP BY c.app_version"

type crashRow struct {
	AppVersion    string              `bigquery:"app_version"`
	FatalCount    int64               `bigquery:"fatal_count"`
	NonfatalCount int64               `bigquery:"nonfatal_count"`
	TopIssuesJSON bigquery.NullString `bigquery:"top_issues_json"`
}

// runCrashRollup aggregates one day of Crashlytics export data per
// app_version and upserts analytics.crash_daily. It is a no-op (0, 0, nil)
// when cfg.CrashlyticsTable is empty - Crashlytics linkage is optional.
//
// crash_free_users_pct/crash_free_sessions_pct are approximated against the
// same day's GA4 engagement counts (dauForDate/sessionsForDate), because the
// Crashlytics BigQuery export has no shared session/user key with GA4's
// export to join on - this is a labeled best-effort ratio
// (1 - crashed_events/total_{users,sessions}), not an exact per-session
// crash-free measurement. Revisit once a verified join key (e.g. a shared
// Crashlytics<->GA4 user id) is confirmed.
func runCrashRollup(ctx context.Context, client *bigquery.Client, pool *pgxpool.Pool, cfg config, dauForDate, sessionsForDate int64) (rowsWritten int64, bytesBilled int64, err error) {
	if cfg.CrashlyticsTable == "" {
		return 0, 0, nil
	}

	sql := fmt.Sprintf(crashQuerySQL, cfg.CrashlyticsTable)
	it, bytesBilled, err := runAggregationQuery(ctx, client, cfg.BQLocation, cfg.MaxBytesBilled, sql,
		bigquery.QueryParameter{Name: "event_date", Value: cfg.SourceDate.Format("2006-01-02")},
	)
	if err != nil {
		return 0, bytesBilled, fmt.Errorf("crash bigquery query: %w", err)
	}

	var rows []crashRow
	for {
		var row crashRow
		fetchErr := it.Next(&row)
		if isDone(fetchErr) {
			break
		}
		if fetchErr != nil {
			return 0, bytesBilled, fmt.Errorf("read crash row: %w", fetchErr)
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		return 0, bytesBilled, nil
	}

	crashRows := buildCrashDailyRows(cfg.TenantID, cfg.SourceDate, rows, dauForDate, sessionsForDate, time.Now())
	tenantIDs, eventDates, appVersions, crashFreeUsersPct, crashFreeSessionsPct, fatalCounts, nonfatalCounts, topIssues, updatedAts := crashDailyRowsToColumns(crashRows)

	_, execErr := pool.Exec(ctx, `
		INSERT INTO analytics.crash_daily (
			tenant_id, event_date, app_version, crash_free_users_pct, crash_free_sessions_pct,
			fatal_count, nonfatal_count, top_issues, updated_at
		)
		SELECT
			t.tenant_id, t.event_date, t.app_version, t.crash_free_users_pct, t.crash_free_sessions_pct,
			t.fatal_count, t.nonfatal_count, t.top_issues::jsonb, t.updated_at
		FROM UNNEST($1::uuid[], $2::date[], $3::text[], $4::float8[], $5::float8[], $6::bigint[], $7::bigint[], $8::text[], $9::timestamptz[])
			AS t(tenant_id, event_date, app_version, crash_free_users_pct, crash_free_sessions_pct, fatal_count, nonfatal_count, top_issues, updated_at)
		ON CONFLICT (tenant_id, event_date, app_version) DO UPDATE SET
			crash_free_users_pct    = EXCLUDED.crash_free_users_pct,
			crash_free_sessions_pct = EXCLUDED.crash_free_sessions_pct,
			fatal_count             = EXCLUDED.fatal_count,
			nonfatal_count          = EXCLUDED.nonfatal_count,
			top_issues              = EXCLUDED.top_issues,
			updated_at              = EXCLUDED.updated_at
	`, tenantIDs, eventDates, appVersions, crashFreeUsersPct, crashFreeSessionsPct, fatalCounts, nonfatalCounts, topIssues, updatedAts)
	if execErr != nil {
		return 0, bytesBilled, fmt.Errorf("upsert analytics.crash_daily: %w", execErr)
	}

	return int64(len(crashRows)), bytesBilled, nil
}

// crashFreeRatioPct returns a 0-100 crash-free percentage given a crash
// count against a total (users or sessions); a zero/negative total is
// treated as 100% crash-free (no denominator, nothing to divide by) rather
// than dividing by zero.
func crashFreeRatioPct(crashCount, total int64) float64 {
	if total <= 0 {
		return 100
	}
	pct := 100 * (1 - float64(crashCount)/float64(total))
	if pct < 0 {
		return 0
	}
	if pct > 100 {
		return 100
	}
	return pct
}

// crashDailyRow is one row bound for analytics.crash_daily, in the shape
// crashDailyRowsToColumns transposes for the UNNEST upsert.
type crashDailyRow struct {
	TenantID             string
	EventDate            time.Time
	AppVersion           string
	CrashFreeUsersPct    float64
	CrashFreeSessionsPct float64
	FatalCount           int64
	NonfatalCount        int64
	TopIssues            string
	UpdatedAt            time.Time
}

// buildCrashDailyRows maps one BigQuery crash-rollup result page into
// analytics.crash_daily rows: it stamps the shared tenant/event_date/updated_at,
// computes the approximate crash-free percentages via crashFreeRatioPct, and
// defaults an empty/absent top_issues_json to the JSON empty array "[]" (the
// column is NOT NULL jsonb). Pure and independent of BigQuery/Postgres so it
// is unit-testable without either.
func buildCrashDailyRows(tenantID string, eventDate time.Time, rows []crashRow, dauForDate, sessionsForDate int64, now time.Time) []crashDailyRow {
	out := make([]crashDailyRow, len(rows))
	for i, row := range rows {
		topIssues := "[]"
		if row.TopIssuesJSON.Valid && row.TopIssuesJSON.StringVal != "" {
			topIssues = row.TopIssuesJSON.StringVal
		}
		out[i] = crashDailyRow{
			TenantID:             tenantID,
			EventDate:            eventDate,
			AppVersion:           row.AppVersion,
			CrashFreeUsersPct:    crashFreeRatioPct(row.FatalCount, dauForDate),
			CrashFreeSessionsPct: crashFreeRatioPct(row.FatalCount, sessionsForDate),
			FatalCount:           row.FatalCount,
			NonfatalCount:        row.NonfatalCount,
			TopIssues:            topIssues,
			UpdatedAt:            now,
		}
	}
	return out
}

// crashDailyRowsToColumns transposes rows into the parallel column slices
// pgx's UNNEST($1::uuid[], $2::date[], ...) call in runCrashRollup expects,
// one slice per analytics.crash_daily column in the same order as the SQL's
// UNNEST argument list. Pure: no BigQuery/Postgres dependency.
func crashDailyRowsToColumns(rows []crashDailyRow) (tenantIDs []string, eventDates []time.Time, appVersions []string, crashFreeUsersPct, crashFreeSessionsPct []float64, fatalCounts, nonfatalCounts []int64, topIssues []string, updatedAts []time.Time) {
	n := len(rows)
	tenantIDs = make([]string, n)
	eventDates = make([]time.Time, n)
	appVersions = make([]string, n)
	crashFreeUsersPct = make([]float64, n)
	crashFreeSessionsPct = make([]float64, n)
	fatalCounts = make([]int64, n)
	nonfatalCounts = make([]int64, n)
	topIssues = make([]string, n)
	updatedAts = make([]time.Time, n)
	for i, row := range rows {
		tenantIDs[i] = row.TenantID
		eventDates[i] = row.EventDate
		appVersions[i] = row.AppVersion
		crashFreeUsersPct[i] = row.CrashFreeUsersPct
		crashFreeSessionsPct[i] = row.CrashFreeSessionsPct
		fatalCounts[i] = row.FatalCount
		nonfatalCounts[i] = row.NonfatalCount
		topIssues[i] = row.TopIssues
		updatedAts[i] = row.UpdatedAt
	}
	return
}
