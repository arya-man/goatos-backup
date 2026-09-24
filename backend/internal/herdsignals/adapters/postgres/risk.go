package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/vgoats/goatos/backend/internal/herdsignals/ports"
	"github.com/vgoats/goatos/backend/internal/platform/sqlbind"
)

// riskClassifierLockKey is the pg advisory lock that keeps one risk classifier run per cluster.
const riskClassifierLockKey int64 = 0x4853_5249_534b // "HSRISK"

// updateTagRiskSQL writes one classifier batch set-based and only where the classification
// changed, so an unchanged herd costs no row versions and no NOTIFY.
const updateTagRiskSQL = `
	UPDATE public.herd_signal_tag_latest tl
	SET risk_state = u.risk_state,
	    risk_score = u.risk_score,
	    risk_reasons = COALESCE(string_to_array(NULLIF(u.risk_reasons, ''), '|'), '{}'),
	    risk_evaluated_at = $2
	FROM unnest($3::text[], $4::text[], $5::int[], $6::text[]) AS u(tag_id, risk_state, risk_score, risk_reasons)
	WHERE tl.tenant_id = $1 AND tl.tag_id = u.tag_id
	  AND (tl.risk_evaluated_at IS NULL
	       OR tl.risk_state IS DISTINCT FROM u.risk_state
	       OR tl.risk_score IS DISTINCT FROM u.risk_score::smallint
	       OR tl.risk_reasons IS DISTINCT FROM COALESCE(string_to_array(NULLIF(u.risk_reasons, ''), '|'), '{}'))
`

func (r *Repository) UpdateTagRisk(ctx context.Context, tenantID string, rows []ports.TagRisk, evaluatedAt time.Time) (int, error) {
	if len(rows) == 0 {
		return 0, nil
	}
	tagIDs := make([]string, len(rows))
	states := make([]*string, len(rows))
	scores := make([]int32, len(rows))
	reasons := make([]string, len(rows))
	for i, row := range rows {
		tagIDs[i], states[i], scores[i] = row.TagID, row.State, int32(row.Score)
		for j, reason := range row.Reasons {
			if j > 0 {
				reasons[i] += "|"
			}
			reasons[i] += reason
		}
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin risk tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op once committed
	q := sqlbind.MustBind(updateTagRiskSQL, tenantID, evaluatedAt, tagIDs, states, scores, reasons)
	tag, err := tx.Exec(ctx, q.SQL(), q.Args()...)
	if err != nil {
		return 0, fmt.Errorf("update tag risk: %w", err)
	}
	changed := int(tag.RowsAffected())
	if changed > 0 {
		if err := notifyLiveUpdateTx(ctx, tx, tenantID); err != nil {
			return 0, err
		}
	}
	return changed, tx.Commit(ctx)
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

// WithRiskClassifierLock holds a session advisory lock on one dedicated pool connection for the
// duration of fn (fn itself uses the pool normally).
func (r *Repository) WithRiskClassifierLock(ctx context.Context, fn func(ctx context.Context) error) (bool, error) {
	conn, err := r.db.Acquire(ctx)
	if err != nil {
		return false, err
	}
	defer conn.Release()
	var got bool
	q := sqlbind.MustBind(`SELECT pg_try_advisory_lock($1)`, riskClassifierLockKey)
	if err := conn.QueryRow(ctx, q.SQL(), q.Args()...).Scan(&got); err != nil {
		return false, err
	}
	if !got {
		return false, nil
	}
	defer func() {
		uctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		u := sqlbind.MustBind(`SELECT pg_advisory_unlock($1)`, riskClassifierLockKey)
		_, _ = conn.Exec(uctx, u.SQL(), u.Args()...)
	}()
	return true, fn(ctx)
}

const upsertPenMediansSQL = `
	INSERT INTO public.herd_signal_pen_medians (tenant_id, shed_id, motion_median, temp_median, computed_at)
	SELECT $1, u.shed_id, u.motion_median, u.temp_median, $2
	FROM unnest($3::uuid[], $4::float8[], $5::float8[]) AS u(shed_id, motion_median, temp_median)
	ON CONFLICT (tenant_id, shed_id) DO UPDATE
	SET motion_median = EXCLUDED.motion_median, temp_median = EXCLUDED.temp_median, computed_at = EXCLUDED.computed_at
`

const deleteVanishedPenMediansSQL = `
	DELETE FROM public.herd_signal_pen_medians
	WHERE tenant_id = $1 AND NOT (shed_id = ANY($2::uuid[]))
`

// ReplacePenMedians upserts this pass's pen medians and deletes only pens no longer present.
func (r *Repository) ReplacePenMedians(ctx context.Context, tenantID string, medians map[string]ports.PenMedians, computedAt time.Time) error {
	pens := make([]string, 0, len(medians))
	motions := make([]*float64, 0, len(medians))
	temps := make([]*float64, 0, len(medians))
	for pen, m := range medians {
		pens, motions, temps = append(pens, pen), append(motions, m.MotionMedian), append(temps, m.TempMedian)
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op once committed
	up := sqlbind.MustBind(upsertPenMediansSQL, tenantID, computedAt, pens, motions, temps)
	if _, err := tx.Exec(ctx, up.SQL(), up.Args()...); err != nil {
		return fmt.Errorf("upsert pen medians: %w", err)
	}
	del := sqlbind.MustBind(deleteVanishedPenMediansSQL, tenantID, pens)
	if _, err := tx.Exec(ctx, del.SQL(), del.Args()...); err != nil {
		return fmt.Errorf("delete vanished pen medians: %w", err)
	}
	return tx.Commit(ctx)
}

const loadPenMediansSQL = `
	SELECT shed_id::text, motion_median, temp_median
	FROM public.herd_signal_pen_medians
	WHERE tenant_id = $1
`

// LoadPenMedians reads the classifier's persisted pen medians; found=false when the classifier
// has not run for this tenant yet.
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
