package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/herdsignals/domain"
	"github.com/vgoats/goatos/backend/internal/herdsignals/ports"
	"github.com/vgoats/goatos/backend/internal/platform/sqlbind"
)

// riskClassifierLockClass is the first key of the per-tenant classifier advisory xact lock
// (the second is hashtext(tenant_id)). Transaction-scoped: it can never outlive its batch or
// be returned to the pool held.
const riskClassifierLockClass int32 = 0x48535249 // "HSRI"

const riskTryLockSQL = `SELECT pg_try_advisory_xact_lock($1::int4, hashtext($2::text))`

// riskMinReevaluation is the minimum interval between two classifications of one tag driven by
// new packets alone: a live tag reports every few seconds, so without it every tick would
// re-classify the whole herd. Pen-median moves and staleness still re-queue immediately.
// Jittered to 2.5-7.5 min so a herd classified together (first pass, pen move) spreads out
// instead of coming due as one wave every 5 minutes.
const riskMinReevaluation = "(interval '150 seconds' + random() * interval '300 seconds')"

// riskQueueBatchSQL picks the change-driven work: never classified, queued, or reporting again
// after risk_due_at (herd_signal_tag_latest_risk_dirty_idx), plus tags that went stale since their last
// evaluation (their pattern turned 'missing' without any new packet; bounded to the last 3h of
// last_seen via the (tenant_id, last_seen_at) index).
var riskQueueBatchSQL = `
	WITH picked AS (
		(SELECT tag_id FROM public.herd_signal_tag_latest
		 WHERE tenant_id = $1 AND (risk_evaluated_at IS NULL OR last_seen_at > risk_due_at)
		 ORDER BY tag_id LIMIT $2)
		UNION
		(SELECT tag_id FROM public.herd_signal_tag_latest
		 WHERE tenant_id = $1
		   AND last_seen_at < now() - ` + staleAfterInterval + `
		   AND last_seen_at > now() - interval '3 hours'
		   AND risk_evaluated_at < last_seen_at + ` + staleAfterInterval + `
		 LIMIT $2)
	)
	SELECT ` + tagLatestColumns + `
	FROM public.herd_signal_tag_latest tl
	WHERE tl.tenant_id = $1 AND tl.tag_id IN (SELECT tag_id FROM picked)
	ORDER BY tl.tag_id
	LIMIT $2
	FOR UPDATE OF tl SKIP LOCKED
`

// riskAgingBatchSQL is the hourly backstop: the oldest evaluations older than an hour
// (herd_signal_tag_latest_risk_evaluated_idx), catching slow drifts such as the 24h baseline.
var riskAgingBatchSQL = `
	SELECT ` + tagLatestColumns + `
	FROM public.herd_signal_tag_latest tl
	WHERE tl.tenant_id = $1 AND tl.risk_evaluated_at IS NOT NULL
	  AND tl.risk_evaluated_at < now() - interval '1 hour'
	ORDER BY tl.risk_evaluated_at
	LIMIT $2
	FOR UPDATE OF tl SKIP LOCKED
`

// writeTagRiskSQL writes a whole batch set-based. risk_evaluated_at is set on EVERY evaluated
// row (so it leaves the queue); changed reports whether the visible classification moved.
// risk_evaluated_at = the transaction's start (now()): a packet that lands after the batch was
// read waits on the row lock and then advances last_seen_at past it, re-queueing the tag.
var writeTagRiskSQL = `
	UPDATE public.herd_signal_tag_latest tl
	SET risk_state = u.risk_state,
	    risk_score = u.risk_score,
	    risk_reasons = COALESCE(string_to_array(NULLIF(u.risk_reasons, ''), '|'), '{}'),
	    risk_own_motion_delta_pct = u.own_pct,
	    risk_group_motion_delta_pct = u.group_pct,
	    risk_group_temp_delta_c = u.temp_delta,
	    risk_evaluated_at = now(),
	    risk_due_at = now() + ` + riskMinReevaluation + `
	FROM unnest($2::text[], $3::text[], $4::int[], $5::text[], $6::float8[], $7::float8[], $8::float8[])
	       AS u(tag_id, risk_state, risk_score, risk_reasons, own_pct, group_pct, temp_delta)
	JOIN public.herd_signal_tag_latest prev ON prev.tenant_id = $1 AND prev.tag_id = u.tag_id
	WHERE tl.tenant_id = $1 AND tl.tag_id = u.tag_id
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

func (r *Repository) ClassifyRiskBatch(ctx context.Context, tenantID string, mode ports.RiskBatchMode, limit int, classify func(ctx context.Context, tags []domain.TagLatest) ([]ports.TagRisk, error)) (ports.RiskBatchResult, error) {
	var res ports.RiskBatchResult
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return res, fmt.Errorf("begin risk batch: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op once committed
	if res.Locked, err = r.tryTenantRiskLock(ctx, tx, tenantID); err != nil || !res.Locked {
		return res, err
	}
	pick := riskQueueBatchSQL
	if mode == ports.RiskBatchAging {
		pick = riskAgingBatchSQL
	}
	q := sqlbind.MustBind(pick, tenantID, limit)
	rows, err := tx.Query(ctx, q.SQL(), q.Args()...)
	if err != nil {
		return res, fmt.Errorf("pick risk batch: %w", err)
	}
	tags := make([]domain.TagLatest, 0, limit)
	for rows.Next() {
		tag, err := scanTagLatest(rows)
		if err != nil {
			rows.Close()
			return res, err
		}
		tags = append(tags, tag)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return res, err
	}
	if len(tags) == 0 {
		return res, tx.Commit(ctx)
	}
	risks, err := classify(ctx, tags)
	if err != nil {
		return res, err
	}
	n := len(risks)
	tagIDs, states, reasons := make([]string, n), make([]*string, n), make([]string, n)
	scores := make([]int32, n)
	own, group, temp := make([]*float64, n), make([]*float64, n), make([]*float64, n)
	for i, rk := range risks {
		tagIDs[i], states[i], scores[i] = rk.TagID, rk.State, int32(rk.Score)
		own[i], group[i], temp[i] = rk.OwnMotionPct, rk.GroupMotionPct, rk.GroupTempDeltaC
		for j, reason := range rk.Reasons {
			if j > 0 {
				reasons[i] += "|"
			}
			reasons[i] += reason
		}
	}
	w := sqlbind.MustBind(writeTagRiskSQL, tenantID, tagIDs, states, scores, reasons, own, group, temp)
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
