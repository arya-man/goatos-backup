package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vgoats/goatos/backend/internal/platform/kmetrics"
)

// projection-review: membership=tenant app_events deduplicated client-event identity; group_key=tenant business-day flow actor/device/journey and proof/request/group identity; join_cardinality=one earliest ordered event per cohort step with lateral limit1 and distinct users/sessions; pagination=whole bounded daily population before any display paging; scope=tenant and IST day with explicit next-day completion window
// A conversion is one ordered cohort (proof, camera request or business group),
// not an event count. Sessions are authenticated actor/device/journey tuples.
// journey_id is Android's login-to-logout work session, not a GA4 auto-session.
type appFlow struct {
	Key      string   `json:"key"`
	Identity string   `json:"identity"`
	Events   []string `json:"events"`
	Steps    []string `json:"steps"`
}

func appEventFlows() []appFlow {
	flows := []appFlow{
		{"proof_delivery", "proof_id", []string{"proof_capture_completed", "proof_processing_completed", "proof_upload_started", "proof_upload_completed"}, []string{"capture", "processed", "upload_started", "synced"}},
		{"camera_capture", "request_token", []string{"proof_camera_requested", "proof_camera_finalized"}, []string{"requested", "finalized"}},
		{"login_to_bootstrap", "", []string{"login_success", "bootstrap_loaded"}, []string{"login", "bootstrap"}},
	}
	for _, feature := range []string{"feed_distribution", "feed_packing", "feed_wastage", "feed_transport"} {
		opened := feature + "_opened"
		if feature == "feed_packing" || feature == "feed_wastage" {
			opened = feature + "_complete_opened"
		}
		flows = append(flows, appFlow{feature, "group_key", []string{"observed_work_session", opened, feature + "_submitted", "sync_write_succeeded"}, []string{"observed_session", "opened", "enqueued", "synced"}})
	}
	return flows
}

// Event-time range is explicit and indexable. D+1 is a bounded completion
// lookahead; starts belong to D and expire after 24h. Re-running D after delayed
// ingestion atomically replaces its results. Missing tenant/actor/device/journey
// never gets pooled into a synthetic shared session.
const appEventsPrepareSQL = `
CREATE TEMP TABLE rollup_events ON COMMIT DROP AS
SELECT DISTINCT ON (COALESCE(NULLIF(client_event_id,''), NULLIF(properties->>'client_event_id',''), event_id::text))
 event_id, actor_id::text AS actor, COALESCE(NULLIF(device_id,''),NULLIF(properties->>'device_id','')) AS device,
 NULLIF(properties->>'journey_id','') AS journey, event_name, properties,
 COALESCE(client_event_time,received_at) AS ts
FROM analytics.app_events
WHERE tenant_id=$1 AND COALESCE(client_event_time,received_at)>=$2 AND COALESCE(client_event_time,received_at)<$3
ORDER BY COALESCE(NULLIF(client_event_id,''),NULLIF(properties->>'client_event_id',''),event_id::text),received_at,event_id;
`

const appFlowsPrepareSQL = `
CREATE TEMP TABLE rollup_flows ON COMMIT DROP AS
SELECT f.key AS flow, f.identity, event.value AS event_name, event.ordinality::int-1 AS step,
 f.steps->>(event.ordinality::int-1) AS step_key, jsonb_array_length(f.events)-1 AS last_step
FROM jsonb_to_recordset($1::jsonb) f(key text,identity text,events jsonb,steps jsonb)
CROSS JOIN LATERAL jsonb_array_elements_text(f.events) WITH ORDINALITY event;
`

