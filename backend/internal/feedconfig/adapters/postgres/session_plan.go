package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/feedconfig/domain"
	"github.com/vgoats/goatos/backend/internal/feedconfig/ports"
)

// Session plan SQL (package-level so query-plan tests and the scale guard can reach it). Every
// statement is scoped to one (tenant, park): a park runs a handful of sessions.
const (
	sessionPlanReadSQL = `
WITH locked AS (
  SELECT session_no, session_label, split_fraction, status
  FROM feed_session_templates
  WHERE tenant_id = $1::uuid AND park_id = $2::uuid
  FOR UPDATE
), wanted AS (
  SELECT * FROM unnest($3::int[], $4::text[], $5::numeric[]) AS w(session_no, session_label, split_fraction)
)
SELECT
  (SELECT count(*) FROM locked WHERE status = 'active')::int,
  NOT EXISTS (
    SELECT session_no, session_label, split_fraction FROM locked WHERE status = 'active'
    EXCEPT SELECT session_no, session_label, split_fraction FROM wanted
  ) AND NOT EXISTS (
    SELECT session_no, session_label, split_fraction FROM wanted
    EXCEPT SELECT session_no, session_label, split_fraction FROM locked WHERE status = 'active'
  )`
	sessionPlanFirstIDSQL = `
SELECT session_template_id::text FROM feed_session_templates
WHERE tenant_id = $1::uuid AND park_id = $2::uuid AND status = 'active'
ORDER BY session_no LIMIT 1`
	sessionPlanServingFeedsSQL = `
SELECT min(i.session_no)
FROM feed_session_template_items i
JOIN feed_session_templates t
  ON t.tenant_id = i.tenant_id AND t.park_id = i.park_id AND t.session_no = i.session_no
WHERE i.tenant_id = $1::uuid AND i.park_id = $2::uuid
  AND i.status = 'active' AND i.valid_to IS NULL
  AND t.status = 'active'
  AND NOT (i.session_no = ANY ($3::int[]))`
	sessionPlanUpsertSQL = `
INSERT INTO feed_session_templates (tenant_id, park_id, session_no, session_label, split_fraction, display_order, status)
SELECT $1::uuid, $2::uuid, w.session_no, w.session_label, w.split_fraction, w.session_no, 'active'
FROM unnest($3::int[], $4::text[], $5::numeric[]) AS w(session_no, session_label, split_fraction)
ON CONFLICT (tenant_id, park_id, session_no) DO UPDATE
SET session_label = EXCLUDED.session_label,
    split_fraction = EXCLUDED.split_fraction,
    status = 'active',
    updated_at = now()`
	sessionPlanRetireSQL = `
UPDATE feed_session_templates
SET status = 'retired', updated_at = now()
WHERE tenant_id = $1::uuid AND park_id = $2::uuid AND status = 'active'
  AND NOT (session_no = ANY ($3::int[]))`
)

// SetSessionPlan replaces a park's active feeding sessions with cmd.Sessions (see ports).
//
// One transaction: park check, refusal of any retirement that would orphan declared feeds, then
// one set-based upsert of the listed sessions and one set-based retire of the unlisted ones. The
// outcome is `inserted` when the park had no active session before (a new park being set up),
// `unchanged` when the plan equals what is in force, else `corrected`.
func (r *Repository) SetSessionPlan(ctx context.Context, cmd domain.SetSessionPlanCommand) (domain.WriteResult, error) {
	return r.runWrite(ctx, domain.WriteKindSessionTemplate, cmd.WriteIdentity, func(ctx context.Context, tx pgx.Tx) (writeEffect, error) {
		if err := requireLocation(ctx, tx, cmd.TenantID, cmd.ParkID, "park", ports.ErrParkNotFound); err != nil {
			return writeEffect{}, err
		}
		nos := make([]int32, 0, len(cmd.Sessions))
		labels := make([]string, 0, len(cmd.Sessions))
		splits := make([]string, 0, len(cmd.Sessions))
		for _, s := range cmd.Sessions {
			nos = append(nos, s.SessionNo)
			labels = append(labels, s.Label)
			splits = append(splits, s.SplitFraction)
		}

		// Lock the park's session rows so two editors cannot interleave a plan.
		var activeBefore int
		var unchanged bool
		if err := tx.QueryRow(ctx, sessionPlanReadSQL, cmd.TenantID, cmd.ParkID, nos, labels, splits).Scan(&activeBefore, &unchanged); err != nil {
			return writeEffect{}, fmt.Errorf("feedconfig: read session plan: %w", err)
		}

		var firstID string
		if unchanged {
			if err := tx.QueryRow(ctx, sessionPlanFirstIDSQL, cmd.TenantID, cmd.ParkID).Scan(&firstID); err != nil {
				return writeEffect{}, fmt.Errorf("feedconfig: read session plan id: %w", err)
			}
			return writeEffect{Outcome: domain.OutcomeUnchanged, ResultRowID: firstID}, nil
		}

		// A session about to be retired must not still serve a feed.
		var servingNo *int32
		if err := tx.QueryRow(ctx, sessionPlanServingFeedsSQL, cmd.TenantID, cmd.ParkID, nos).Scan(&servingNo); err != nil {
			return writeEffect{}, fmt.Errorf("feedconfig: check session feeds: %w", err)
		}
		if servingNo != nil {
			return writeEffect{}, fmt.Errorf("%w: session %d", ports.ErrSessionHasFeeds, *servingNo)
		}

		if _, err := tx.Exec(ctx, sessionPlanUpsertSQL, cmd.TenantID, cmd.ParkID, nos, labels, splits); err != nil {
			return writeEffect{}, fmt.Errorf("feedconfig: upsert sessions: %w", err)
		}
		if _, err := tx.Exec(ctx, sessionPlanRetireSQL, cmd.TenantID, cmd.ParkID, nos); err != nil {
			return writeEffect{}, fmt.Errorf("feedconfig: retire sessions: %w", err)
		}
		if err := tx.QueryRow(ctx, sessionPlanFirstIDSQL, cmd.TenantID, cmd.ParkID).Scan(&firstID); err != nil {
			return writeEffect{}, fmt.Errorf("feedconfig: read session plan id: %w", err)
		}
		outcome := domain.OutcomeCorrected
		if activeBefore == 0 {
			outcome = domain.OutcomeInserted
		}
		return writeEffect{Outcome: outcome, ResultRowID: firstID}, nil
	})
}
