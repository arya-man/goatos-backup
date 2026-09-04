package postgres

import (
	"context"

	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

// THE MIDNIGHT FASTING GATE (maintainer decision 2026-09-03, domain/fasting.go).
//
// A weighing whose feed & water removal was NOT submitted before 00:00 IST of
// the weigh date must not run that day: the animals ate overnight and the
// weights would be wrong. Two coupled sweeps, both bounded and chunked like
// every other cadence pass:
//
//  1. gate pass — every OPEN work item due today-or-earlier whose campaign has
//     a fasting row with submitted_at IS NULL is pushed to TOMORROW (never
//     today). Runs BEFORE the generic roll-forward pass, so a stale gated item
//     goes straight to tomorrow instead of being pulled to today first.
//  2. fasting roll — the unsubmitted fasting rows themselves move their
//     weigh_business_date to tomorrow, re-arming the operator's card for the
//     next evening's 20:00 window. Separate from the gate pass because a
//     not-yet-published campaign has NO work items yet and its fasting row
//     still has to re-arm.
//
// The gate reads submitted_at, never status: a fasting task in verifier rework
// was still SUBMITTED before midnight and does not re-block the weighing.
// Campaigns with no fasting row (pre-feature) match nothing and behave exactly
// as before.
//
// projection-review: gate pass — producer weighing_work_items unique
// (work_item_id) PK; consumer claim matches (wi.tenant_id, wi.campaign_id)
// against weighing_fasting_tasks, whose UNIQUE (tenant_id, campaign_id)
// (migration 000252) makes the join AT MOST 1:1 per work item, so the claim
// can never multiply rows; group_key none (row-grain update); the compared
// key set of the two UPDATE halves is identical (tenant_id + campaign_id).
// fastingGateClaimSQL pushes every OPEN work item of a fasting-unsubmitted
// campaign to TOMORROW ($2::date + 1). Package-level so a query-plan test and
// the scale guard reach it.
const fastingGateClaimSQL = `
WITH claimed AS (
  SELECT wi.work_item_id, wi.due_business_date AS sort_date
  FROM weighing_work_items wi
  JOIN weighing_fasting_tasks ft
    ON ft.tenant_id = wi.tenant_id AND ft.campaign_id = wi.campaign_id
  WHERE wi.tenant_id = $1::uuid
    AND wi.work_state IN ('scheduled','delayed')
    AND wi.due_business_date <= $2::date
    AND ft.submitted_at IS NULL
    AND (wi.due_business_date > $3::date OR (wi.due_business_date = $3::date AND wi.work_item_id > $4::uuid))
  ORDER BY wi.due_business_date, wi.work_item_id
  LIMIT $5
  FOR UPDATE OF wi SKIP LOCKED
)
UPDATE weighing_work_items wi
SET due_business_date = ($2::date + 1),
    rolled_forward_count = wi.rolled_forward_count + 1,
    last_rolled_forward_on = $2::date,
    updated_at = now()
FROM claimed
WHERE wi.work_item_id = claimed.work_item_id
RETURNING wi.work_item_id::text, wi.campaign_id::text, wi.campaign_shed_id::text,
          wi.park_id::text, wi.operator_user_id::text, wi.shed_label,
          wi.shed_location_id::text, wi.planned_business_date::text, wi.due_business_date::text,
          claimed.sort_date::text`

// fastingRollSQL re-arms one chunk of unsubmitted, deadline-passed fasting
// rows for tomorrow evening; the campaign join excludes ended tasks.
const fastingRollSQL = `
WITH claimed AS (
  SELECT ft.fasting_task_id
  FROM weighing_fasting_tasks ft
  JOIN weighing_campaigns c
    ON c.tenant_id = ft.tenant_id AND c.campaign_id = ft.campaign_id
  WHERE ft.tenant_id = $1::uuid
    AND ft.submitted_at IS NULL
    AND ft.weigh_business_date <= $2::date
    AND c.status NOT IN ('completed','closed','canceled')
  ORDER BY ft.weigh_business_date, ft.fasting_task_id
  LIMIT $3
  FOR UPDATE OF ft SKIP LOCKED
),
rolled AS (
  UPDATE weighing_fasting_tasks ft
  SET weigh_business_date = ($2::date + 1),
      rolled_forward_count = ft.rolled_forward_count + 1,
      row_version = ft.row_version + 1,
      updated_at = now()
  FROM claimed
  WHERE ft.fasting_task_id = claimed.fasting_task_id
  RETURNING 1
)
SELECT COUNT(*)::int FROM rolled`

func (r *Repository) sweepFastingGate(ctx context.Context, tenantID, businessDate string, chunk, maxChunks int) (gated, rolledTasks, events int, truncated bool, err error) {
	gated, events, truncated, err = r.runCadencePass(ctx, cadencePass{
		tenantID:     tenantID,
		businessDate: businessDate,
		chunk:        chunk,
		maxChunks:    maxChunks,
		eventType:    domain.EventWorkItemRolledForward,
		claimSQL:     fastingGateClaimSQL,
	})
	if err != nil {
		return gated, 0, events, truncated, err
	}

	// Re-arm the unsubmitted fasting rows for tomorrow evening. Chunked with
	// SKIP LOCKED so a concurrent submit (which locks its row FOR UPDATE) is
	// simply skipped this tick and settled by its own commit.
	for i := 0; i < maxChunks; i++ {
		var moved int
		// scale-guard:ignore: bounded chunked FOR UPDATE SKIP LOCKED cadence sweep; each iteration claims one chunk and the loop terminates when a chunk comes back short — the same shape as every runCadencePass tick
		err = r.pool.QueryRow(ctx, fastingRollSQL, tenantID, businessDate, chunk).Scan(&moved)
		if err != nil {
			return gated, rolledTasks, events, truncated, err
		}
		rolledTasks += moved
		if moved < chunk {
			return gated, rolledTasks, events, truncated, nil
		}
	}
	return gated, rolledTasks, events, true, nil
}
