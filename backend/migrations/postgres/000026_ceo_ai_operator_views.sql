-- +goose Up
-- +goose StatementBegin
-- ===========================================================================
-- ceo_ai OPERATOR-GRAIN vaccination reporting — the governed, business-language
-- read surface for the OPERATOR-BASED vaccination drive model.
--
-- WHY THIS EXISTS
-- Main moved vaccination drive planning to an OPERATOR grain: drives are planned
-- by operator animal CAPACITY, work is assigned at OPERATOR grain, and proof is
-- shed-level video. The prior ceo_ai vaccination views (migration 000023:
-- vaccination_shed_status / vaccination_dose_pickup) answer the SHED question
-- (which sheds are due/overdue, doses to pick per shed). They carry NO operator
-- dimension, so the leadership assistant could not answer "which operators are
-- behind", "who is overloaded", "operator drive assignments today", or "how many
-- animals is <operator> assigned". This view closes that gap over the REAL new
-- operator tables.
--
-- SOURCE (operator model, learned from main):
--   * public.vaccination_drive_assignments (migration 000020, operator grain in
--     000022) — one row per (batch, planned_date, park, shed, partition,
--     operator). Columns used: tenant_id, operator_id, park_id, shed_id,
--     physical_shed, partition_label, animal_count, capacity_status, batch_id,
--     planned_date. operator_id FK → workforce_members(workforce_member_id).
--   * public.obligation_batches — batch_id status (planned/in_progress/completed/
--     …) tells whether the assigned drive work is done vs still open. Each
--     assignment row references exactly ONE batch (assignment→batch is 1:1), so
--     joining batch status never fans out the animal_count sum.
--   * public.vaccination_capacity_config — tenant-scoped max_per_day is the
--     animal CAP one available operator handles on one business date (the unit
--     the OperatorDrivePlanner consumes: unique animals per operator per day).
--   * public.workforce_members — operator display label.
--
-- GRAIN: one row per (tenant_id, operator_id, planned_date, park_id, shed_id).
-- This preserves BOTH the operator dimension AND the park→shed scope leadership
-- filters on. Capacity/utilization are a per-operator-per-DAY concept, so a
-- day-level window carries the operator's whole-day assigned total and the
-- utilization ratio onto every shed row (capacity is NOT summed across sheds —
-- summing a per-day cap across shed rows would multiply it; the Cube measure
-- uses MAX(daily_capacity) at the operator-day grouping to avoid that trap).
--
-- SCALE: vaccination_drive_assignments is a DRIVE-PLAN table (bounded by
-- operators × business days × sheds in the planning window), NOT a per-animal
-- table, so this is a bounded reporting read at the 5k-50k envelope
-- (docs/decisions/operational-kernel-5k-50k-scale-envelope.md). The tenant
-- predicate pushes into the base scan because tenant_id is a GROUP BY key, and
-- vaccination_drive_assignments_operator_day_idx (tenant_id, operator_id,
-- planned_date) covers the operator/day access.
--
-- IST business calendar: "overdue" compares planned_date to the Asia/Kolkata
-- business day, never a UTC instant.
-- ===========================================================================

