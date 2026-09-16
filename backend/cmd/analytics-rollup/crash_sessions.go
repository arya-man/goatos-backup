package main

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"cloud.google.com/go/bigquery"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vgoats/goatos/backend/internal/platform/observability"
)

// projection-review: membership=collection-enabled Firebase session starts and same-day fatal events; group_key=app business-day version session/install identity; join_cardinality=fatal side distinct session before left join and distinct session/install counts; pagination=entire bounded daily export across all iterator pages; scope=explicit source app and matching export project without tenant attribution
// Schema: https://firebase.google.com/docs/crashlytics/bigquery-dataset-schema
// App-wide daily observed sessions, with fatal events in the same bounded day.
// Installation IDs are users here, not authenticated people or tenant IDs.
const appCrashSQL = `WITH sessions AS (
 SELECT DISTINCT instance_id, session_id, COALESCE(application.display_version, '') app_version
 FROM %s
 WHERE event_timestamp >= @start AND event_timestamp < @end
 AND event_type = 'SESSION_START' AND crashlytics_data_collection_enabled = TRUE
 AND instance_id IS NOT NULL AND instance_id != '' AND session_id IS NOT NULL AND session_id != ''
), fatal AS (
 SELECT DISTINCT firebase_session_id
 FROM %s
 WHERE event_timestamp >= @start AND event_timestamp < @end
 AND error_type = 'FATAL' AND firebase_session_id IS NOT NULL
)
SELECT s.app_version, COUNT(DISTINCT s.instance_id) total_users,
 COUNT(DISTINCT s.session_id) total_sessions,
 COUNT(DISTINCT IF(f.firebase_session_id IS NOT NULL, s.instance_id, NULL)) crashed_users,
 COUNT(DISTINCT f.firebase_session_id) crashed_sessions
FROM sessions s LEFT JOIN fatal f ON s.session_id = f.firebase_session_id
GROUP BY s.app_version`

var qualifiedCrashTable = regexp.MustCompile(`^[a-zA-Z0-9_-]+\.[a-zA-Z0-9_]+\.[a-zA-Z0-9_]+$`)

func crashSessionsAvailability(crashTable, sessionsTable string) error {
	if crashTable == "" && sessionsTable == "" {
		return nil
	}
	if !qualifiedCrashTable.MatchString(crashTable) || !qualifiedCrashTable.MatchString(sessionsTable) {
		return fmt.Errorf("Crashlytics requires both fully-qualified crash and Firebase sessions export tables (Include sessions)")
	}
	return nil
}

type appCrashRow struct {
	AppVersion      string `bigquery:"app_version"`
	TotalUsers      int64  `bigquery:"total_users"`
	TotalSessions   int64  `bigquery:"total_sessions"`
	CrashedUsers    int64  `bigquery:"crashed_users"`
	CrashedSessions int64  `bigquery:"crashed_sessions"`
}

func observedCrashFree(crashed, total int64) (*float64, error) {
	if total < 0 || crashed < 0 || crashed > total {
		return nil, fmt.Errorf("invalid crash/session aggregate")
	}
	if total == 0 {
		return nil, nil
	}
	value := 100 * (1 - float64(crashed)/float64(total))
	return &value, nil
}

func runOptionalCrashSessions(ctx context.Context, pool *pgxpool.Pool, cfg config) (int64, int64, error) {
	if err := crashSessionsAvailability(cfg.CrashlyticsTable, cfg.CrashlyticsSessionsTable); err != nil {
		return 0, 0, err
	}
	if cfg.CrashlyticsTable == "" {
		observability.New(observability.Config{Service: "analytics-rollup"}).InfoContext(ctx, "crash_export_unavailable", "reason", "Crashlytics Include sessions exports are not configured")
		return 0, 0, nil
	}
	if cfg.SourceAppID == "" {
		return 0, 0, fmt.Errorf("crash source-app-id is required: exports are app-wide, not tenant-scoped")
	}
	for _, table := range []string{cfg.CrashlyticsTable, cfg.CrashlyticsSessionsTable} {
		if strings.Split(table, ".")[0] != cfg.GA4BQProject {
			return 0, 0, fmt.Errorf("crash export project must match configured BigQuery project")
		}
	}
	client, err := bigquery.NewClient(ctx, cfg.GA4BQProject)
	if err != nil {
		return 0, 0, err
	}
	defer client.Close()
	sql := fmt.Sprintf(appCrashSQL, "`"+cfg.CrashlyticsSessionsTable+"`", "`"+cfg.CrashlyticsTable+"`")
	it, billed, err := runAggregationQuery(ctx, client, cfg.BQLocation, cfg.MaxBytesBilled, sql,
		bigquery.QueryParameter{Name: "start", Value: cfg.SourceDate}, bigquery.QueryParameter{Name: "end", Value: cfg.SourceDate.AddDate(0, 0, 1)})
	if err != nil {
		return 0, billed, err
	}
	var rows []appCrashRow
	for {
		var row appCrashRow
		err := it.Next(&row)
		if isDone(err) {
			break
		}
		if err != nil {
			return 0, billed, err
		}
		rows = append(rows, row)
	}
	written, err := replaceAppCrashRows(ctx, pool, cfg, rows)
	return written, billed, err
}
func replaceAppCrashRows(ctx context.Context, pool *pgxpool.Pool, cfg config, rows []appCrashRow) (int64, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "app_crash_daily:"+cfg.SourceAppID+":"+cfg.SourceDate.Format("2006-01-02")); err != nil {
		return 0, err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM analytics.app_crash_daily WHERE source_app_id=$1 AND event_date=$2`, cfg.SourceAppID, cfg.SourceDate); err != nil {
		return 0, err
	}
	batch := make([][]any, 0, len(rows))
	for _, r := range rows {
		users, e := observedCrashFree(r.CrashedUsers, r.TotalUsers)
		if e != nil {
			return 0, e
		}
		sessions, e := observedCrashFree(r.CrashedSessions, r.TotalSessions)
		if e != nil {
			return 0, e
		}
		batch = append(batch, []any{cfg.SourceAppID, cfg.SourceDate, r.AppVersion, r.TotalUsers, r.TotalSessions, r.CrashedUsers, r.CrashedSessions, users, sessions})
	}
	if len(batch) > 0 {
		if _, err = tx.CopyFrom(ctx, pgx.Identifier{"analytics", "app_crash_daily"}, []string{"source_app_id", "event_date", "app_version", "total_users", "total_sessions", "crashed_users", "crashed_sessions", "crash_free_users_pct", "crash_free_sessions_pct"}, pgx.CopyFromRows(batch)); err != nil {
			return 0, err
		}
	}

	if err = tx.Commit(ctx); err != nil {
		return 0, err
	}
	return int64(len(rows)), nil
}
