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
	Truncated     bool
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
			return result, nil
		}
	}
	result.Truncated = true
	return result, nil
}
