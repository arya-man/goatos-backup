package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/vgoats/goatos/backend/internal/pccare/ports"
)

// PC CARE REPEAT (maintainer instruction 2026-09-30). The SOP card says "repeat every N days";
// the pc-care-repeat stage plans the next task of that work for the same pen(s) and operator(s),
// N days after the last task's PLANNED date, whether or not the last one is finished.

var _ ports.RepeatStore = (*Repository)(nil)

// repeatCandidatesSQL finds, for every configured category, the LATEST live task per pen
// ($1 tenant, $2 categories, $3 intervals in days, $4 horizon date, $5 limit) whose next date
// (planned + N) falls on or before the horizon, that no task was repeated from yet and that was
// not skipped.
//
// projection-review: membership=pc_care_tasks of the tenant in a configured planner category,
// not canceled, pen-grain (shed_id set) and not a removal card; group_key=(category, park_id,
// shed_id, partition_key) -- the natural key of a pen's work, served by
// pc_care_tasks_natural_uq's leading columns -- with DISTINCT ON keeping the latest planned date;
// join_cardinality=cfg is one row per category (unnest of two equal-length arrays); assignees
// and the removal card are pre-aggregated per task in scalar subqueries, so no join fans out a
// candidate; pagination=bounded by LIMIT $5 per tick, ordered by (next date, task id).
//
// scale-guard:ignore: one bounded set-based read per kernel tick over a tenant's PC Care tasks (a few thousand rows at the 5k-50k envelope), LIMIT per tick; the natural-key index serves the DISTINCT ON.
const repeatCandidatesSQL = `
WITH cfg AS (
  SELECT c.category, c.every_days
  FROM unnest($2::text[], $3::int[]) AS c(category, every_days)
),
latest AS (
  SELECT DISTINCT ON (t.category, t.park_id, t.shed_id, t.partition_key)
         t.task_id, t.round_id, t.category, t.park_id, t.shed_id,
         coalesce(t.partition_label, '') AS partition_label,
         t.planned_business_date, t.created_by, cfg.every_days
  FROM pc_care_tasks t
  JOIN cfg ON cfg.category = t.category
  WHERE t.tenant_id = $1::uuid
    AND t.work_state <> 'canceled'
    AND t.shed_id IS NOT NULL
    AND t.gates_round_id IS NULL
    AND t.gates_task_id IS NULL
  ORDER BY t.category, t.park_id, t.shed_id, t.partition_key, t.planned_business_date DESC, t.created_at DESC
)
SELECT l.task_id::text, coalesce(l.round_id::text, ''), l.category, l.park_id::text, coalesce(p.name, ''), l.shed_id::text,
       coalesce(s.name, ''), l.partition_label, l.planned_business_date, l.every_days,
       l.created_by::text,
       coalesce((SELECT array_agg(a.operator_user_id::text ORDER BY a.operator_user_id)
                 FROM pc_care_task_assignees a
                 WHERE a.tenant_id = $1::uuid AND a.task_id = l.task_id), ARRAY[]::text[]),
       (l.round_id IS NOT NULL AND EXISTS (
          SELECT 1 FROM pc_care_tasks g
          WHERE g.tenant_id = $1::uuid AND g.gates_round_id = l.round_id AND g.work_state <> 'canceled')),
       coalesce((SELECT array_agg(DISTINCT a.operator_user_id::text)
                 FROM pc_care_tasks g
                 JOIN pc_care_task_assignees a ON a.tenant_id = g.tenant_id AND a.task_id = g.task_id
                 WHERE g.tenant_id = $1::uuid AND l.round_id IS NOT NULL AND g.gates_round_id = l.round_id
                   AND g.work_state <> 'canceled'), ARRAY[]::text[])
FROM latest l
LEFT JOIN locations s ON s.tenant_id = $1::uuid AND s.location_id = l.shed_id
LEFT JOIN locations p ON p.tenant_id = $1::uuid AND p.location_id = l.park_id
WHERE l.planned_business_date + l.every_days <= $4::date
  AND NOT EXISTS (SELECT 1 FROM pc_care_tasks r WHERE r.tenant_id = $1::uuid AND r.repeat_of_task_id = l.task_id)
  AND NOT EXISTS (SELECT 1 FROM pc_care_repeat_skips k WHERE k.tenant_id = $1::uuid AND k.source_task_id = l.task_id)
ORDER BY l.planned_business_date + l.every_days, l.task_id
LIMIT $5`