const appStagesSQL = `
CREATE TEMP TABLE rollup_stages ON COMMIT DROP AS
WITH identified AS MATERIALIZED (
 SELECT *,jsonb_build_array(actor,device,journey)::text AS session
 FROM rollup_events WHERE actor IS NOT NULL AND device IS NOT NULL AND journey IS NOT NULL
), flow_events AS MATERIALIZED (
 SELECT f.flow,f.step,f.last_step,e.actor,e.session,
  CASE WHEN f.identity='' THEN e.session ELSE jsonb_build_array(e.session,e.properties->>f.identity)::text END AS cohort,e.ts
 FROM identified e JOIN rollup_flows f ON f.event_name=e.event_name
 WHERE (f.identity='' OR NULLIF(e.properties->>f.identity,'') IS NOT NULL)
 AND f.event_name NOT IN ('observed_work_session','sync_write_succeeded')
 AND (f.identity<>'group_key' OR f.step<>2 OR COALESCE(e.properties->>'action','')<>'sync_success')
 UNION ALL
 -- Success is server acknowledgement of the EXACT submitted outbox item;
 -- submitted alone means local enqueue, never a completed business write.
 SELECT f.flow,3,f.last_step,s.actor,s.session,jsonb_build_array(s.session,s.properties->>'group_key')::text,done.ts
 FROM identified s JOIN rollup_flows f ON f.step=2 AND f.identity='group_key' AND f.event_name=s.event_name
 JOIN identified done ON done.session=s.session AND done.event_name='sync_write_succeeded'
 AND NULLIF(s.properties->>'outbox_item_id','')=done.properties->>'outbox_item_id' AND done.ts>=s.ts
 WHERE NULLIF(s.properties->>'group_key','') IS NOT NULL
 UNION ALL
 -- Distribution also reports the actual terminal observer result. This can
 -- cover a missing generic sync event without counting enqueue as success.
 SELECT f.flow,3,f.last_step,e.actor,e.session,jsonb_build_array(e.session,e.properties->>'group_key')::text,e.ts
 FROM identified e JOIN rollup_flows f ON f.step=2 AND f.identity='group_key' AND f.event_name=e.event_name
 WHERE e.properties->>'action'='sync_success' AND NULLIF(e.properties->>'group_key','') IS NOT NULL
), observed_sessions AS (
 SELECT session,min(ts) AS ts FROM identified WHERE ts>=$1 AND ts<$2 GROUP BY session
), stages AS MATERIALIZED (
 SELECT * FROM flow_events
 UNION ALL
 SELECT DISTINCT e.flow,0,e.last_step,e.actor,e.session,e.cohort,s.ts
 FROM flow_events e JOIN observed_sessions s USING(session)
 WHERE e.step=1 AND e.flow IN ('feed_distribution','feed_packing','feed_wastage','feed_transport') AND e.ts>=$1 AND e.ts<$2
) SELECT * FROM stages;
`

const appSequencesSQL = `
CREATE TEMP TABLE rollup_sequences ON COMMIT DROP AS
WITH RECURSIVE starts AS (
 SELECT flow,cohort,actor,session,last_step,min(ts) AS start_ts
 FROM rollup_stages WHERE step=0 AND ts>=$1 AND ts<$2 GROUP BY flow,cohort,actor,session,last_step
), cohort_starts AS (
 SELECT s.*,CASE WHEN flow IN ('feed_distribution','feed_packing','feed_wastage','feed_transport')
 THEN (SELECT min(e.ts) FROM rollup_stages e WHERE e.flow=s.flow AND e.cohort=s.cohort AND e.step=1 AND e.ts>=s.start_ts)
 ELSE start_ts END AS expiry_start_ts FROM starts s
), sequence AS (
 SELECT flow,cohort,actor,session,last_step,start_ts,expiry_start_ts,0 AS step,start_ts AS reached_at FROM cohort_starts
 UNION ALL
 SELECT s.flow,s.cohort,s.actor,s.session,s.last_step,s.start_ts,s.expiry_start_ts,s.step+1,n.ts
 FROM sequence s JOIN LATERAL (
  SELECT min(e.ts) AS ts FROM rollup_stages e WHERE e.flow=s.flow AND e.cohort=s.cohort
  AND e.step=s.step+1 AND e.ts>=s.reached_at AND e.ts<=s.expiry_start_ts+interval '24 hours'
 ) n ON n.ts IS NOT NULL WHERE s.step<s.last_step
)
SELECT * FROM sequence;
`

