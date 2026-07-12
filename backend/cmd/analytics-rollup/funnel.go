package main

import (
	"context"
	"fmt"
	"time"

	"cloud.google.com/go/bigquery"
	"github.com/jackc/pgx/v5/pgxpool"
)

// funnelQuerySQL aggregates GA4 export events for exactly one day partition
// (_TABLE_SUFFIX = @event_date_suffix) into one row per event_name: distinct
// user count and distinct (user, ga_session_id) session count. This is the
// ONLY query issued for the funnel rollup - one GROUP BY, one partition, no
// per-row export.
//
// ga_session_id is extracted from the standard GA4 export event_params
// repeated RECORD via the documented
// "(SELECT value.int_value FROM UNNEST(event_params) WHERE key = '...')"
// idiom (GA4 BigQuery Export schema).
const funnelQuerySQL = "SELECT\n" +
	"  event_name,\n" +
	"  COUNT(DISTINCT user_pseudo_id) AS users,\n" +
	"  COUNT(DISTINCT CONCAT(user_pseudo_id, '-', CAST((SELECT value.int_value FROM UNNEST(event_params) WHERE key = 'ga_session_id') AS STRING))) AS sessions\n" +
	"FROM `%s.%s.events_*`\n" +
	"WHERE _TABLE_SUFFIX = @event_date_suffix\n" +
	"  AND event_name IN UNNEST(@step_event_names)\n" +
	"GROUP BY event_name"

type funnelRow struct {
	EventName string `bigquery:"event_name"`
	Users     int64  `bigquery:"users"`
	Sessions  int64  `bigquery:"sessions"`
}

// runFunnelRollup aggregates one day of funnel step conversion from the GA4
// export and upserts it into analytics.funnel_daily. Returns rows written
// and BigQuery bytes billed.
func runFunnelRollup(ctx context.Context, client *bigquery.Client, pool *pgxpool.Pool, cfg config) (rowsWritten int64, bytesBilled int64, err error) {
	eventNames := make([]string, 0, len(cfg.FunnelSteps))
	for _, step := range cfg.FunnelSteps {
		eventNames = append(eventNames, step.EventName)
	}

	sql := fmt.Sprintf(funnelQuerySQL, cfg.GA4BQProject, cfg.GA4Dataset)
	it, bytesBilled, err := runAggregationQuery(ctx, client, cfg.BQLocation, cfg.MaxBytesBilled, sql,
		bigquery.QueryParameter{Name: "event_date_suffix", Value: cfg.eventDateSuffix()},
		bigquery.QueryParameter{Name: "step_event_names", Value: eventNames},
	)
	if err != nil {
		return 0, bytesBilled, fmt.Errorf("funnel bigquery query: %w", err)
	}

	usersByEvent := make(map[string]int64, len(cfg.FunnelSteps))
	sessionsByEvent := make(map[string]int64, len(cfg.FunnelSteps))
	for {
		var row funnelRow
		fetchErr := it.Next(&row)
		if isDone(fetchErr) {
			break
		}
		if fetchErr != nil {
			return 0, bytesBilled, fmt.Errorf("read funnel row: %w", fetchErr)
		}
		usersByEvent[row.EventName] = row.Users
		sessionsByEvent[row.EventName] = row.Sessions
	}

	funnelRows := buildFunnelDailyRows(cfg.TenantID, cfg.SourceDate, cfg.FunnelSteps, usersByEvent, sessionsByEvent, time.Now())
	tenantIDs, eventDates, funnelKeys, stepKeys, stepIndexes, users, sessions, conversions, updatedAts := funnelDailyRowsToColumns(funnelRows)

	_, execErr := pool.Exec(ctx, `
		INSERT INTO analytics.funnel_daily (
			tenant_id, event_date, funnel_key, step_key, step_index, users, sessions, conversions, updated_at
		)
		SELECT * FROM UNNEST(
			$1::uuid[], $2::date[], $3::text[], $4::text[], $5::int[], $6::bigint[], $7::bigint[], $8::bigint[], $9::timestamptz[]
		)
		ON CONFLICT (tenant_id, event_date, funnel_key, step_key) DO UPDATE SET
			step_index  = EXCLUDED.step_index,
			users       = EXCLUDED.users,
			sessions    = EXCLUDED.sessions,
			conversions = EXCLUDED.conversions,
			updated_at  = EXCLUDED.updated_at
	`, tenantIDs, eventDates, funnelKeys, stepKeys, stepIndexes, users, sessions, conversions, updatedAts)
	if execErr != nil {
		return 0, bytesBilled, fmt.Errorf("upsert analytics.funnel_daily: %w", execErr)
	}

	return int64(len(funnelRows)), bytesBilled, nil
}

