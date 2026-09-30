package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/vgoats/goatos/backend/internal/platform/sqlbind"
	"github.com/vgoats/goatos/backend/internal/tasks/domain"
)

// KID STAGE SHIFT TASKS (maintainer decision 2026-09-30, docs/decisions/kid-stage-shift-tasks.md).
// The litter workflow's shift steps are completed from the herd register, never by a tap. These
// reads are bounded by ONE litter: goat_births_event_child_unique (tenant, birth_event_id,
// child_ordinal) holds at most three kids, each joined 1:1 to its goats row by primary key.
const (
	// litterOfChildSQL resolves the litter a kid belongs to (goat_births_tenant_child_unique).
	litterOfChildSQL = `
SELECT birth_event_id::text FROM goat_births
WHERE tenant_id = $1::uuid AND child_goat_id = $2::uuid AND birth_event_id IS NOT NULL`
	// litterKidsSQL lists the litter's kids with their CURRENT stage. A rejected birth's kids are
	// not a litter anyone owes a move (its workflow is canceled by the rejection consumer).
	litterKidsSQL = `
SELECT gb.child_goat_id::text, COALESCE(g.management_stage, ''),
       (g.lifecycle_status = 'alive' AND g.merged_into_goat_id IS NULL)
FROM goat_births gb
JOIN goats g ON g.tenant_id = gb.tenant_id AND g.goat_id = gb.child_goat_id
WHERE gb.tenant_id = $1::uuid AND gb.birth_event_id = $2::uuid
  AND gb.count_status <> 'rejected'
ORDER BY gb.child_ordinal`
)

// litterKidViewsSQL is the litter detail read: each kid's RFID (the LATERAL pre-selects ONE
// active animal identifier, keeping the join 1:0..1), stage, and pen (locations by primary key,
// goat_shed_partitions by (tenant, goat)). Bounded by one litter (<= 3 rows).
const litterKidViewsSQL = `
SELECT gb.child_goat_id::text,
       COALESCE(rfid.identifier_value, g.display_id, ''),
       COALESCE(g.management_stage, ''),
       COALESCE(shed.name, ''), COALESCE(gsp.partition_label, ''),
       (g.lifecycle_status = 'alive' AND g.merged_into_goat_id IS NULL)
FROM goat_births gb
JOIN goats g ON g.tenant_id = gb.tenant_id AND g.goat_id = gb.child_goat_id
LEFT JOIN LATERAL (
  SELECT gi.identifier_value FROM goat_identifiers gi
  WHERE gi.tenant_id = g.tenant_id AND gi.goat_id = g.goat_id AND gi.status = 'active'
    AND gi.identifier_type IN ('animal_identifier_1', 'animal_identifier_2')
  ORDER BY CASE gi.identifier_type WHEN 'animal_identifier_1' THEN 0 ELSE 1 END
  LIMIT 1
) rfid ON true
LEFT JOIN locations shed ON shed.tenant_id = g.tenant_id AND shed.location_id = g.shed_id
LEFT JOIN goat_shed_partitions gsp ON gsp.tenant_id = g.tenant_id AND gsp.goat_id = g.goat_id
WHERE gb.tenant_id = $1::uuid AND gb.birth_event_id = $2::uuid
  AND gb.count_status <> 'rejected'
ORDER BY gb.child_ordinal`

// litterKidViews reads the litter detail's kids.
func (r *Repository) litterKidViews(ctx context.Context, tenantID, birthEventID string) ([]domain.LitterKidView, error) {
	bound := sqlbind.MustBind(litterKidViewsSQL, tenantID, birthEventID)
	rows, err := r.pool.Query(ctx, bound.SQL(), bound.Args()...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.LitterKidView
	for rows.Next() {
		var k domain.LitterKidView
		var shed, partition string
		if err := rows.Scan(&k.GoatID, &k.Tag, &k.Stage, &shed, &partition, &k.Alive); err != nil {
			return nil, err
		}
		k.PenLabel = cardPenLabel(shed, partition)
		out = append(out, k)
	}
	return out, rows.Err()
}

// LitterOfChild returns the birth event (litter) a kid was born in, or domain.ErrNotFound.
func (r *Repository) LitterOfChild(ctx context.Context, tenantID, goatID string) (string, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	bound := sqlbind.MustBind(litterOfChildSQL, tenantID, goatID)
	var eventID string
	err := r.pool.QueryRow(ctx, bound.SQL(), bound.Args()...).Scan(&eventID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", domain.ErrNotFound
	}
	return eventID, err
}

// LitterKids lists a litter's kids and their current stage (<= 3 rows).
func (r *Repository) LitterKids(ctx context.Context, tenantID, birthEventID string) ([]domain.LitterKid, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	return litterKids(ctx, r.pool, tenantID, birthEventID)
}

func litterKids(ctx context.Context, q queryer, tenantID, birthEventID string) ([]domain.LitterKid, error) {
	bound := sqlbind.MustBind(litterKidsSQL, tenantID, birthEventID)
	rows, err := q.Query(ctx, bound.SQL(), bound.Args()...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.LitterKid
	for rows.Next() {
		var k domain.LitterKid
		if err := rows.Scan(&k.GoatID, &k.Stage, &k.Alive); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// ReconcileLitterShiftSteps applies the herd register to a litter workflow's shift steps
// (domain.ApplyLitterShift) inside the shared workflow write transaction, so the steps, their
// dependents' due times and the card never disagree. The kids are read UNDER that transaction.
// A litter with no workflow (a birth recorded before the rule, or a Birth SOP without the litter
// track) is a no-op.
func (r *Repository) ReconcileLitterShiftSteps(ctx context.Context, tenantID, birthEventID string, at time.Time) error {
	workflowID, err := r.WorkflowIDBySubjectRef(ctx, tenantID, domain.TemplateKeyBirthLitter, birthEventID)
	if errors.Is(err, domain.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	_, _, _, err = r.workflowMutation(ctx, tenantID, workflowID,
		func(tx pgx.Tx, w *domain.WorkflowInstance, actions []domain.WorkflowAction) ([]domain.WorkflowAction, bool, error) {
			if w.State == domain.WorkflowStateCanceled {
				return nil, true, nil
			}
			kids, err := litterKids(ctx, tx, tenantID, birthEventID)
			if err != nil {
				return nil, false, err
			}
			changed, cancelWorkflow := domain.ApplyLitterShift(actions, kids, at)
			if cancelWorkflow {
				w.State = domain.WorkflowStateCanceled
			}
			if len(changed) == 0 && !cancelWorkflow {
				return nil, true, nil
			}
			return changed, false, nil
		})
	return err
}
