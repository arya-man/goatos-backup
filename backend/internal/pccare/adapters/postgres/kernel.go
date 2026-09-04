package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// PC Care kernel roll-forward (weighing 000059 semantics, minimal form): an unfinished task
// whose due_business_date has passed rolls FORWARD to today and is marked 'delayed', so the
// operator's worklist for today still carries yesterday's unfinished pen and the delay is
// visible (planned_business_date stays immutable; delayed_since_business_date records when the
// slide began; rolled_forward_count counts the slides).
//
// Bounded and chunked: each pass claims up to chunkSize rows via FOR UPDATE SKIP LOCKED
// (covered by pc_care_tasks_sweep_due_idx) at most maxChunks times, so a tick can never scan
// unbounded state and two concurrent workers never fight over one row.

// SweepTaskRollForwardResult reports one sweep pass.
type SweepTaskRollForwardResult struct {
	RolledForward int
	// HeldForRemoval counts dewormings pushed to TOMORROW because their linked feed & water
	// removal task was never submitted (the midnight gate, maintainer decision 2026-09-03).
	HeldForRemoval int
	Truncated      bool
}

// SweepTaskRollForward rolls every overdue open task's due date to the current Asia/Kolkata
// business day. Idempotent: a task already due today matches nothing.
func (r *Repository) SweepTaskRollForward(ctx context.Context, tenantID string, asOf time.Time, chunkSize, maxChunks int) (SweepTaskRollForwardResult, error) {
	if chunkSize < 1 {
		chunkSize = 200
	}
	if maxChunks < 1 {
		maxChunks = 50
	}
	today := biztime.BusinessDate(asOf)

	result := SweepTaskRollForwardResult{}
	for chunk := 0; chunk < maxChunks; chunk++ {
		ctxChunk, cancel := r.timeout(ctx)
		// scale-guard:ignore: bounded chunked claim over pc_care_tasks_sweep_due_idx (tenant_id, work_state, due_business_date, task_id) with FOR UPDATE SKIP LOCKED; each pass touches at most chunkSize rows and the loop is capped at maxChunks.
		tag, err := r.pool.Exec(ctxChunk, `
UPDATE pc_care_tasks t
SET work_state = 'delayed',
    delayed_since_business_date = COALESCE(t.delayed_since_business_date, t.due_business_date),
    due_business_date = $2::date,
    rolled_forward_count = t.rolled_forward_count + 1,
    updated_at = now(),
    row_version = t.row_version + 1
FROM (
  SELECT task_id
  FROM pc_care_tasks
  WHERE tenant_id = $1::uuid
    AND work_state IN ('scheduled', 'delayed')
    AND due_business_date < $2::date
  ORDER BY due_business_date, task_id
  LIMIT $3
  FOR UPDATE SKIP LOCKED
) claim
WHERE t.tenant_id = $1::uuid AND t.task_id = claim.task_id`,
			tenantID, today, chunkSize)
		cancel()
		if err != nil {
			return result, fmt.Errorf("pccare: sweep task roll-forward: %w", err)
		}
		moved := int(tag.RowsAffected())
		result.RolledForward += moved
		if moved < chunkSize {
			held, truncated, err := r.sweepDewormingRemovalGate(ctx, tenantID, today, chunkSize, maxChunks)
			result.HeldForRemoval = held
			result.Truncated = truncated
			return result, err
		}
	}
	result.Truncated = true
	return result, nil
}

// sweepDewormingRemovalGate is the MIDNIGHT GATE (maintainer decision 2026-09-03): a deworming
// whose linked feed & water removal task was never SUBMITTED cannot run — the animals still had
// feed and water overnight — so its due date is pushed to TOMORROW (today + 1, never today) and
// it reads as delayed. It runs AFTER the ordinary roll-forward pass, which has already carried
// every overdue row to today, so the candidates here are exactly the dewormings due today (or
// somehow still earlier) whose gate is unmet. The gate is submitted_at IS NOT NULL EVER: a
// later verifier rework of the removal evidence does NOT re-block the deworming — verification
// is post-hoc review, and the feed really was removed that night.
//
// projection-review: membership=pc_care_tasks deworming rows in ('scheduled','delayed') with
// due_business_date <= today whose linked removal row (removal.gates_task_id = d.task_id, same
// tenant) is live and unsubmitted; group_key=d.task_id (the UPDATE grain);
// join_cardinality=removal 0..1 per deworming — pc_care_tasks_gates_task_uq (tenant_id,
// gates_task_id) WHERE gates_task_id IS NOT NULL makes the removal side unique per deworming,
// so the join cannot multiply claim rows; consumer match columns (tenant_id, gates_task_id)
// equal that index's columns; no aggregate, so no numerator/denominator key set.
// pagination=chunked FOR UPDATE SKIP LOCKED claim like the roll-forward pass above;
// scope=tenant_id.
func (r *Repository) sweepDewormingRemovalGate(ctx context.Context, tenantID, today string, chunkSize, maxChunks int) (held int, truncated bool, err error) {
	for chunk := 0; chunk < maxChunks; chunk++ {
		ctxChunk, cancel := r.timeout(ctx)
		// scale-guard:ignore: bounded chunked claim over pc_care_tasks_sweep_due_idx narrowed by the pc_care_tasks_gates_task_uq join, FOR UPDATE SKIP LOCKED, at most chunkSize rows per pass and maxChunks passes.
		tag, execErr := r.pool.Exec(ctxChunk, `
UPDATE pc_care_tasks t
SET work_state = 'delayed',
    delayed_since_business_date = COALESCE(t.delayed_since_business_date, t.due_business_date),
    due_business_date = $2::date + 1,
    rolled_forward_count = t.rolled_forward_count + 1,
    updated_at = now(),
    row_version = t.row_version + 1
FROM (
  SELECT d.task_id
  FROM pc_care_tasks d
  JOIN pc_care_tasks removal
    ON removal.tenant_id = d.tenant_id AND removal.gates_task_id = d.task_id
  WHERE d.tenant_id = $1::uuid
    AND d.category = 'deworming'
    AND d.work_state IN ('scheduled', 'delayed')
    AND d.due_business_date <= $2::date
    AND (
      removal.submitted_at IS NULL
      OR (removal.submitted_at AT TIME ZONE 'Asia/Kolkata')::date >= d.due_business_date
    )
    AND removal.work_state <> 'canceled'
  ORDER BY d.due_business_date, d.task_id
  LIMIT $3
  FOR UPDATE OF d, removal SKIP LOCKED
) claim
WHERE t.tenant_id = $1::uuid AND t.task_id = claim.task_id`,
			tenantID, today, chunkSize)
		cancel()
		if execErr != nil {
			return held, false, fmt.Errorf("pccare: sweep deworming removal gate: %w", execErr)
		}
		moved := int(tag.RowsAffected())
		held += moved
		if moved < chunkSize {
			return held, false, nil
		}
	}
	return held, true, nil
}
