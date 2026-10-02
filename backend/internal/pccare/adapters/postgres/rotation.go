package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/vgoats/goatos/backend/internal/pccare/ports"
	"github.com/vgoats/goatos/backend/internal/platform/sqlbind"
)

// PC CARE ROTATION (maintainer instruction 2026-10-02, docs/decisions/pc-care-rotation.md). A
// card set to rotate goes round the park ONE PEN A DAY: when every pen of the latest task is
// SUBMITTED, the next pen with animals in it, in the park's pen order, is planned for the day
// after; after the last pen it wraps to the first, after the card's gap.

var _ ports.RotationStore = (*Repository)(nil)

// rotationTaskPenSortSQL is a task's pen ORDER key, built exactly as penCoverageScopedPensSQL
// builds pen_sort (same natural key over the shed name and the partition label), so "the pen
// after this one" is decided on one ordering.
var rotationTaskPenSortSQL = `(` + naturalSortKeySQL("ts.name") + ` || chr(1) || ` + naturalSortKeySQL("t.partition_label") + `) COLLATE "C"`

// rotationCandidatesSQL ($1 tenant, $2 true, $3 empty parks, $4 ” -- the catalog CTE's own
// parameters, asking for every park -- $5 categories, $6 gaps, $7 today, $8 horizon, $9 limit).
//
// projection-review: membership=pc_care_tasks of the tenant in a rotating category, not
// canceled, pen-grain, not a removal card; group_key=(category, park_id) for "the latest task"
// (DISTINCT ON, latest planned date then created_at), then that task's ROUND (round_id, or the
// task alone) for "every pen submitted" and "the last pen", and the pen catalog is
// penCoverageScopedPensSQL keyed (park_id, pen_sort, shed_id, partition_key) -- the Care
// Coverage board's own occupied-pen set and order; join_cardinality=cfg is one row per category,
// the round aggregate is one row per (category, park), the next-pen lateral is LIMIT 1, the
// assignee list is a scalar array subquery -- nothing fans out; pagination=LIMIT $9 per tick,
// ordered by (next date, source task id); scope=tenant_id, and per (category, park) the next pen
// is read from THAT park's pens only.
//
// scale-guard:ignore: one bounded set-based read per kernel tick over a tenant's PC Care tasks (a few thousand rows at the 5k-50k envelope) and its pen catalog (physical infrastructure, a few hundred rows), LIMIT per tick.
var rotationCandidatesSQL = `
WITH cfg AS (
  SELECT c.category, c.gap_days
  FROM unnest($5::text[], $6::int[]) AS c(category, gap_days)
),
latest AS (
  SELECT DISTINCT ON (t.category, t.park_id)
         t.task_id, t.round_id, t.category, t.park_id, cfg.gap_days
  FROM pc_care_tasks t
  JOIN cfg ON cfg.category = t.category
  WHERE t.tenant_id = $1::uuid
    AND t.work_state <> 'canceled'
    AND t.shed_id IS NOT NULL
    AND t.gates_round_id IS NULL
    AND t.gates_task_id IS NULL
  ORDER BY t.category, t.park_id, t.planned_business_date DESC, t.created_at DESC, t.task_id DESC
),
round_pens AS (
  -- The latest task's whole round (a hand-planned round may hold several pens), each with its
  -- pen order key. A round-less task is a round of one.
  SELECT l.category, l.park_id, l.gap_days, t.task_id, t.shed_id,
         t.partition_key,
         t.submitted_at, t.planned_business_date, t.created_by,
         ` + rotationTaskPenSortSQL + ` AS pen_sort
  FROM latest l
  JOIN pc_care_tasks t
    ON t.tenant_id = $1::uuid
   AND t.category = l.category
   AND t.work_state <> 'canceled'
   AND (t.task_id = l.task_id OR (l.round_id IS NOT NULL AND t.round_id = l.round_id))
  JOIN locations ts ON ts.tenant_id = t.tenant_id AND ts.location_id = t.shed_id
),
rounds AS (
  SELECT r.category, r.park_id, max(r.gap_days) AS gap_days,
         bool_and(r.submitted_at IS NOT NULL) AS all_submitted,
         max(greatest((r.submitted_at AT TIME ZONE 'Asia/Kolkata')::date, r.planned_business_date)) AS last_day,
         (array_agg(r.task_id ORDER BY r.pen_sort DESC, r.shed_id::text DESC, r.partition_key DESC))[1] AS source_task_id,
         (array_agg(r.pen_sort ORDER BY r.pen_sort DESC, r.shed_id::text DESC, r.partition_key DESC))[1] AS last_sort,
         (array_agg(r.shed_id::text ORDER BY r.pen_sort DESC, r.shed_id::text DESC, r.partition_key DESC))[1] AS last_shed,
         (array_agg(r.partition_key ORDER BY r.pen_sort DESC, r.shed_id::text DESC, r.partition_key DESC))[1] AS last_partition,
         (array_agg(r.created_by::text ORDER BY r.pen_sort DESC, r.shed_id::text DESC, r.partition_key DESC))[1] AS created_by
  FROM round_pens r
  GROUP BY r.category, r.park_id
),
pens AS (
` + penCoverageScopedPensSQL + `),
due AS (
  SELECT r.*, nxt.shed_id AS next_shed_id, nxt.shed_name AS next_shed_name,
         nxt.partition_label AS next_partition_label, nxt.wrapped,
         greatest(r.last_day + 1 + CASE WHEN nxt.wrapped THEN r.gap_days ELSE 0 END, $7::date) AS next_date
  FROM rounds r
  CROSS JOIN LATERAL (
    SELECT p.shed_id, p.shed_name, p.partition_label,
           NOT ((p.pen_sort, p.shed_id::text, p.partition_key) > (r.last_sort, r.last_shed, r.last_partition)) AS wrapped
    FROM pens p
    WHERE p.park_id = r.park_id
    -- The next pen after the last one in pen order; when none is after it, the first pen (a new
    -- round). Ordering "after" first, then pen order, picks exactly that in one LIMIT 1.
    ORDER BY ((p.pen_sort, p.shed_id::text, p.partition_key) > (r.last_sort, r.last_shed, r.last_partition)) DESC,
             p.pen_sort, p.shed_id::text, p.partition_key
    LIMIT 1
  ) nxt
  WHERE r.all_submitted
)
SELECT d.source_task_id::text, d.category, d.park_id::text, COALESCE(park.name, ''), COALESCE(d.created_by, ''),
       COALESCE((SELECT array_agg(a.operator_user_id::text ORDER BY a.operator_user_id)
                 FROM pc_care_task_assignees a
                 WHERE a.tenant_id = $1::uuid AND a.task_id = d.source_task_id), ARRAY[]::text[]),
       d.next_date, d.next_shed_id::text, d.next_shed_name, d.next_partition_label, d.wrapped
FROM due d
LEFT JOIN locations park ON park.tenant_id = $1::uuid AND park.location_id = d.park_id
WHERE d.next_date <= $8::date
  AND NOT EXISTS (SELECT 1 FROM pc_care_tasks x WHERE x.tenant_id = $1::uuid AND x.repeat_of_task_id = d.source_task_id)
  AND NOT EXISTS (SELECT 1 FROM pc_care_repeat_skips k WHERE k.tenant_id = $1::uuid AND k.source_task_id = d.source_task_id)
ORDER BY d.next_date, d.source_task_id
LIMIT $9`