// projection-review: membership=tenant app_events deduplicated client-event identity; group_key=tenant business-day flow actor/device/journey and proof/request/group identity; join_cardinality=one earliest ordered event per cohort step with lateral limit1 and distinct users/sessions; pagination=whole bounded daily population before any display paging; scope=tenant and IST day with explicit next-day completion window
const appFunnelInsertSQL = `
INSERT INTO analytics.funnel_daily(tenant_id,event_date,funnel_key,step_key,step_index,users,sessions,conversions)
SELECT $1,$2,f.flow,f.step_key,f.step,count(DISTINCT s.actor),count(DISTINCT s.session),count(s.cohort)
FROM rollup_flows f LEFT JOIN rollup_sequences s ON s.flow=f.flow AND s.step=f.step
GROUP BY f.flow,f.step_key,f.step;
`
const appJourneyInsertSQL = `
INSERT INTO analytics.journey_daily(tenant_id,event_date,journey_key,p50_ms,p90_ms,p99_ms,completions,drop_offs)
SELECT $1,$2,flow,
 COALESCE(round((percentile_cont(0.5) WITHIN GROUP(ORDER BY duration))::numeric),0)::bigint,
 COALESCE(round((percentile_cont(0.9) WITHIN GROUP(ORDER BY duration))::numeric),0)::bigint,
 COALESCE(round((percentile_cont(0.99) WITHIN GROUP(ORDER BY duration))::numeric),0)::bigint,
 count(duration),count(*) FILTER (WHERE duration IS NULL AND start_ts+interval '24 hours'<=now())
FROM (
 SELECT first.flow,first.expiry_start_ts AS start_ts,CASE WHEN done.step IS NOT NULL THEN extract(epoch FROM done.reached_at-first.reached_at)*1000 END AS duration
 FROM rollup_sequences first LEFT JOIN rollup_sequences done ON done.flow=first.flow AND done.cohort=first.cohort AND done.step=first.last_step
 WHERE first.step=CASE WHEN first.flow IN ('feed_distribution','feed_packing','feed_wastage','feed_transport') THEN 1 ELSE 0 END
) spans GROUP BY flow;
`
const appEngagementInsertSQL = `
INSERT INTO analytics.engagement_daily(tenant_id,event_date,dau,wau_approx,sessions,avg_session_ms)
SELECT $1,$2,
 (SELECT count(DISTINCT actor) FROM rollup_events WHERE ts>=$3 AND ts<$4),
 (SELECT count(DISTINCT actor_id) FROM analytics.app_events WHERE tenant_id=$1 AND COALESCE(client_event_time,received_at)>=$5 AND COALESCE(client_event_time,received_at)<$4),
 count(*),COALESCE(round(avg(duration)),0)::bigint
FROM (
 SELECT extract(epoch FROM max(ts)-min(ts))*1000 AS duration FROM rollup_events
 WHERE ts>=$3 AND ts<$4 AND actor IS NOT NULL AND device IS NOT NULL AND journey IS NOT NULL
 GROUP BY actor,device,journey
) sessions;
`

