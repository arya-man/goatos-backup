package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/herdsignals/ports"
	"github.com/vgoats/goatos/backend/internal/platform/sqlbind"
)

// riskClassifierLockClass is the first key of the per-tenant classifier advisory xact lock
// (the second is hashtext(tenant_id)). Transaction-scoped: it can never outlive its batch or
// be returned to the pool held.
const riskClassifierLockClass int32 = 0x48535249 // "HSRI"

const riskTryLockSQL = `SELECT pg_try_advisory_xact_lock($1::int4, hashtext($2::text))`

// Re-evaluation floor: after a classification, new packets alone re-queue a tag only after
// risk_due_at = now() + a per-tag deterministic offset in [floor/2, 3*floor/2) (hash of tag_id),
// so a herd classified together spreads out instead of coming due as one wave. Ingest pulls
// risk_due_at forward immediately when movement/pattern changes or motion crosses the
// far-below-own-baseline line (repository.go updateTagLatest), and pen-median moves and
// staleness re-queue regardless.
const riskDueOffsetExpr = `make_interval(secs => $2::float8 / 2 + ((hashtext(tl.tag_id) & 2147483647) % 1000) / 1000.0 * $2::float8)`

// riskPickQueueSQL picks change-driven work WITHOUT row locks: never classified / queued, or
// reporting after risk_due_at (herd_signal_tag_latest_risk_dirty_idx), plus tags that went stale
// since their last evaluation (bounded to 3h of last_seen via (tenant_id, last_seen_at)).
var riskPickQueueSQL = `
		(SELECT tag_id FROM public.herd_signal_tag_latest
		 WHERE tenant_id = $1 AND (risk_evaluated_at IS NULL OR last_seen_at > risk_due_at)
		 ORDER BY tag_id LIMIT $2)
		UNION
		(SELECT tag_id FROM public.herd_signal_tag_latest
		 WHERE tenant_id = $1
		   AND last_seen_at < now() - ` + staleAfterInterval + `
		   AND last_seen_at > now() - interval '3 hours'
		   AND risk_evaluated_at < last_seen_at + ` + staleAfterInterval + `
		 ORDER BY tag_id LIMIT $2)`

// riskPickAgingSQL is the hourly backstop: the oldest evaluations older than an hour.
var riskPickAgingSQL = `
		SELECT tag_id FROM public.herd_signal_tag_latest
		WHERE tenant_id = $1 AND risk_evaluated_at IS NOT NULL
		  AND risk_evaluated_at < now() - interval '1 hour'
		ORDER BY risk_evaluated_at, tag_id LIMIT $2`