-- projection-review: membership=canonical vaccination_drive_assignments rows filtered to the current tenant (operator_id NOT NULL); group_key=(tenant_id, operator_id, planned_date, park_id, shed_id) — every SUM/FILTER below groups on the same key; join_cardinality=assignment→obligation_batches is 1:1 on (tenant_id,batch_id) so the batch-status FILTER never double-counts animal_count, and workforce_members/locations/capacity_config are 1:1 dimension joins on their keys (max_per_day is tenant-constant, joined 1:1); pagination=drive-plan table bounded by operators×days×sheds, read tenant-scoped with LIMIT at the Cube/toolbox call site; scope=explicit park_id/shed_id + operator_id preserved on every row so leadership scope (operator→park→shed→day) and the capacity/overdue matrix resolve without collapsing operators or sheds. daily_capacity is per-operator-per-day (NOT additive across sheds); utilization is computed from a per-(operator,day) window sum, and the Cube twin aggregates capacity with MAX not SUM. Adversarial grain proof: backend/internal/ceoai/reporting/operator_views_test.go (Postgres-gated).
CREATE OR REPLACE VIEW ceo_ai.vaccination_operator_status AS
WITH assigned AS (
    SELECT
        a.tenant_id,
        a.operator_id,
        a.park_id,
        a.shed_id,
        a.planned_date,
        COALESCE(SUM(a.animal_count), 0)::bigint                                            AS assigned_animals,
        COALESCE(SUM(a.animal_count) FILTER (WHERE b.status IN ('planned','in_progress')), 0)::bigint AS due,
        COALESCE(SUM(a.animal_count) FILTER (WHERE b.status = 'completed'), 0)::bigint       AS done,
        COALESCE(SUM(a.animal_count) FILTER (
            WHERE b.status IN ('planned','in_progress')
              AND a.planned_date < (now() AT TIME ZONE 'Asia/Kolkata')::date
        ), 0)::bigint                                                                        AS overdue,
        bool_or(a.capacity_status IN ('over_cap_required','capacity_action'))               AS any_over_cap
    FROM public.vaccination_drive_assignments a
    LEFT JOIN public.obligation_batches b
      ON b.tenant_id = a.tenant_id AND b.batch_id = a.batch_id
    WHERE a.operator_id IS NOT NULL
    GROUP BY a.tenant_id, a.operator_id, a.park_id, a.shed_id, a.planned_date
),
cap AS (
    -- tenant-wide per-operator-per-day animal cap (only 'tenant' scope is honored
    -- by the planner today; see vaccinationexecution/domain/capacity.go).
    SELECT tenant_id, max_per_day
    FROM public.vaccination_capacity_config
    WHERE capacity_scope = 'tenant'
)
SELECT
    s.tenant_id                                                            AS tenant_id,
    s.operator_id                                                          AS operator_id,
    wm.display_name                                                        AS operator_label,
    s.park_id                                                             AS park_id,
    pk.name                                                                AS park_label,
    s.shed_id                                                             AS shed_id,
    sh.name                                                                AS shed_label,
    s.planned_date                                                        AS planned_date,
    s.assigned_animals                                                     AS assigned_animals,
    s.due                                                                  AS due,
    s.done                                                                 AS done,
    s.overdue                                                              AS overdue,
    COALESCE(cap.max_per_day, 200)                                         AS daily_capacity,
    -- the operator's WHOLE-day assigned total (across every shed that day), the
    -- correct numerator for a per-operator-per-day utilization ratio.
    SUM(s.assigned_animals) OVER (
        PARTITION BY s.tenant_id, s.operator_id, s.planned_date
    )                                                                     AS operator_day_assigned,
    ROUND(
        (SUM(s.assigned_animals) OVER (
            PARTITION BY s.tenant_id, s.operator_id, s.planned_date
        ))::numeric / NULLIF(COALESCE(cap.max_per_day, 200), 0), 3
    )                                                                     AS utilization,
    CASE
        WHEN s.overdue > 0 THEN 'catch_up_overdue'
        WHEN (SUM(s.assigned_animals) OVER (
                 PARTITION BY s.tenant_id, s.operator_id, s.planned_date))
             > COALESCE(cap.max_per_day, 200) THEN 'rebalance_overloaded'
        WHEN s.due > 0 THEN 'run_drive'
        WHEN s.done > 0 AND s.due = 0 THEN 'completed'
        ELSE 'no_action'
    END                                                                   AS next_action
FROM assigned s
LEFT JOIN public.workforce_members wm ON wm.workforce_member_id = s.operator_id
LEFT JOIN public.locations         pk ON pk.location_id = s.park_id
LEFT JOIN public.locations         sh ON sh.location_id = s.shed_id
LEFT JOIN cap ON cap.tenant_id = s.tenant_id;
-- +goose StatementEnd

-- ===========================================================================
-- GUARDED GRANTS — re-apply the ceo_ai SELECT grant for the new view if the
-- read-only roles exist (idempotent; mirrors migration 000023). ALTER DEFAULT
-- PRIVILEGES from 000023 already covers objects created afterward, but an
-- explicit grant here keeps this migration self-contained and safe to re-run.
-- ===========================================================================
-- +goose StatementBegin
DO $grants$
DECLARE
    r text;
BEGIN
    FOREACH r IN ARRAY ARRAY['mesha_ceo_readonly','mesha_cube_readonly'] LOOP
        IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = r) THEN
            EXECUTE format('GRANT SELECT ON ceo_ai.vaccination_operator_status TO %I', r);
        END IF;
    END LOOP;
END;
$grants$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP VIEW IF EXISTS ceo_ai.vaccination_operator_status;
-- +goose StatementEnd