// ListRepeatCandidates returns the pens whose work is due to be repeated by the horizon.
func (r *Repository) ListRepeatCandidates(ctx context.Context, tenantID string, cfg []ports.RepeatConfig, through time.Time, limit int) ([]ports.RepeatCandidate, error) {
	if len(cfg) == 0 {
		return nil, nil
	}
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	categories := make([]string, 0, len(cfg))
	intervals := make([]int32, 0, len(cfg))
	for _, c := range cfg {
		categories = append(categories, c.Category)
		intervals = append(intervals, int32(c.EveryDays))
	}
	rows, err := r.pool.Query(ctx, repeatCandidatesSQL, tenantID, categories, intervals, through.Format("2006-01-02"), limit)
	if err != nil {
		return nil, fmt.Errorf("pccare: list repeat candidates: %w", err)
	}
	defer rows.Close()
	out := []ports.RepeatCandidate{}
	for rows.Next() {
		var c ports.RepeatCandidate
		if err := rows.Scan(&c.SourceTaskID, &c.SourceRoundID, &c.Category, &c.ParkID, &c.ParkName, &c.ShedID,
			&c.ShedName, &c.PartitionLabel, &c.PlannedBusinessDate, &c.EveryDays, &c.CreatedBy,
			&c.AssigneeUserIDs, &c.HadRemoval, &c.RemovalOperatorUserIDs); err != nil {
			return nil, fmt.Errorf("pccare: scan repeat candidate: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pccare: iterate repeat candidates: %w", err)
	}
	return out, nil
}

// operatorsAvailableInParkSQL keeps, of the named users, those still able to be assigned work in
// this park: an ACTIVE workforce member with an ACTIVE grant scoped to the tenant or to this
// park -- the same two checks a planner's create applies (operatorsOutsideParkCountSQL,
// activeMembersCountSQL), in one set-based read.
const operatorsAvailableInParkSQL = `
SELECT u.user_id::text
FROM unnest($3::uuid[]) AS u(user_id)
WHERE EXISTS (
    SELECT 1 FROM workforce_members m
    WHERE m.tenant_id = $1::uuid AND m.user_id = u.user_id AND m.status = 'active')
  AND EXISTS (
    SELECT 1 FROM user_scope_grants g
    WHERE g.tenant_id = $1::uuid AND g.user_id = u.user_id AND g.status = 'active'
      AND (g.valid_to IS NULL OR g.valid_to > now())
      AND (g.scope_type = 'tenant' OR (g.scope_type = 'park' AND g.scope_id = $2::uuid)))
ORDER BY u.user_id`

// OperatorsAvailableInPark returns the subset of userIDs who can still be assigned in parkID.
func (r *Repository) OperatorsAvailableInPark(ctx context.Context, tenantID, parkID string, userIDs []string) ([]string, error) {
	if len(userIDs) == 0 {
		return []string{}, nil
	}
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	rows, err := r.pool.Query(ctx, operatorsAvailableInParkSQL, tenantID, parkID, distinctIDs(userIDs))
	if err != nil {
		return nil, fmt.Errorf("pccare: operators available in park: %w", err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("pccare: scan available operator: %w", err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// RecordRepeatSkip writes the one-time skip row for a source task; false when it was already
// recorded (the alert then does not fire again).
func (r *Repository) RecordRepeatSkip(ctx context.Context, tenantID, sourceTaskID, reason string, dueDate time.Time, alertedUser string) (bool, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	tag, err := r.pool.Exec(ctx, `
INSERT INTO pc_care_repeat_skips (tenant_id, source_task_id, reason, due_date, alerted_user)
VALUES ($1::uuid, $2::uuid, $3, $4::date, nullif($5::text, '')::uuid)
ON CONFLICT (tenant_id, source_task_id) DO NOTHING`,
		tenantID, sourceTaskID, reason, dueDate.Format("2006-01-02"), alertedUser)
	if err != nil {
		return false, fmt.Errorf("pccare: record repeat skip: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// roundRepeatStampSQL stamps each new pen task with the task it repeats ($2 new ids, $3 sources).
const roundRepeatStampSQL = `
UPDATE pc_care_tasks t
SET repeat_of_task_id = src.source_id
FROM unnest($2::uuid[], $3::uuid[]) AS src(task_id, source_id)
WHERE t.tenant_id = $1::uuid AND t.task_id = src.task_id`

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
