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
	// scale-guard:plan-proof-exempt: bounded by one birth_event_id; goat_births_event_child_unique returns <=3 kids before the goat/event probes.
	// litterKidsSQL lists the litter's kids with their CURRENT stage. A rejected birth's kids are
	// not a litter anyone owes a move (its workflow is canceled by the rejection consumer).
	// stage_since is the latest goat.stage_changed naming the kid's CURRENT stage: one LATERAL
	// probe per kid on goat_identity_events_goat_timeline_idx (goat_id, occurred_at DESC), LIMIT 1,
	// so the join stays 1:0..1 and the read stays bounded by the litter (<= 3 kids).
	litterKidsSQL = `
SELECT gb.child_goat_id::text, COALESCE(g.management_stage, ''),
       (g.lifecycle_status = 'alive' AND g.merged_into_goat_id IS NULL),
       since.occurred_at
FROM goat_births gb
JOIN goats g ON g.tenant_id = gb.tenant_id AND g.goat_id = gb.child_goat_id
LEFT JOIN LATERAL (
  SELECT e.occurred_at FROM goat_identity_events e
  WHERE e.goat_id = g.goat_id AND e.tenant_id = g.tenant_id
    AND e.event_type = 'goat.stage_changed'
    AND e.payload->>'management_stage' = g.management_stage
  ORDER BY e.occurred_at DESC
  LIMIT 1
) since ON true
WHERE gb.tenant_id = $1::uuid AND gb.birth_event_id = $2::uuid
  AND gb.count_status <> 'rejected'
ORDER BY gb.child_ordinal`
)

// scale-guard:plan-proof-exempt: bounded by one birth_event_id; active RFID lookup runs once per <=3 litter kids.
// litterKidViewsSQL is the litter detail read: each kid's RFID (the LATERAL pre-selects ONE
// active animal identifier, keeping the join 1:0..1), stage, and pen (locations by primary key,
// goat_shed_partitions by (tenant, goat)). Bounded by one litter (<= 3 rows).
const litterKidViewsSQL = `
SELECT gb.child_goat_id::text,
       COALESCE(rfid.identifier_value, g.display_id, ''),
       COALESCE(g.management_stage, ''),
       COALESCE(shed.name, ''), COALESCE(gsp.partition_label, ''),
       (g.lifecycle_status = 'alive' AND g.merged_into_goat_id IS NULL),
       COALESCE(g.sex, ''), COALESCE(g.breed, ''), COALESCE(g.age_band, '')
FROM goat_births gb
JOIN goats g ON g.tenant_id = gb.tenant_id AND g.goat_id = gb.child_goat_id
-- The tag the farm can READ on the kid: its permanent RFID, else the provisional birth tag
-- (CBE-38085) a kid wears until "Tag the kid", and only then the internal display id.
LEFT JOIN LATERAL (
  SELECT gi.identifier_value FROM goat_identifiers gi
  WHERE gi.tenant_id = g.tenant_id AND gi.goat_id = g.goat_id AND gi.status = 'active'
    AND gi.identifier_type IN ('animal_identifier_1', 'animal_identifier_2', 'temporary_tag')
  ORDER BY CASE gi.identifier_type WHEN 'animal_identifier_1' THEN 0 WHEN 'animal_identifier_2' THEN 1 ELSE 2 END
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
		if err := rows.Scan(&k.GoatID, &k.Tag, &k.Stage, &shed, &partition, &k.Alive, &k.Sex, &k.Breed, &k.AgeBand); err != nil {
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
		if err := rows.Scan(&k.GoatID, &k.Stage, &k.Alive, &k.StageSince); err != nil {
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

// LitterOwingShift is one litter the backfill opens a shift workflow for: its birth event and one
// of its live kids (the opener reads the birth moment and placement from that kid).
type LitterOwingShift struct {
	BirthEventID string
	KidGoatID    string
}

// scale-guard:plan-proof-exempt: keyset-paged by birth_event_id with LIMIT; goat rows are probed from the litter child index.
// littersOwingShiftSQL lists, keyset-paged on birth_event_id, the recorded litters that still hold
// a LIVE kid on one of $2's stages and have NO litter workflow yet. goat_births is read through
// goat_births_event_child_unique (tenant_id, birth_event_id, child_ordinal); goats and
// workflow_instances_subject_ref_uq are probed by key. A rejected birth is not a litter.
const littersOwingShiftSQL = `
SELECT gb.birth_event_id::text, min(gb.child_goat_id::text)
FROM goat_births gb
JOIN goats g ON g.tenant_id = gb.tenant_id AND g.goat_id = gb.child_goat_id
WHERE gb.tenant_id = $1::uuid
  AND gb.birth_event_id IS NOT NULL
  AND gb.count_status <> 'rejected'
  AND g.lifecycle_status = 'alive' AND g.merged_into_goat_id IS NULL
  AND g.management_stage = ANY($2::text[])
  AND ($3::text = '' OR gb.birth_event_id > nullif($3::text, '')::uuid)
  AND NOT EXISTS (
    SELECT 1 FROM workflow_instances wi
    WHERE wi.tenant_id = gb.tenant_id AND wi.template_key = 'birth_litter'
      AND wi.subject_ref_id = gb.birth_event_id)
GROUP BY gb.birth_event_id
ORDER BY gb.birth_event_id
LIMIT $4`

// LittersOwingShift is one keyset page of the backfill's candidates (after = last birth event
// id of the previous page, "" for the first).
func (r *Repository) LittersOwingShift(ctx context.Context, tenantID string, stages []string, after string, limit int) ([]LitterOwingShift, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	bound := sqlbind.MustBind(littersOwingShiftSQL, tenantID, stages, after, limit)
	rows, err := r.pool.Query(ctx, bound.SQL(), bound.Args()...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LitterOwingShift
	for rows.Next() {
		var l LitterOwingShift
		if err := rows.Scan(&l.BirthEventID, &l.KidGoatID); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
