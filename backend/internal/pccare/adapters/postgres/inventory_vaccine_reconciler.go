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
	LegacyShedTasksClosed int64
	// DirectorAssigneesRemoved counts pc_director assignee rows stripped from unfinished
	// per-vaccine stock tasks during the 2026-09-02 operator cutover.
	DirectorAssigneesRemoved int64
	// ParksMissingOperators counts parks that needed a stock task on this pass but have NO
	// active vaccination operator — those tasks are NOT created (never a fallback assignee)
	// and the caller must surface this loudly as a park-config gap.
	ParksMissingOperators int
}

// ReconcileInventoryVaccineTasks creates fridge-stock tasks seven days before vaccination drive
// dates. The source is vaccination_drive_assignments, the same durable drive plan used by
// operator vaccination screens, so direct DB edits are picked up by the kernel's periodic pass
// without depending on Pub/Sub events.
//
// GRAIN (maintainer decision 2026-08-27): one task per (park, vaccine, task date), NEVER per shed.
// Stock lives in the park's fridge, so the question is "are there N doses of FMD for everything
// scheduled that day?" — which sheds those doses are for is irrelevant to the fridge.
// Doses are counted at the obligation grain (count(DISTINCT obligation_id)) summed across every
// shed of that park's drives on the target date, so one animal is one dose no matter how the
// drive is split across pens.
//
// ASSIGNEES (maintainer decision 2026-09-02, SUPERSEDING the PC-director assignment): each
// park's task is assigned to that PARK'S OWN VACCINATION OPERATORS — the director cannot be in
// both farms, so the people who work vaccination there film the fridge, and the PC Director
// approves the submitted videos on the stock-verdict route. The operator pool is the same
// position/duty definition the vaccination drive planner uses (active non-director positions
// scoped to the park whose duties execute preventive_care/vaccination), without the per-day
// absence/week-off filters — the task is multi-assignee and lives for days, so any teammate can
// record it. A park with NO such operator gets NO task and is reported loudly
// (ParksMissingOperators); a fallback assignee is never invented. The same pass strips any
// remaining pc_director assignee off unfinished per-vaccine tasks so an installed APK cannot
// keep offering the director a camera.
//
// Cutover: any still-open legacy per-shed inventory task (shed_id IS NOT NULL) is canceled on
// every pass; the per-vaccine tasks replace them. Canceling repeatedly is a no-op because the
// predicate only matches open rows.
func (r *Repository) ReconcileInventoryVaccineTasks(ctx context.Context, tenantID string, asOf time.Time) (ReconcileInventoryVaccineTasksResult, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()

	taskDay := biztime.BusinessDayStart(asOf)
	asOfDate := taskDay.Format("2006-01-02")
	latestVaccinationDate := taskDay.AddDate(0, 0, 7).Format("2006-01-02")

	var result ReconcileInventoryVaccineTasksResult
	err := r.pool.QueryRow(ctx, `-- scale-guard:ignore: bounded kernel reconciliation over a seven-day window; materializes tasks/requirements for readers.
-- projection-review: membership=vaccination_drive_assignments exact assignment or exact assignment_members when present; group_key=tenant_id + park_id + lower(btrim(vaccine_label)) + task_date; join_cardinality=protocol_rule_dimensions collapsed by LATERAL LIMIT 1, obligations deduped by count(DISTINCT obligation_id), and stock_operators collapsed to one row per park (array_agg DISTINCT over the position/duty fan-out); pagination=single bounded kernel reconciliation over as_of..as_of+7 before task listing; scope=park-level fridge stock tasks assigned to that park's own vaccination operators, legacy shed-scoped rows and director assignees retired during cutover.
-- One row per (park, vaccine, task_date): the doses needed across EVERY shed of that park's
-- drives on the target date. The vaccine label resolution mirrors the operator screens'
-- protocol_rule_dimensions-first fallback chain.
WITH source_requirements AS (
  SELECT
    v.tenant_id,
    v.park_id,
    (v.planned_date - 7) AS task_date,
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
  FROM vaccination_drive_assignments v
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
  WHERE v.tenant_id = $1::uuid
    AND v.planned_date > $2::date
    AND v.planned_date <= $3::date
    AND v.animal_count > 0
    AND b.status IN ('planned', 'in_progress')
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
  GROUP BY v.tenant_id, v.park_id, (v.planned_date - 7), 4
),
requirement_parks AS (
  SELECT DISTINCT tenant_id, park_id FROM source_requirements WHERE required_doses > 0
),
-- Each park's vaccination operator pool: active non-director positions scoped to the park whose
-- duties execute preventive_care/vaccination — the drive planner's candidate definition, minus
-- the per-day absence/week-off filters (the task is multi-assignee and lives for days).
stock_operators AS (
  SELECT rp.park_id,
         array_agg(DISTINCT wm.user_id) AS user_ids,
         min(wm.user_id::text) AS created_by
  FROM requirement_parks rp
  JOIN locations park_loc
    ON park_loc.tenant_id = rp.tenant_id
   AND park_loc.location_id = rp.park_id
   AND park_loc.status = 'active'
  JOIN workforce_positions wp
    ON wp.tenant_id = rp.tenant_id
   AND wp.status = 'active'
   AND wp.valid_from <= $2::date + interval '1 day'
   AND (wp.valid_to IS NULL OR wp.valid_to > $2::date)
   AND wp.position_tier <> 'director'
   AND (
     (wp.scope_type = 'center' AND wp.scope_id IN (rp.park_id, park_loc.parent_location_id))
     OR (wp.scope_type = 'shed' AND wp.scope_id IN (
       SELECT l.location_id
       FROM locations l
       WHERE l.tenant_id = rp.tenant_id
         AND l.parent_location_id = rp.park_id
         AND l.location_type = 'shed'
         AND l.status = 'active'
     ))
   )
  JOIN position_module_duties pmd
    ON pmd.tenant_id = wp.tenant_id
   AND pmd.position_code = wp.position_code
   AND pmd.status = 'active'
   AND pmd.effective_from <= $2::date + interval '1 day'
   AND (pmd.effective_to IS NULL OR pmd.effective_to > $2::date)
   AND pmd.duty_type = 'execute'
   AND pmd.module_code IN ('preventive_care', 'vaccination', 'pc.vaccination')
  JOIN workforce_members wm
    ON wm.tenant_id = wp.tenant_id
   AND wm.workforce_member_id = wp.workforce_member_id
   AND wm.status = 'active'
   AND wm.user_id IS NOT NULL
  GROUP BY rp.park_id
),
-- A park with no operator pool creates NO task (the inner join) — never a fallback assignee.
source_tasks AS (
  SELECT sr.tenant_id, sr.park_id, sr.task_date, sr.vaccine_label, so.created_by
  FROM source_requirements sr
  JOIN stock_operators so ON so.park_id = sr.park_id
  WHERE sr.required_doses > 0
),
inserted_tasks AS (
  INSERT INTO pc_care_tasks (
    tenant_id, category, park_id, vaccine_label,
    planned_business_date, due_business_date, idempotency_key, created_by
  )
  SELECT
    tenant_id,
    $4,
    park_id,
    vaccine_label,
    task_date,
    task_date,
    'kernel:inventory-vaccine:' || tenant_id::text || ':' || park_id::text || ':vaccine:' ||
      lower(btrim(vaccine_label)) || ':' || task_date::text,
    created_by::uuid
  FROM source_tasks
  ON CONFLICT (tenant_id, category, park_id, lower(btrim(vaccine_label)), planned_business_date)
    WHERE work_state <> 'canceled' AND vaccine_label IS NOT NULL
  DO NOTHING
  RETURNING tenant_id, task_id, park_id, vaccine_label, planned_business_date AS task_date
),
live_tasks AS (
  SELECT tenant_id, task_id, park_id, vaccine_label, task_date
  FROM inserted_tasks
  UNION
  SELECT t.tenant_id, t.task_id, t.park_id, t.vaccine_label, t.planned_business_date AS task_date
  FROM pc_care_tasks t
  JOIN (SELECT DISTINCT tenant_id, park_id, task_date, vaccine_label FROM source_requirements) c
    ON c.tenant_id = t.tenant_id
   AND c.park_id = t.park_id
   AND lower(btrim(c.vaccine_label)) = lower(btrim(t.vaccine_label))
   AND c.task_date = t.planned_business_date
  WHERE t.category = $4
    AND t.vaccine_label IS NOT NULL
    AND t.work_state <> 'canceled'
),
-- A per-vaccine task whose drive evaporated (moved, canceled, completed) before its day is done.
stale_inventory_tasks AS (
  SELECT t.tenant_id, t.task_id
  FROM pc_care_tasks t
  WHERE t.tenant_id = $1::uuid
    AND t.category = $4
    AND t.vaccine_label IS NOT NULL
    AND t.planned_business_date <= $2::date
    AND t.work_state IN ('scheduled', 'delayed')
    AND t.status = 'open'
    AND NOT EXISTS (
      SELECT 1
      FROM source_requirements sr
      WHERE sr.tenant_id = t.tenant_id
        AND sr.park_id = t.park_id
        AND lower(btrim(sr.vaccine_label)) = lower(btrim(t.vaccine_label))
        AND sr.task_date = t.planned_business_date
        AND sr.required_doses > 0
    )
),
-- Cutover: the per-shed grain is retired. Any still-open legacy per-shed stock task is
-- canceled; its planned work reappears on the per-vaccine tasks above. Submitted/completed
-- legacy rows keep their history untouched.
legacy_shed_tasks AS (
  SELECT t.tenant_id, t.task_id
  FROM pc_care_tasks t
  WHERE t.tenant_id = $1::uuid
    AND t.category = $4
    AND t.shed_id IS NOT NULL
    AND t.work_state IN ('scheduled', 'delayed')
    AND t.status = 'open'
),
inserted_assignees AS (
  INSERT INTO pc_care_task_assignees (tenant_id, task_id, operator_user_id)
  SELECT lt.tenant_id, lt.task_id, unnest(so.user_ids)
  FROM live_tasks lt
  JOIN stock_operators so ON so.park_id = lt.park_id
  ON CONFLICT DO NOTHING
  RETURNING 1
),
-- 2026-09-02 cutover: strip any remaining pc_director assignee off UNFINISHED per-vaccine
-- stock tasks — the director judges the videos and must not be offered a camera. A submitted
-- or completed task keeps its assignee history untouched.
removed_director_assignees AS (
  DELETE FROM pc_care_task_assignees a
  USING pc_care_tasks t, workforce_members dm
  WHERE t.tenant_id = $1::uuid
    AND t.category = $4
    AND t.vaccine_label IS NOT NULL
    AND t.status IN ('open', 'rework')
    AND t.work_state IN ('scheduled', 'delayed')
    AND a.tenant_id = t.tenant_id
    AND a.task_id = t.task_id
    AND dm.tenant_id = a.tenant_id
    AND dm.user_id = a.operator_user_id
    AND dm.primary_role_hint = 'pc_director'
  RETURNING 1
),
requirement_source AS (
  SELECT lt.tenant_id, lt.task_id, sr.vaccine_label, sr.required_doses, sr.source_batch_ids
  FROM live_tasks lt
  JOIN source_requirements sr
    ON sr.tenant_id = lt.tenant_id
   AND sr.park_id = lt.park_id
   AND lower(btrim(sr.vaccine_label)) = lower(btrim(lt.vaccine_label))
   AND sr.task_date = lt.task_date
  WHERE sr.required_doses > 0
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
canceled_legacy_shed_tasks AS (
  UPDATE pc_care_tasks t
  SET work_state = 'canceled',
      terminal_at = now(),
      updated_at = now(),
      row_version = row_version + 1
  FROM legacy_shed_tasks legacy
  WHERE t.tenant_id = legacy.tenant_id
    AND t.task_id = legacy.task_id
  RETURNING t.tenant_id, t.task_id
),
deleted_canceled_requirements AS (
  DELETE FROM pc_care_task_inventory_requirements r
  USING (
    SELECT tenant_id, task_id FROM canceled_stale_tasks
    UNION
    SELECT tenant_id, task_id FROM canceled_legacy_shed_tasks
  ) canceled
  WHERE r.tenant_id = canceled.tenant_id
    AND r.task_id = canceled.task_id
  RETURNING 1
)
SELECT
  (SELECT count(*) FROM inserted_tasks)::bigint,
  (SELECT count(*) FROM inserted_assignees)::bigint,
  (SELECT count(*) FROM upserted_requirements)::bigint,
  (SELECT count(*) FROM canceled_legacy_shed_tasks)::bigint,
  (SELECT count(*) FROM removed_director_assignees)::bigint,
  (SELECT count(*)::int
   FROM requirement_parks rp
   WHERE NOT EXISTS (SELECT 1 FROM stock_operators so WHERE so.park_id = rp.park_id))`,
		tenantID, asOfDate, latestVaccinationDate, domain.CategoryInventoryVaccine).Scan(
		&result.TasksCreated,
		&result.AssigneesInserted,
		&result.RequirementsUpserted,
		&result.LegacyShedTasksClosed,
		&result.DirectorAssigneesRemoved,
		&result.ParksMissingOperators,
	)
	if err != nil {
		return result, fmt.Errorf("pccare: reconcile inventory vaccine tasks: %w", err)
	}
	return result, nil
}