// riskScoreSQL (%s = pick query) scores the picked batch in ONE set-based read, without row
// locks. It is the classifier: a line-for-line SQL form of app.applyRiskSignals (proved equal by
// TestPersistedRiskMatchesInMemoryClassification) over the stored pen medians
// (herd_signal_pen_medians, pen = shed + the partition the animal resides in) and each tag's
// 24h p75 own baseline (exactly GetBaselineDeltas' predicate).
// projection-review: membership=the picked batch of herd_signal_tag_latest rows (queue or aging pick, one tenant) and, per tag, its herd_signal_activity_windows in the rolling 24h 300s tier since animal_monitoring_since; group_key=tag_id for the per-tag p75 (LATERAL, one row per tag) and pen key shed_id#partition_key for the stored pen medians; join_cardinality=tagLocationJoin is LATERAL LIMIT 1, goat_shed_partitions joins on its (tenant_id, goat_id) key and herd_signal_pen_medians on its primary key, so each picked tag yields exactly one row; pagination=bounded batch of riskBatchSize tags, never a page-local count shown as a total; scope=tenant_id
var riskScoreSQL = `
	WITH picked AS (%s),
	src AS (
		SELECT tl.tag_id, tl.last_seen_at, tl.motion_delta::float8 AS md, tl.gap_delta AS gap,
		       tl.motion_window_seconds AS mws, tl.tag_temperature_c::float8 AS temp,
		       ` + effectivePatternStateExpr + ` AS pat,
		       (tl.temperature_sensor_ok IS FALSE OR tl.accelerometer_sensor_ok IS FALSE) AS sensor_bad,
		       CASE WHEN tl.mapping_state = 'mapped' AND g.shed_id IS NOT NULL
		            THEN g.shed_id::text || '#' || ` + penPartitionKeyExpr + ` END AS pen,
		       bl.baseline
		FROM picked
		JOIN public.herd_signal_tag_latest tl ON tl.tenant_id = $1 AND tl.tag_id = picked.tag_id
		` + tagLocationJoin + penPartitionJoin + `
		LEFT JOIN LATERAL (
			SELECT percentile_disc(0.75) WITHIN GROUP (ORDER BY w.motion_delta) AS baseline
			FROM public.herd_signal_activity_windows w
			WHERE w.tenant_id = $1 AND w.tag_id = tl.tag_id
			  AND w.bucket_seconds = 300 AND w.packet_count > 0 AND w.gap_delta = false
			  AND w.bucket_start >= now() - interval '24 hours'
			  AND tl.animal_monitoring_since IS NOT NULL
			  AND w.bucket_start >= tl.animal_monitoring_since
		) bl ON true
	),
	calc AS (
		SELECT src.*, pm.shed_id IS NOT NULL AS has_pen, pm.motion_median AS mm, pm.temp_median AS tm,
		       CASE WHEN src.md IS NOT NULL AND src.baseline > 0 AND NOT src.gap
		            THEN (src.md - bw.v) / bw.v * 100 END AS own_pct
		FROM src
		LEFT JOIN public.herd_signal_pen_medians pm
		       ON pm.tenant_id = $1 AND (pm.shed_id::text || '#' || pm.partition_key) = src.pen
		CROSS JOIN LATERAL (
			SELECT CASE WHEN src.mws > 0 THEN src.baseline::float8 * (src.mws::float8 / 300)
			            ELSE src.baseline::float8 * 3 END AS v
		) bw
	),
	parts AS (
		SELECT calc.*,
		       CASE WHEN has_pen AND md IS NOT NULL AND mm > 0 AND NOT gap THEN (md - mm) / mm * 100 END AS group_pct,
		       CASE WHEN has_pen AND temp IS NOT NULL AND tm IS NOT NULL THEN temp - tm END AS temp_delta
		FROM calc
	),
	scored AS (
		SELECT parts.*,
		       CASE WHEN own_pct <= -70 THEN 'motion far below own baseline'
		            WHEN own_pct >= 150 THEN 'motion spike vs own baseline' END AS r_own,
		       CASE WHEN group_pct <= -70 THEN 'motion lower than pen group' END AS r_group,
		       CASE WHEN temp_delta >= 1.5 THEN 'tag temperature high vs pen group' END AS r_temp,
		       CASE WHEN pat IN ('inactive', 'missing') THEN 'persistent abnormal activity'
		            WHEN pat IN ('quiet_watch', 'spike') THEN 'activity pattern needs watch' END AS r_pat,
		       CASE WHEN sensor_bad THEN 'sensor abnormal' END AS r_sensor,
		       (CASE WHEN own_pct <= -70 THEN 2 WHEN own_pct >= 150 THEN 1 ELSE 0 END
		        + CASE WHEN group_pct <= -70 THEN 1 ELSE 0 END
		        + CASE WHEN temp_delta >= 1.5 THEN 1 ELSE 0 END
		        + CASE WHEN pat IN ('inactive', 'missing') THEN 2 WHEN pat IN ('quiet_watch', 'spike') THEN 1 ELSE 0 END
		        + CASE WHEN sensor_bad THEN 1 ELSE 0 END) AS score
		FROM parts
	)
	SELECT tag_id, last_seen_at,
	       CASE WHEN score >= 3 THEN 'high' WHEN score = 2 THEN 'watch' WHEN score = 1 THEN 'low' END,
	       score,
	       CASE WHEN score > 0 THEN array_remove(ARRAY[r_own, r_group, r_temp, r_pat, r_sensor], NULL) ELSE '{}' END,
	       own_pct, group_pct, temp_delta, baseline
	FROM scored
	ORDER BY tag_id
`

// writeTagRiskSQL writes a scored batch set-based with an optimistic guard: a row is written
// only if its last_seen_at is still the one that was scored and nobody holds it (SKIP LOCKED --
// the classifier never waits on ingest). A tag that reported meanwhile is not written and stays
// queued. risk_evaluated_at is set on EVERY written row; changed reports whether the visible
// classification moved.
var writeTagRiskSQL = `
	WITH u AS (
		SELECT * FROM unnest($3::text[], $4::timestamptz[], $5::text[], $6::int[], $7::text[], $8::float8[], $9::float8[], $10::float8[], $11::bigint[])
		       AS u(tag_id, seen, risk_state, risk_score, risk_reasons, own_pct, group_pct, temp_delta, baseline)
	),
	lockable AS (
		SELECT tl.tag_id FROM public.herd_signal_tag_latest tl JOIN u ON u.tag_id = tl.tag_id
		WHERE tl.tenant_id = $1 AND tl.last_seen_at = u.seen
		FOR UPDATE OF tl SKIP LOCKED
	)
	UPDATE public.herd_signal_tag_latest tl
	SET risk_state = u.risk_state,
	    risk_score = u.risk_score,
	    risk_reasons = COALESCE(string_to_array(NULLIF(u.risk_reasons, ''), '|'), '{}'),
	    risk_own_motion_delta_pct = u.own_pct,
	    risk_group_motion_delta_pct = u.group_pct,
	    risk_group_temp_delta_c = u.temp_delta,
	    risk_baseline_delta = u.baseline,
	    risk_evaluated_at = now(),
	    risk_due_at = now() + ` + riskDueOffsetExpr + `
	FROM u
	JOIN public.herd_signal_tag_latest prev ON prev.tenant_id = $1 AND prev.tag_id = u.tag_id
	WHERE tl.tenant_id = $1 AND tl.tag_id = u.tag_id
	  AND tl.tag_id IN (SELECT tag_id FROM lockable)
	  AND tl.last_seen_at = u.seen
	RETURNING (prev.risk_evaluated_at IS NULL
	        OR prev.risk_state IS DISTINCT FROM u.risk_state
	        OR prev.risk_reasons IS DISTINCT FROM COALESCE(string_to_array(NULLIF(u.risk_reasons, ''), '|'), '{}'))
`