// ListRotationCandidates returns the parks whose rotating work is ready for its next pen.
func (r *Repository) ListRotationCandidates(ctx context.Context, tenantID string, cfg []ports.RotationConfig, today, through time.Time, limit int) ([]ports.RotationCandidate, error) {
	if len(cfg) == 0 {
		return nil, nil
	}
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	categories := make([]string, 0, len(cfg))
	gaps := make([]int32, 0, len(cfg))
	for _, c := range cfg {
		categories = append(categories, c.Category)
		gaps = append(gaps, int32(c.GapDays))
	}
	bound := sqlbind.MustBind(rotationCandidatesSQL, tenantID, true, []string{}, "",
		categories, gaps, today.Format("2006-01-02"), through.Format("2006-01-02"), limit)
	rows, err := r.pool.Query(ctx, bound.SQL(), bound.Args()...)
	if err != nil {
		return nil, fmt.Errorf("pccare: list rotation candidates: %w", err)
	}
	defer rows.Close()
	out := []ports.RotationCandidate{}
	for rows.Next() {
		var c ports.RotationCandidate
		if err := rows.Scan(&c.SourceTaskID, &c.Category, &c.ParkID, &c.ParkName, &c.CreatedBy,
			&c.AssigneeUserIDs, &c.NextDate, &c.ShedID, &c.ShedName, &c.PartitionLabel, &c.Wrapped); err != nil {
			return nil, fmt.Errorf("pccare: scan rotation candidate: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pccare: iterate rotation candidates: %w", err)
	}
	return out, nil
}