func runAppEventsRollup(ctx context.Context, pool *pgxpool.Pool, cfg config) (int64, error) {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return 0, err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	day := cfg.SourceDate
	end := day.AddDate(0, 0, 1)
	// Serialize all daily replacements for this tenant; no partially visible
	// tables or overlapping backfill writes. Raw events are never modified.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "analytics-rollup/"+cfg.TenantID); err != nil {
		return 0, err
	}
	if _, err = tx.Exec(ctx, appEventsPrepareSQL, cfg.TenantID, day, end.AddDate(0, 0, 1)); err != nil {
		return 0, fmt.Errorf("prepare app events: %w", err)
	}
	definitions, _ := json.Marshal(appEventFlows())
	if _, err = tx.Exec(ctx, appFlowsPrepareSQL, string(definitions)); err != nil {
		return 0, err
	}
	if _, err = tx.Exec(ctx, appStagesSQL, day, end); err != nil {
		return 0, fmt.Errorf("prepare app stages: %w", err)
	}
	if _, err = tx.Exec(ctx, `CREATE INDEX ON rollup_stages(flow,cohort,step,ts)`); err != nil {
		return 0, err
	}
	if _, err = tx.Exec(ctx, `ANALYZE rollup_stages`); err != nil {
		return 0, err
	}
	if _, err = tx.Exec(ctx, appSequencesSQL, day, end); err != nil {
		return 0, fmt.Errorf("ordered app sequences: %w", err)
	}
	if _, err = tx.Exec(ctx, `WITH funnel AS (DELETE FROM analytics.funnel_daily WHERE tenant_id=$1 AND event_date=$2), journey AS (DELETE FROM analytics.journey_daily WHERE tenant_id=$1 AND event_date=$2) DELETE FROM analytics.engagement_daily WHERE tenant_id=$1 AND event_date=$2`, cfg.TenantID, day.Format("2006-01-02")); err != nil {
		return 0, err
	}
	funnelTag, err := tx.Exec(ctx, appFunnelInsertSQL, cfg.TenantID, day.Format("2006-01-02"))
	if err != nil {
		return 0, err
	}
	journeyTag, err := tx.Exec(ctx, appJourneyInsertSQL, cfg.TenantID, day.Format("2006-01-02"))
	if err != nil {
		return 0, err
	}
	written := funnelTag.RowsAffected() + journeyTag.RowsAffected()

	tag, err := tx.Exec(ctx, appEngagementInsertSQL, cfg.TenantID, day.Format("2006-01-02"), day, end, day.AddDate(0, 0, -6))
	if err != nil {
		return 0, err
	}
	written += tag.RowsAffected()
	if err = tx.Commit(ctx); err != nil {
		return 0, err
	}
	return written, nil
}

// The default run refreshes yesterday. Explicit --source-date provides a safe
// bounded backfill unit; caller can iterate dates, each committed atomically.
func runAppEventsAndFinish(ctx context.Context, pool *pgxpool.Pool, cfg config, runID int64, started time.Time) error {
	var rows, bytes int64
	days := cfg.LookbackDays
	if days == 0 {
		days = 1
	}
	for offset := days - 1; offset >= 0; offset-- {
		one := cfg
		one.SourceDate = cfg.SourceDate.AddDate(0, 0, -offset)
		written, err := runAppEventsRollup(ctx, pool, one)
		if err != nil {
			return failRun(ctx, pool, runID, started, rows, bytes, err)
		}
		rows += written
		crashRows, crashBytes, err := runOptionalCrashSessions(ctx, pool, one)
		rows += crashRows
		bytes += crashBytes
		if err != nil {
			return failRun(ctx, pool, runID, started, rows, bytes, err)
		}
		perfRows, perfBytes, err := runOptionalPerformance(ctx, pool, one)
		rows += perfRows
		bytes += perfBytes
		if err != nil {
			return failRun(ctx, pool, runID, started, rows, bytes, err)
		}
	}
	var err error
	if err = finishRollupRun(ctx, pool, runID, "succeeded", rows, bytes, nil); err != nil {
		return err
	}
	kmetrics.RecordAnalyticsRollupRun(ctx, kmetrics.AnalyticsRollupOutcomeSucceeded, time.Since(started).Seconds(), rows, bytes)
	fmt.Printf("analytics app_events rollup complete tenant=%s source_date=%s rows_written=%d\n", cfg.TenantID, cfg.SourceDate.Format("2006-01-02"), rows)
	return nil
}