// funnelDailyRow is one row bound for analytics.funnel_daily, in the shape
// funnelDailyRowsToColumns transposes for the UNNEST upsert.
type funnelDailyRow struct {
	TenantID    string
	EventDate   time.Time
	FunnelKey   string
	StepKey     string
	StepIndex   int32
	Users       int64
	Sessions    int64
	Conversions int64
	UpdatedAt   time.Time
}

// buildFunnelDailyRows maps the configured funnel steps plus their
// BigQuery-aggregated per-event user/session counts into analytics.funnel_daily
// rows, one per step (in FunnelSteps order) regardless of whether BigQuery
// returned a row for that step's event_name (a missing entry in
// usersByEvent/sessionsByEvent - e.g. an event Android does not emit yet -
// zero-values via the map's zero-value lookup, matching runFunnelRollup's
// prior inline behavior). conversions is the raw per-step user count, not a
// cascaded funnel figure - see the field's usage note in runFunnelRollup.
// Pure and independent of BigQuery/Postgres so it is unit-testable without
// either.
func buildFunnelDailyRows(tenantID string, eventDate time.Time, steps []FunnelStep, usersByEvent, sessionsByEvent map[string]int64, now time.Time) []funnelDailyRow {
	out := make([]funnelDailyRow, len(steps))
	for i, step := range steps {
		out[i] = funnelDailyRow{
			TenantID:    tenantID,
			EventDate:   eventDate,
			FunnelKey:   step.FunnelKey,
			StepKey:     step.StepKey,
			StepIndex:   int32(step.StepIndex),
			Users:       usersByEvent[step.EventName],
			Sessions:    sessionsByEvent[step.EventName],
			Conversions: usersByEvent[step.EventName],
			UpdatedAt:   now,
		}
	}
	return out
}

// funnelDailyRowsToColumns transposes rows into the parallel column slices
// pgx's UNNEST($1::uuid[], $2::date[], ...) call in runFunnelRollup expects,
// one slice per analytics.funnel_daily column in the same order as the SQL's
// UNNEST argument list. Pure: no BigQuery/Postgres dependency.
func funnelDailyRowsToColumns(rows []funnelDailyRow) (tenantIDs []string, eventDates []time.Time, funnelKeys, stepKeys []string, stepIndexes []int32, users, sessions, conversions []int64, updatedAts []time.Time) {
	n := len(rows)
	tenantIDs = make([]string, n)
	eventDates = make([]time.Time, n)
	funnelKeys = make([]string, n)
	stepKeys = make([]string, n)
	stepIndexes = make([]int32, n)
	users = make([]int64, n)
	sessions = make([]int64, n)
	conversions = make([]int64, n)
	updatedAts = make([]time.Time, n)
	for i, row := range rows {
		tenantIDs[i] = row.TenantID
		eventDates[i] = row.EventDate
		funnelKeys[i] = row.FunnelKey
		stepKeys[i] = row.StepKey
		stepIndexes[i] = row.StepIndex
		users[i] = row.Users
		sessions[i] = row.Sessions
		conversions[i] = row.Conversions
		updatedAts[i] = row.UpdatedAt
	}
	return
}
