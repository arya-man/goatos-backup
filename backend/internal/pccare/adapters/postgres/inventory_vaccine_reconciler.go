package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/vgoats/goatos/backend/internal/pccare/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// ReconcileInventoryVaccineTasksResult reports the idempotent kernel pass.
type ReconcileInventoryVaccineTasksResult struct {
	TasksCreated          int64
	AssigneesInserted     int64
	RequirementsUpserted  int64
	DirectorAssigneeCount int
}

// ReconcileInventoryVaccineTasks creates PC-director fridge-stock tasks seven days before
// vaccination drive dates. The source is vaccination_drive_assignments, the same durable drive plan
// used by operator vaccination screens, so direct DB edits are picked up by the kernel's periodic
// pass without depending on Pub/Sub events.
func (r *Repository) ReconcileInventoryVaccineTasks(ctx context.Context, tenantID string, asOf time.Time) (ReconcileInventoryVaccineTasksResult, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()

	taskDay := biztime.BusinessDayStart(asOf)
	asOfDate := taskDay.Format("2006-01-02")
	latestVaccinationDate := taskDay.AddDate(0, 0, 7).Format("2006-01-02")

	var result ReconcileInventoryVaccineTasksResult
	err := r.pool.QueryRow(ctx, ` -- scale-guard:ignore: bounded kernel reconciliation for one tenant and 7-day vaccination assignment window, not request-path fanout
-- projection-review: membership=vaccination_drive_assignments; group_key=(tenant_id, park_id, shed_id, normalized partition_label, task_date, vaccine_label) so each stock task/requirement line is idempotent; join_cardinality=drive assignments and obligation instances are collapsed by GROUP BY before writes, director assignees are inserted through bounded active pc_director users, and stale tasks are semi-joined by NOT EXISTS so no branch multiplies task rows; pagination=none because this is a scheduled/kernel reconciliation over a fixed seven-day window, not a request page; scope=tenant_id plus drive park/shed/partition/date predicates
WITH directors AS (
  SELECT array_agg(m.user_id ORDER BY m.display_name, m.user_id) AS user_ids,
         min(m.user_id::text) AS created_by,
         count(*)::int AS n
  FROM workforce_members m
  WHERE m.tenant_id = $1::uuid
    AND m.status = 'active'
    AND m.user_id IS NOT NULL
    AND m.primary_role_hint = 'pc_director'
),
source_tasks AS (
  SELECT
    v.tenant_id,
    v.park_id,
    v.shed_id,
    nullif(v.partition_label, 'whole') AS partition_label,
    (v.planned_date - 7) AS task_date
  FROM vaccination_drive_assignments v
  JOIN obligation_batches b
    ON b.tenant_id = v.tenant_id
   AND b.batch_id = v.batch_id
  WHERE v.tenant_id = $1::uuid
    AND v.planned_date > $2::date
    AND v.planned_date <= $3::date
    AND v.shed_id IS NOT NULL
    AND v.animal_count > 0
    AND b.status IN ('planned', 'in_progress')
  GROUP BY v.tenant_id, v.park_id, v.shed_id, nullif(v.partition_label, 'whole'), (v.planned_date - 7)
),
candidate_tasks AS (
  SELECT s.*, (SELECT created_by FROM directors) AS created_by
  FROM source_tasks s
  WHERE (SELECT n FROM directors) > 0
),
inserted_tasks AS (
  INSERT INTO pc_care_tasks (
    tenant_id, category, park_id, shed_id, partition_label,
    planned_business_date, due_business_date, idempotency_key, created_by
  )
  SELECT
    tenant_id,
    $4,
    park_id,
    shed_id,
    partition_label,
    task_date,
    task_date,
    'kernel:inventory-vaccine:' || tenant_id::text || ':' || park_id::text || ':' || shed_id::text || ':' ||
      coalesce(partition_label, 'whole') || ':' || task_date::text,
    created_by::uuid
  FROM candidate_tasks
  ON CONFLICT (tenant_id, category, park_id, shed_id, partition_key, planned_business_date)
    WHERE work_state <> 'canceled'
  DO NOTHING
  RETURNING tenant_id, task_id, park_id, shed_id, coalesce(partition_label, '') AS partition_label, planned_business_date AS task_date
),
live_tasks AS (
  SELECT tenant_id, task_id, park_id, shed_id, partition_label, task_date
  FROM inserted_tasks
  UNION
  SELECT t.tenant_id, t.task_id, t.park_id, t.shed_id, coalesce(t.partition_label, '') AS partition_label, t.planned_business_date AS task_date
  FROM pc_care_tasks t
  JOIN source_tasks c
    ON c.tenant_id = t.tenant_id
   AND c.park_id = t.park_id
   AND c.shed_id = t.shed_id
   AND coalesce(c.partition_label, '') = coalesce(t.partition_label, '')
   AND c.task_date = t.planned_business_date
  WHERE t.category = $4
    AND t.work_state <> 'canceled'
),
stale_inventory_tasks AS (
  SELECT t.tenant_id, t.task_id
  FROM pc_care_tasks t
  WHERE t.tenant_id = $1::uuid
    AND t.category = $4
    AND t.planned_business_date <= $2::date
    AND t.work_state IN ('scheduled', 'delayed')
    AND t.status = 'open'
    AND NOT EXISTS (
      SELECT 1
      FROM vaccination_drive_assignments v
      JOIN obligation_batches b
        ON b.tenant_id = v.tenant_id
       AND b.batch_id = v.batch_id
      WHERE v.tenant_id = t.tenant_id
        AND v.park_id = t.park_id
        AND v.shed_id = t.shed_id
        AND coalesce(nullif(v.partition_label, 'whole'), '') = coalesce(t.partition_label, '')
        AND v.planned_date = (t.planned_business_date + 7)
        AND v.animal_count > 0
        AND b.status IN ('planned', 'in_progress')
    )
),
inserted_assignees AS (
  INSERT INTO pc_care_task_assignees (tenant_id, task_id, operator_user_id)
  SELECT lt.tenant_id, lt.task_id, unnest((SELECT user_ids FROM directors))
  FROM live_tasks lt
  ON CONFLICT DO NOTHING
  RETURNING 1
),
requirement_source AS (
  -- projection-review: membership=requirement_source; group_key=(tenant_id, task_id, vaccine_label) so one-to-many assignment members collapse into a single requirement row per stock task and vaccine; join_cardinality=obligation_instances are filtered by the assignment's vaccine_rule_ids and optional assignment_members exact match before COUNT(DISTINCT obligation_id), while protocol_rule_dimensions is limited to one row per rule; pagination=none because reconciliation is a bounded kernel job over the generated live task set, with ListTasks pagination covered separately; scope=tenant_id plus task_date, park_id, shed_id, partition_label, batch status, obligation status, and exact member constraints
  SELECT
    lt.tenant_id,
    lt.task_id,
    COALESCE(
      CASE upper(nullif(prd.vaccine_code, ''))
        WHEN 'ET_TT' THEN 'ET+TT'
        WHEN 'ETTT' THEN 'ET+TT'
        WHEN 'BLUE_TONGUE' THEN 'Blue Tongue'
        WHEN 'GOAT_POX' THEN 'Goat Pox'
        WHEN 'SHEEP_POX' THEN 'Sheep Pox'
        ELSE nullif(prd.vaccine_code, '')
      END,
      nullif(prd.vaccine_type, ''),
      nullif(pv.rule_dsl -> 'vaccine' ->> 'name', ''),
      nullif(pv.rule_dsl -> 'vaccine' ->> 'code', ''),
      nullif(pr.dose_code, ''),
      'Unspecified'
    ) AS vaccine_label,
    count(DISTINCT oi.obligation_id)::int AS required_doses,
    array_agg(DISTINCT v.batch_id ORDER BY v.batch_id) AS source_batch_ids
  FROM live_tasks lt
  JOIN vaccination_drive_assignments v
    ON v.tenant_id = lt.tenant_id
   AND v.park_id = lt.park_id
   AND v.shed_id = lt.shed_id
   AND coalesce(nullif(v.partition_label, 'whole'), '') = lt.partition_label
   AND v.planned_date = (lt.task_date + 7)
  JOIN obligation_batches b
    ON b.tenant_id = v.tenant_id
   AND b.batch_id = v.batch_id
  JOIN obligation_instances oi
    ON oi.tenant_id = v.tenant_id
   AND oi.batch_id = v.batch_id
   AND (cardinality(v.vaccine_rule_ids) = 0 OR oi.rule_id = ANY(v.vaccine_rule_ids))
  LEFT JOIN vaccination_drive_assignment_members vdam
    ON vdam.tenant_id = v.tenant_id
   AND vdam.assignment_id = v.assignment_id
   AND vdam.obligation_id = oi.obligation_id
  JOIN protocol_rules pr
    ON pr.tenant_id = oi.tenant_id
   AND pr.protocol_version_id = oi.protocol_version_id
   AND pr.rule_id = oi.rule_id
  JOIN protocol_versions pv
    ON pv.tenant_id = oi.tenant_id
   AND pv.protocol_version_id = oi.protocol_version_id
  LEFT JOIN LATERAL (
    SELECT prd.vaccine_code, prd.vaccine_type
    FROM protocol_rule_dimensions prd
    WHERE prd.tenant_id = oi.tenant_id
      AND prd.rule_id = oi.rule_id
    ORDER BY prd.vaccine_type, prd.vaccine_code
    LIMIT 1
  ) prd ON true
  WHERE b.status IN ('planned', 'in_progress')
    AND oi.status NOT IN ('completed', 'waived', 'canceled', 'superseded')
    AND (
      NOT EXISTS (
        SELECT 1
        FROM vaccination_drive_assignment_members exact
        WHERE exact.tenant_id = v.tenant_id
          AND exact.assignment_id = v.assignment_id
      )
      OR vdam.obligation_id IS NOT NULL
    )
  GROUP BY lt.tenant_id, lt.task_id, vaccine_label
),
upserted_requirements AS (
  INSERT INTO pc_care_task_inventory_requirements (
    tenant_id, task_id, vaccine_label, required_doses, source_batch_ids, updated_at
  )
  SELECT tenant_id, task_id, vaccine_label, required_doses, source_batch_ids, now()
  FROM requirement_source
  ON CONFLICT (tenant_id, task_id, vaccine_label)
  DO UPDATE SET
    required_doses = EXCLUDED.required_doses,
    source_batch_ids = EXCLUDED.source_batch_ids,
    updated_at = now()
  RETURNING 1
),
deleted_stale_requirements AS (
  DELETE FROM pc_care_task_inventory_requirements r
  USING live_tasks lt
  WHERE r.tenant_id = lt.tenant_id
    AND r.task_id = lt.task_id
    AND NOT EXISTS (
      SELECT 1
      FROM requirement_source rs
      WHERE rs.tenant_id = r.tenant_id
        AND rs.task_id = r.task_id
        AND rs.vaccine_label = r.vaccine_label
  )
  RETURNING 1
),
canceled_stale_tasks AS (
  UPDATE pc_care_tasks t
  SET work_state = 'canceled',
      terminal_at = now(),
      updated_at = now(),
      row_version = row_version + 1
  FROM stale_inventory_tasks stale
  WHERE t.tenant_id = stale.tenant_id
    AND t.task_id = stale.task_id
  RETURNING t.tenant_id, t.task_id
),
canceled_empty_inventory_tasks AS (
  UPDATE pc_care_tasks t
  SET work_state = 'canceled',
      terminal_at = now(),
      updated_at = now(),
      row_version = row_version + 1
  FROM live_tasks lt
  WHERE t.tenant_id = lt.tenant_id
    AND t.task_id = lt.task_id
    AND t.work_state IN ('scheduled', 'delayed')
    AND t.status = 'open'
    AND NOT EXISTS (
      SELECT 1
      FROM requirement_source rs
      WHERE rs.tenant_id = lt.tenant_id
        AND rs.task_id = lt.task_id
    )
  RETURNING t.tenant_id, t.task_id
),
deleted_canceled_requirements AS (
  DELETE FROM pc_care_task_inventory_requirements r
  USING (
    SELECT tenant_id, task_id FROM canceled_stale_tasks
    UNION
    SELECT tenant_id, task_id FROM canceled_empty_inventory_tasks
  ) canceled
  WHERE r.tenant_id = canceled.tenant_id
    AND r.task_id = canceled.task_id
  RETURNING 1
)
SELECT
  (SELECT count(*) FROM inserted_tasks)::bigint,
  (SELECT count(*) FROM inserted_assignees)::bigint,
  (SELECT count(*) FROM upserted_requirements)::bigint,
  coalesce((SELECT n FROM directors), 0)::int`,
		tenantID, asOfDate, latestVaccinationDate, domain.CategoryInventoryVaccine).Scan(
		&result.TasksCreated,
		&result.AssigneesInserted,
		&result.RequirementsUpserted,
		&result.DirectorAssigneeCount,
	)
	if err != nil {
		return result, fmt.Errorf("pccare: reconcile inventory vaccine tasks: %w", err)
	}
	return result, nil
}
