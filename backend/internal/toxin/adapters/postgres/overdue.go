package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/vgoats/goatos/backend/internal/toxin/domain"
)

// The 12-hour start clock's two reads. They live beside the module's own repository rather than
// with the /feed/toxin report adapter: the deadline is a property of the TASK, read by the phone's
// card and by the shared-cadence reminder, and neither has anything to do with the leadership
// report.

// UnstartedOverdueTasks returns rounds past the 12-hour start deadline that carry NO completed
// step. It backs the shared-cadence reminder, never a screen.
//
// The deadline is applied in SQL off created_at with the caller's clock, so the reminder and the
// card agree on what "late" means (domain.StartDeadline is the one definition of the interval).
// A LEFT JOIN + IS NULL is used rather than NOT EXISTS with a correlated count so the read stays
// one index scan over the small open-round set.
func (r *Repository) UnstartedOverdueTasks(ctx context.Context, tenantID string, now time.Time) ([]domain.Task, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	cutoff := now.Add(-domain.StartDeadline)
	query := fmt.Sprintf(`SELECT %s FROM public.toxin_test_tasks t
  LEFT JOIN public.toxin_test_step_completions c
         ON c.tenant_id = t.tenant_id AND c.task_id = t.task_id
 WHERE t.tenant_id = $1
   AND t.status = $2
   AND t.created_at <= $3
   AND c.task_id IS NULL
 ORDER BY t.created_at ASC, t.task_id ASC
 LIMIT 500`, taskColumns)

	rows, err := r.pool.Query(ctx, query, tenantID, domain.StatusInProgress, cutoff)
	if err != nil {
		return nil, fmt.Errorf("toxin: unstarted overdue tasks: %w", err)
	}
	defer rows.Close()
	out := []domain.Task{}
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, fmt.Errorf("toxin: unstarted overdue tasks scan: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ToxinTesterUserIDs lists who currently holds the tester grant, so the reminder reaches the named
// individuals rather than whoever happens to sit in a director seat. Reading the GRANT is what
// keeps the per-person rule (perPersonGrants) true here.
func (r *Repository) ToxinTesterUserIDs(ctx context.Context, tenantID string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, `SELECT DISTINCT user_id::text
  FROM public.user_scope_grants
 WHERE tenant_id = $1 AND role = $2 AND status = 'active'
   AND (valid_to IS NULL OR valid_to > now())`, tenantID, toxinTesterRole)
	if err != nil {
		return nil, fmt.Errorf("toxin: tester grants: %w", err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("toxin: tester grants scan: %w", err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// toxinTesterRole is the RBAC role string the grant carries. Kept as a literal here rather than
// importing internal/permissions, which would pull the whole route table into the data adapter.
const toxinTesterRole = "toxin_tester"