func (r *Repository) tryTenantRiskLock(ctx context.Context, tx pgx.Tx, tenantID string) (bool, error) {
	var got bool
	q := sqlbind.MustBind(riskTryLockSQL, riskClassifierLockClass, tenantID)
	err := tx.QueryRow(ctx, q.SQL(), q.Args()...).Scan(&got)
	return got, err
}

// riskBatchScoredHook runs between scoring and writing; tests use it to prove ingest is not
// blocked mid-batch. nil in production.
var riskBatchScoredHook func()

// ClassifyRiskBatch: one short transaction holding only the tenant's advisory xact lock (one
// pool connection, no row locks while scoring): pick + score the batch in one set-based read,
// then write it with the optimistic guard.
func (r *Repository) ClassifyRiskBatch(ctx context.Context, tenantID string, mode ports.RiskBatchMode, limit int, floor time.Duration) (ports.RiskBatchResult, error) {
	var res ports.RiskBatchResult
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return res, fmt.Errorf("begin risk batch: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op once committed
	if res.Locked, err = r.tryTenantRiskLock(ctx, tx, tenantID); err != nil || !res.Locked {
		return res, err
	}
	pick := riskPickQueueSQL
	if mode == ports.RiskBatchAging {
		pick = riskPickAgingSQL
	}
	q := sqlbind.MustBind(fmt.Sprintf(riskScoreSQL, pick), tenantID, limit)
	rows, err := tx.Query(ctx, q.SQL(), q.Args()...)
	if err != nil {
		return res, fmt.Errorf("score risk batch: %w", err)
	}
	var (
		tagIDs, reasons  []string
		seen             []time.Time
		states           []*string
		scores           []int32
		own, group, temp []*float64
		baselines        []*int64
	)
	for rows.Next() {
		var (
			tagID    string
			at       time.Time
			state    *string
			score    int32
			rs       []string
			o, g, tp *float64
			b        *int64
		)
		if err := rows.Scan(&tagID, &at, &state, &score, &rs, &o, &g, &tp, &b); err != nil {
			rows.Close()
			return res, err
		}
		tagIDs, seen, states, scores = append(tagIDs, tagID), append(seen, at), append(states, state), append(scores, score)
		reasons = append(reasons, strings.Join(rs, "|"))
		own, group, temp, baselines = append(own, o), append(group, g), append(temp, tp), append(baselines, b)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return res, err
	}
	res.Picked = len(tagIDs)
	if res.Picked == 0 {
		return res, tx.Commit(ctx)
	}
	if riskBatchScoredHook != nil {
		riskBatchScoredHook()
	}
	w := sqlbind.MustBind(writeTagRiskSQL, tenantID, floor.Seconds(), tagIDs, seen, states, scores, reasons, own, group, temp, baselines)
	wrows, err := tx.Query(ctx, w.SQL(), w.Args()...)
	if err != nil {
		return res, fmt.Errorf("write risk batch: %w", err)
	}
	for wrows.Next() {
		var changed bool
		if err := wrows.Scan(&changed); err != nil {
			wrows.Close()
			return res, err
		}
		res.Processed++
		if changed {
			res.Changed++
		}
	}
	wrows.Close()
	if err := wrows.Err(); err != nil {
		return res, err
	}
	if res.Changed > 0 {
		if err := notifyLiveUpdateTx(ctx, tx, tenantID); err != nil {
			return res, err
		}
	}
	return res, tx.Commit(ctx)
}

const listRiskTenantsSQL = `
	SELECT t.tenant_id::text
	FROM public.tenants t
	WHERE EXISTS (SELECT 1 FROM public.herd_signal_tag_latest tl WHERE tl.tenant_id = t.tenant_id)
`

func (r *Repository) ListRiskTenants(ctx context.Context) ([]string, error) {
	q := sqlbind.MustBind(listRiskTenantsSQL)
	rows, err := r.db.Query(ctx, q.SQL(), q.Args()...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// Pen medians are keyed shed_id#partition_key (oploc.OperationalLocation.Key()); the SQL
// splits the key back into its two stored columns.
const upsertPenMediansSQL = `
	INSERT INTO public.herd_signal_pen_medians (tenant_id, shed_id, partition_key, motion_median, temp_median, computed_at)
	SELECT $1, split_part(u.pen, '#', 1)::uuid, split_part(u.pen, '#', 2), u.motion_median, u.temp_median, $2
	FROM unnest($3::text[], $4::float8[], $5::float8[]) AS u(pen, motion_median, temp_median)
	ON CONFLICT (tenant_id, shed_id, partition_key) DO UPDATE
	SET motion_median = EXCLUDED.motion_median, temp_median = EXCLUDED.temp_median, computed_at = EXCLUDED.computed_at
`

const deletePenMediansSQL = `
	DELETE FROM public.herd_signal_pen_medians
	WHERE tenant_id = $1 AND (shed_id::text || '#' || partition_key) = ANY($2::text[])
`

// queuePenTagsSQL re-queues every tag whose mapped animal is in one of the moved/vanished pens
// (same tag-id-first location join and partition key the medians use).
var queuePenTagsSQL = `
	UPDATE public.herd_signal_tag_latest q
	SET risk_evaluated_at = NULL
	WHERE q.tenant_id = $1 AND q.risk_evaluated_at IS NOT NULL
	  AND q.tag_id IN (
		SELECT tl.tag_id FROM public.herd_signal_tag_latest tl
		` + tagLocationJoin + penPartitionJoin + `
		WHERE tl.tenant_id = $1 AND (g.shed_id::text || '#' || ` + penPartitionKeyExpr + `) = ANY($2::text[])
	  )
`

func (r *Repository) ApplyPenMedians(ctx context.Context, tenantID string, moved map[string]ports.PenMedians, vanished []string, computedAt time.Time) (bool, int, error) {
	if len(moved) == 0 && len(vanished) == 0 {
		return true, 0, nil
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return false, 0, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op once committed
	locked, err := r.tryTenantRiskLock(ctx, tx, tenantID)
	if err != nil || !locked {
		return false, 0, err
	}
	pens := make([]string, 0, len(moved))
	motions := make([]*float64, 0, len(moved))
	temps := make([]*float64, 0, len(moved))
	for pen, m := range moved {
		pens, motions, temps = append(pens, pen), append(motions, m.MotionMedian), append(temps, m.TempMedian)
	}
	if len(pens) > 0 {
		up := sqlbind.MustBind(upsertPenMediansSQL, tenantID, computedAt, pens, motions, temps)
		if _, err := tx.Exec(ctx, up.SQL(), up.Args()...); err != nil {
			return true, 0, fmt.Errorf("upsert pen medians: %w", err)
		}
	}
	if len(vanished) > 0 {
		del := sqlbind.MustBind(deletePenMediansSQL, tenantID, vanished)
		if _, err := tx.Exec(ctx, del.SQL(), del.Args()...); err != nil {
			return true, 0, fmt.Errorf("delete vanished pen medians: %w", err)
		}
	}
	queue := sqlbind.MustBind(queuePenTagsSQL, tenantID, append(append([]string{}, pens...), vanished...))
	tag, err := tx.Exec(ctx, queue.SQL(), queue.Args()...)
	if err != nil {
		return true, 0, fmt.Errorf("queue pen tags: %w", err)
	}
	return true, int(tag.RowsAffected()), tx.Commit(ctx)
}

const loadPenMediansSQL = `
	SELECT shed_id::text || '#' || partition_key, motion_median, temp_median
	FROM public.herd_signal_pen_medians
	WHERE tenant_id = $1
`

// LoadPenMedians reads the classifier's persisted pen medians; found=false when the classifier
// has not stored any for this tenant yet.
func (r *Repository) LoadPenMedians(ctx context.Context, tenantID string) (map[string]ports.PenMedians, bool, error) {
	q := sqlbind.MustBind(loadPenMediansSQL, tenantID)
	rows, err := r.db.Query(ctx, q.SQL(), q.Args()...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	out := map[string]ports.PenMedians{}
	for rows.Next() {
		var pen string
		var m ports.PenMedians
		if err := rows.Scan(&pen, &m.MotionMedian, &m.TempMedian); err != nil {
			return nil, false, err
		}
		out[pen] = m
	}
	return out, len(out) > 0, rows.Err()
}
