-- +goose Up
-- Partition-scope fix for four ceo_ai leadership-reporting views, per maintainer
-- decision (backend/internal/platform/oploc is the shared OperationalLocation
-- primitive; see its package doc for the domain rule this migration implements).
--
-- OperationalLocation = park + physical shed + OPTIONAL partition. goats.shed_id
-- is always the PARENT physical shed; goat_shed_partitions (tenant_id, goat_id)
-- carries the actual sub-location for goats that live in a partitioned shed. Not
-- every shed has partitions -- a non-partitioned shed must render as the bare
-- shed name, never as a synthetic "whole" location. Before this migration, four
-- ceo_ai views silently collapsed every partition of a shed into one shed-grain
-- row, so "how many kids in Castro 1" and "how many kids in Castro 2" both
-- answered with the Castro TOTAL.
--
-- FIXED HERE (can join goat_shed_partitions / already carry partition_label):
--   ceo_ai.animal_current_scope        -- add partition_label to the per-goat row
--   ceo_ai.shed_capacity_current        -- add per-partition occupancy rows
--   ceo_ai.counts_movement_daily        -- add partition_label to birth/death branches
--   ceo_ai.vaccination_operator_status  -- GROUP BY the partition_label already on
--                                           vaccination_drive_assignments
--
-- DELIBERATELY NOT FIXED (documented gap, see docs/ceo-ai/coverage-matrix.md):
--   ceo_ai.vaccination_shed_status and ceo_ai.vaccination_dose_pickup read
--   vaccination_eligibility_rollups / obligation_instances, which are SHED-GRAIN
--   BY SCHEMA -- there is no partition column on either table. Fixing those two
--   needs a rollup-grain migration + backfill, deliberately deferred to a
--   separate change. This migration does NOT touch them and does NOT fake
--   per-partition numbers by dividing or guessing.
--
-- Every raw partition label read here is passed through NULLIF(..., 'whole') so
-- the "not partitioned" sentinel never leaks into a reporting column -- mirrors
-- oploc.NormalizePartition without changing the stored convention ('N' vs
-- 'Part N' both pass through verbatim; display normalization is a client concern
-- per oploc.OperationalLocation.Display()).
--
-- Lock-safety: every statement below is CREATE OR REPLACE VIEW, which takes only
-- an ACCESS EXCLUSIVE lock for the instant of the catalog swap (no table rewrite,
-- no data movement) and is safe to run against a live tenant.

-- ===========================================================================
-- 1. ceo_ai.animal_current_scope -- add partition_label to the per-goat row.
-- ===========================================================================
CREATE OR REPLACE VIEW ceo_ai.animal_current_scope AS
SELECT
    g.tenant_id                                   AS tenant_id,
    g.goat_id                                     AS animal_id,
    g.park_id                                     AS park_id,
    pk.name                                       AS park_label,
    g.shed_id                                     AS shed_id,
    sh.name                                       AS shed_label,
    g.species                                     AS species,
    g.management_stage                            AS management_stage,
    g.lifecycle_status                            AS lifecycle_status,
    g.sex                                         AS sex,
    COALESCE(b.canonical_name, g.breed)           AS breed,
    -- age at Asia/Kolkata business day; dob preferred, approx_dob fallback
    (((now() AT TIME ZONE 'Asia/Kolkata')::date) - COALESCE(g.dob, g.approx_dob))::int
                                                  AS age_days,
    -- partition_review: goat_shed_partitions PK is (tenant_id, goat_id) -- at
    -- most one row per goat, so this LEFT JOIN cannot fan out animal_current_scope's
    -- per-goat grain. NULLIF(...,'whole') keeps the "not partitioned" sentinel out
    -- of the reporting column; NULL here means "no partition", never a merge bug.
    -- Appended as the LAST column: CREATE OR REPLACE VIEW cannot reorder or
    -- insert columns mid-list, only append.
    NULLIF(gsp.partition_label, 'whole')           AS partition_label
FROM goats g
LEFT JOIN locations pk ON pk.location_id = g.park_id
LEFT JOIN locations sh ON sh.location_id = g.shed_id
LEFT JOIN breeds    b  ON b.breed_id     = g.breed_id
LEFT JOIN goat_shed_partitions gsp ON gsp.tenant_id = g.tenant_id AND gsp.goat_id = g.goat_id;

-- ===========================================================================
-- 2. ceo_ai.shed_capacity_current -- occupancy vs capacity per shed, PLUS one
--    additional row per (shed, partition) reporting occupancy scoped to that
--    partition. Capacity/variance/status stay shed-grain (there is no
--    per-partition capacity column anywhere in the schema -- shed_profiles.capacity
--    is the only stored capacity and it is a whole-shed figure), so a partition
--    row shows the SAME capacity/variance/status as its parent shed, with only
--    `animals` scoped to that partition. The original bare-shed row is UNCHANGED
--    (still the whole-shed total across every partition) so every existing
--    caller keeps its current behavior; partition rows are additive.
-- ===========================================================================
CREATE OR REPLACE VIEW ceo_ai.shed_capacity_current AS
WITH occ AS (
    SELECT tenant_id, shed_id, COUNT(*)::bigint AS animals
    FROM goats
    WHERE shed_id IS NOT NULL
      AND lifecycle_status NOT IN ('dead','sold','culled','transferred','lost','merged','inactive')
    GROUP BY tenant_id, shed_id
),
-- partition_review: producer key = (tenant_id, shed_id, NULLIF(gsp.partition_label,'whole'));
-- consumer (final SELECT) matches on the identical three-column key via po. The
-- goat_shed_partitions join is 1:{0,1} per goat (its PK is (tenant_id, goat_id)),
-- so this GROUP BY cannot fan out occupancy -- each goat contributes to exactly
-- one partition bucket.
part_occ AS (
    SELECT g.tenant_id, g.shed_id, NULLIF(gsp.partition_label, 'whole') AS partition_label,
           COUNT(*)::bigint AS animals
    FROM goats g
    JOIN goat_shed_partitions gsp ON gsp.tenant_id = g.tenant_id AND gsp.goat_id = g.goat_id
    WHERE g.shed_id IS NOT NULL
      AND g.lifecycle_status NOT IN ('dead','sold','culled','transferred','lost','merged','inactive')
      AND NULLIF(gsp.partition_label, 'whole') IS NOT NULL
    GROUP BY g.tenant_id, g.shed_id, NULLIF(gsp.partition_label, 'whole')
),
partitions AS (
    SELECT DISTINCT tenant_id, shed_id, NULLIF(partition_label, 'whole') AS partition_label
    FROM goat_shed_partitions
    WHERE NULLIF(partition_label, 'whole') IS NOT NULL
),
owner_seat AS (
    SELECT wp.tenant_id, wp.scope_id AS shed_id, wm.display_name AS owner_label
    FROM workforce_positions wp
    JOIN workforce_members wm ON wm.workforce_member_id = wp.workforce_member_id
    WHERE wp.scope_type = 'shed' AND wp.is_backup_slot = false
      AND wp.status = 'active'
      AND now() >= wp.valid_from AND now() < COALESCE(wp.valid_to, 'infinity'::timestamptz)
),
backup_seat AS (
    SELECT wp.tenant_id, wp.scope_id AS shed_id, wm.display_name AS backup_label
    FROM workforce_positions wp
    JOIN workforce_members wm ON wm.workforce_member_id = wp.workforce_member_id
    WHERE wp.scope_type = 'shed' AND wp.is_backup_slot = true
      AND wp.status = 'active'
      AND now() >= wp.valid_from AND now() < COALESCE(wp.valid_to, 'infinity'::timestamptz)
),
shed_grains AS (
    -- the pre-existing bare-shed row: partition_label NULL, whole-shed total.
    SELECT s.tenant_id, s.location_id AS shed_id, NULL::text AS partition_label
    FROM locations s
    WHERE s.location_type = 'shed'
    UNION ALL
    -- one additional row per real partition that shed has.
    SELECT p.tenant_id, p.shed_id, p.partition_label
    FROM partitions p
)
SELECT
    s.tenant_id                                   AS tenant_id,
    pk.name                                       AS park_label,
    s.name                                        AS shed_label,
    CASE WHEN sg.partition_label IS NULL
         THEN COALESCE(occ.animals, 0)
         ELSE COALESCE(po.animals, 0)
    END                                           AS animals,
    sp.capacity                                   AS capacity,
    (sp.capacity - CASE WHEN sg.partition_label IS NULL
                        THEN COALESCE(occ.animals, 0)
                        ELSE COALESCE(po.animals, 0)
                   END)                           AS variance,
    CASE
        WHEN sp.capacity IS NULL THEN 'unknown_capacity'
        WHEN (CASE WHEN sg.partition_label IS NULL THEN COALESCE(occ.animals, 0) ELSE COALESCE(po.animals, 0) END) > sp.capacity THEN 'over_capacity'
        WHEN (CASE WHEN sg.partition_label IS NULL THEN COALESCE(occ.animals, 0) ELSE COALESCE(po.animals, 0) END) = sp.capacity THEN 'at_capacity'
        ELSE 'under_capacity'
    END                                           AS status,
    o.owner_label                                 AS owner_label,
    bk.backup_label                                AS backup_label,
    -- appended last: CREATE OR REPLACE VIEW can only add trailing columns.
    sg.partition_label                            AS partition_label
FROM shed_grains sg
JOIN locations s ON s.location_id = sg.shed_id AND s.tenant_id = sg.tenant_id
LEFT JOIN locations     pk ON pk.location_id = s.parent_location_id
LEFT JOIN shed_profiles sp ON sp.location_id = s.location_id
LEFT JOIN occ         ON occ.tenant_id = sg.tenant_id AND occ.shed_id = sg.shed_id
LEFT JOIN part_occ  po ON po.tenant_id = sg.tenant_id AND po.shed_id = sg.shed_id AND po.partition_label = sg.partition_label
LEFT JOIN owner_seat  o  ON o.tenant_id  = s.tenant_id AND o.shed_id  = s.location_id
LEFT JOIN backup_seat bk ON bk.tenant_id = s.tenant_id AND bk.shed_id = s.location_id
WHERE s.location_type = 'shed';

-- ===========================================================================
-- 3. ceo_ai.counts_movement_daily -- add partition_label to the birth/death
--    branches, which read `goats` per-animal and can therefore join
--    goat_shed_partitions without fan-out (same 1:{0,1} PK argument as above).
--    The transfer/shift/approval branches read shifting_event_impacts, which is
--    aggregated per (event, breed, stage) -- NOT per goat -- so there is no
--    per-goat partition to attribute there; those branches keep emitting a NULL
--    partition_label (documented, not silently dropped) and continue rolling up
--    at shed grain only. See docs/ceo-ai/coverage-matrix.md for this boundary.
-- ===========================================================================
CREATE OR REPLACE VIEW ceo_ai.counts_movement_daily AS
WITH events AS (
    SELECT tenant_id, shed_id, event_date, partition_label,
           births, 0::bigint AS deaths, 0::bigint AS transfers_out,
           0::bigint AS shifts_in, 0::bigint AS shifts_out, 0::bigint AS approvals_pending
    FROM (
        -- partition_review: producer group key = (tenant_id, shed_id, event_date,
        -- NULLIF(gsp.partition_label,'whole')); goat_shed_partitions PK is
        -- (tenant_id, goat_id), 1:{0,1} per goat, so the join cannot fan out births.
        SELECT g.tenant_id, g.shed_id,
               (COALESCE(g.entry_date, (g.created_at AT TIME ZONE 'Asia/Kolkata')::date)) AS event_date,
               NULLIF(gsp.partition_label, 'whole') AS partition_label,
               COUNT(*)::bigint AS births
        FROM goats g
        LEFT JOIN goat_shed_partitions gsp ON gsp.tenant_id = g.tenant_id AND gsp.goat_id = g.goat_id
        WHERE g.origin_type = 'birth'
        GROUP BY 1,2,3,4
    ) b
    UNION ALL
    SELECT tenant_id, shed_id, event_date, partition_label,
           0, deaths, 0, 0, 0, 0
    FROM (
        SELECT g.tenant_id, g.shed_id,
               (g.exited_at AT TIME ZONE 'Asia/Kolkata')::date AS event_date,
               NULLIF(gsp.partition_label, 'whole') AS partition_label,
               COUNT(*)::bigint AS deaths
        FROM goats g
        LEFT JOIN goat_shed_partitions gsp ON gsp.tenant_id = g.tenant_id AND gsp.goat_id = g.goat_id
        WHERE g.exited_at IS NOT NULL AND g.exit_reason = 'died'
        GROUP BY 1,2,3,4
    ) d
    UNION ALL
    SELECT tenant_id, shed_id, event_date, NULL::text AS partition_label,
           0, 0, transfers_out, 0, 0, 0
    FROM (
        -- cross-park exit is terminal (transferred/sold); attribute to source shed.
        -- shifting_event_impacts is per (event, breed, stage) -- no per-goat
        -- partition available here (documented gap above).
        SELECT se.tenant_id, se.source_shed_id AS shed_id,
               (se.applied_at AT TIME ZONE 'Asia/Kolkata')::date AS event_date,
               SUM(im.head_count)::bigint AS transfers_out
        FROM shifting_events se
        JOIN shifting_event_impacts im ON im.shifting_event_id = se.shifting_event_id
        WHERE se.applied_at IS NOT NULL AND se.event_status = 'completed'
          AND se.category IN ('transfer','sale','exit')
        GROUP BY 1,2,3
    ) t
    UNION ALL
    SELECT tenant_id, shed_id, event_date, NULL::text AS partition_label,
           0, 0, 0, shifts_in, 0, 0
    FROM (
        SELECT se.tenant_id, se.destination_shed_id AS shed_id,
               (se.applied_at AT TIME ZONE 'Asia/Kolkata')::date AS event_date,
               SUM(im.head_count)::bigint AS shifts_in
        FROM shifting_events se
        JOIN shifting_event_impacts im ON im.shifting_event_id = se.shifting_event_id
        WHERE se.applied_at IS NOT NULL AND se.event_status = 'completed'
        GROUP BY 1,2,3
    ) si
    UNION ALL
    SELECT tenant_id, shed_id, event_date, NULL::text AS partition_label,
           0, 0, 0, 0, shifts_out, 0
    FROM (
        SELECT se.tenant_id, se.source_shed_id AS shed_id,
               (se.applied_at AT TIME ZONE 'Asia/Kolkata')::date AS event_date,
               SUM(im.head_count)::bigint AS shifts_out
        FROM shifting_events se
        JOIN shifting_event_impacts im ON im.shifting_event_id = se.shifting_event_id
        WHERE se.applied_at IS NOT NULL AND se.event_status = 'completed'
        GROUP BY 1,2,3
    ) so
    UNION ALL
    SELECT tenant_id, shed_id, event_date, NULL::text AS partition_label,
           0, 0, 0, 0, 0, approvals_pending
    FROM (
        SELECT ca.tenant_id, se.source_shed_id AS shed_id,
               (ca.raised_at AT TIME ZONE 'Asia/Kolkata')::date AS event_date,
               COUNT(*)::bigint AS approvals_pending
        FROM counts_approval_requests ca
        LEFT JOIN shifting_events se ON se.shifting_event_id = ca.shifting_event_id
        WHERE ca.status = 'pending'
        GROUP BY 1,2,3
    ) a
)
SELECT
    e.tenant_id                                   AS tenant_id,
    e.event_date                                  AS event_date,
    pk.name                                       AS park_label,
    sh.name                                       AS shed_label,
    SUM(e.births)::bigint                         AS births,
    SUM(e.deaths)::bigint                         AS deaths,
    SUM(e.transfers_out)::bigint                  AS transfers_out,
    SUM(e.shifts_in)::bigint                      AS shifts_in,
    SUM(e.shifts_out)::bigint                     AS shifts_out,
    SUM(e.approvals_pending)::bigint              AS approvals_pending,
    -- appended last: CREATE OR REPLACE VIEW can only add trailing columns.
    e.partition_label                             AS partition_label
FROM events e
LEFT JOIN locations sh ON sh.location_id = e.shed_id
LEFT JOIN locations pk ON pk.location_id = sh.parent_location_id
GROUP BY e.tenant_id, e.event_date, e.shed_id, e.partition_label, sh.name, pk.name;

-- ===========================================================================
-- 4. ceo_ai.vaccination_operator_status -- GROUP BY the partition_label that
--    vaccination_drive_assignments already stores but this view previously
--    ignored, so a physical shed's two partitions assigned to the SAME operator
--    on the SAME day silently merged into one row.
--    partition_review: producer key adds a.partition_label to the existing
--    (tenant_id, operator_id, park_id, shed_id, planned_date) GROUP BY; consumer
--    (the outer SELECT) carries it straight through with no aggregation, so no
--    fan-out is introduced. The per-operator-per-day capacity/utilization window
--    functions stay PARTITION BY (tenant_id, operator_id, planned_date) --
--    UNCHANGED -- because capacity is a whole-day, cross-shed, cross-partition
--    cap, never a per-partition one.
-- ===========================================================================
CREATE OR REPLACE VIEW ceo_ai.vaccination_operator_status AS
WITH assigned AS (
    SELECT
        a.tenant_id,
        a.operator_id,
        a.park_id,
        a.shed_id,
        NULLIF(a.partition_label, 'whole')                                                  AS partition_label,
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
    GROUP BY a.tenant_id, a.operator_id, a.park_id, a.shed_id, NULLIF(a.partition_label, 'whole'), a.planned_date
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
    -- the operator's WHOLE-day assigned total (across every shed AND partition
    -- that day), the correct numerator for a per-operator-per-day utilization
    -- ratio. PARTITION BY intentionally excludes partition_label.
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
    END                                                                   AS next_action,
    -- appended last: CREATE OR REPLACE VIEW can only add trailing columns.
    s.partition_label                                                     AS partition_label
FROM assigned s
LEFT JOIN public.workforce_members wm ON wm.workforce_member_id = s.operator_id
LEFT JOIN public.locations         pk ON pk.location_id = s.park_id
LEFT JOIN public.locations         sh ON sh.location_id = s.shed_id
LEFT JOIN cap ON cap.tenant_id = s.tenant_id;

-- +goose Down
-- NOTE (found while rolling this back on a live local DB): CREATE OR REPLACE VIEW
-- CANNOT DROP COLUMNS. Because the Up added partition_label to these views, a Down
-- written as CREATE OR REPLACE fails with "cannot drop columns from view" and the
-- rollback silently leaves the new columns in place. Each view is therefore DROPped
-- first and recreated at its pre-partition shape. Views are dropped in dependency
-- order; nothing else in the schema depends on them (ceo_ai is a leaf reporting
-- namespace -- core operator paths may not read it, per
-- docs/decisions/ceo-ai-reporting-boundary.md).
DROP VIEW IF EXISTS ceo_ai.vaccination_operator_status;
DROP VIEW IF EXISTS ceo_ai.counts_movement_daily;
DROP VIEW IF EXISTS ceo_ai.shed_capacity_current;
DROP VIEW IF EXISTS ceo_ai.animal_current_scope;

-- Restore the pre-partition-fix view definitions exactly as they were before
-- this migration (migration 000001 baseline / 000030 cube-source pass).

CREATE VIEW ceo_ai.animal_current_scope AS
SELECT
    g.tenant_id                                   AS tenant_id,
    g.goat_id                                     AS animal_id,
    g.park_id                                     AS park_id,
    pk.name                                       AS park_label,
    g.shed_id                                     AS shed_id,
    sh.name                                       AS shed_label,
    g.species                                     AS species,
    g.management_stage                            AS management_stage,
    g.lifecycle_status                            AS lifecycle_status,
    g.sex                                         AS sex,
    COALESCE(b.canonical_name, g.breed)           AS breed,
    (((now() AT TIME ZONE 'Asia/Kolkata')::date) - COALESCE(g.dob, g.approx_dob))::int
                                                  AS age_days
FROM goats g
LEFT JOIN locations pk ON pk.location_id = g.park_id
LEFT JOIN locations sh ON sh.location_id = g.shed_id
LEFT JOIN breeds    b  ON b.breed_id     = g.breed_id;

CREATE VIEW ceo_ai.shed_capacity_current AS
WITH occ AS (
    SELECT tenant_id, shed_id, COUNT(*)::bigint AS animals
    FROM goats
    WHERE shed_id IS NOT NULL
      AND lifecycle_status NOT IN ('dead','sold','culled','transferred','lost','merged','inactive')
    GROUP BY tenant_id, shed_id
),
owner_seat AS (
    SELECT wp.tenant_id, wp.scope_id AS shed_id, wm.display_name AS owner_label
    FROM workforce_positions wp
    JOIN workforce_members wm ON wm.workforce_member_id = wp.workforce_member_id
    WHERE wp.scope_type = 'shed' AND wp.is_backup_slot = false
      AND wp.status = 'active'
      AND now() >= wp.valid_from AND now() < COALESCE(wp.valid_to, 'infinity'::timestamptz)
),
backup_seat AS (
    SELECT wp.tenant_id, wp.scope_id AS shed_id, wm.display_name AS backup_label
    FROM workforce_positions wp
    JOIN workforce_members wm ON wm.workforce_member_id = wp.workforce_member_id
    WHERE wp.scope_type = 'shed' AND wp.is_backup_slot = true
      AND wp.status = 'active'
      AND now() >= wp.valid_from AND now() < COALESCE(wp.valid_to, 'infinity'::timestamptz)
)
SELECT
    s.tenant_id                                   AS tenant_id,
    pk.name                                       AS park_label,
    s.name                                        AS shed_label,
    COALESCE(occ.animals, 0)                      AS animals,
    sp.capacity                                   AS capacity,
    (sp.capacity - COALESCE(occ.animals, 0))      AS variance,
    CASE
        WHEN sp.capacity IS NULL THEN 'unknown_capacity'
        WHEN COALESCE(occ.animals, 0) > sp.capacity THEN 'over_capacity'
        WHEN COALESCE(occ.animals, 0) = sp.capacity THEN 'at_capacity'
        ELSE 'under_capacity'
    END                                           AS status,
    o.owner_label                                 AS owner_label,
    bk.backup_label                                AS backup_label
FROM locations s
LEFT JOIN locations     pk ON pk.location_id = s.parent_location_id
LEFT JOIN shed_profiles sp ON sp.location_id = s.location_id
LEFT JOIN occ         ON occ.tenant_id = s.tenant_id AND occ.shed_id = s.location_id
LEFT JOIN owner_seat  o  ON o.tenant_id  = s.tenant_id AND o.shed_id  = s.location_id
LEFT JOIN backup_seat bk ON bk.tenant_id = s.tenant_id AND bk.shed_id = s.location_id
WHERE s.location_type = 'shed';

CREATE VIEW ceo_ai.counts_movement_daily AS
WITH events AS (
    SELECT tenant_id, shed_id, event_date,
           births, 0::bigint AS deaths, 0::bigint AS transfers_out,
           0::bigint AS shifts_in, 0::bigint AS shifts_out, 0::bigint AS approvals_pending
    FROM (
        SELECT tenant_id, shed_id,
               (COALESCE(entry_date, (created_at AT TIME ZONE 'Asia/Kolkata')::date)) AS event_date,
               COUNT(*)::bigint AS births
        FROM goats
        WHERE origin_type = 'birth'
        GROUP BY 1,2,3
    ) b
    UNION ALL
    SELECT tenant_id, shed_id, event_date,
           0, deaths, 0, 0, 0, 0
    FROM (
        SELECT tenant_id, shed_id,
               (exited_at AT TIME ZONE 'Asia/Kolkata')::date AS event_date,
               COUNT(*)::bigint AS deaths
        FROM goats
        WHERE exited_at IS NOT NULL AND exit_reason = 'died'
        GROUP BY 1,2,3
    ) d
    UNION ALL
    SELECT tenant_id, shed_id, event_date,
           0, 0, transfers_out, 0, 0, 0
    FROM (
        SELECT se.tenant_id, se.source_shed_id AS shed_id,
               (se.applied_at AT TIME ZONE 'Asia/Kolkata')::date AS event_date,
               SUM(im.head_count)::bigint AS transfers_out
        FROM shifting_events se
        JOIN shifting_event_impacts im ON im.shifting_event_id = se.shifting_event_id
        WHERE se.applied_at IS NOT NULL AND se.event_status = 'completed'
          AND se.category IN ('transfer','sale','exit')
        GROUP BY 1,2,3
    ) t
    UNION ALL
    SELECT tenant_id, shed_id, event_date,
           0, 0, 0, shifts_in, 0, 0
    FROM (
        SELECT se.tenant_id, se.destination_shed_id AS shed_id,
               (se.applied_at AT TIME ZONE 'Asia/Kolkata')::date AS event_date,
               SUM(im.head_count)::bigint AS shifts_in
        FROM shifting_events se
        JOIN shifting_event_impacts im ON im.shifting_event_id = se.shifting_event_id
        WHERE se.applied_at IS NOT NULL AND se.event_status = 'completed'
        GROUP BY 1,2,3
    ) si
    UNION ALL
    SELECT tenant_id, shed_id, event_date,
           0, 0, 0, 0, shifts_out, 0
    FROM (
        SELECT se.tenant_id, se.source_shed_id AS shed_id,
               (se.applied_at AT TIME ZONE 'Asia/Kolkata')::date AS event_date,
               SUM(im.head_count)::bigint AS shifts_out
        FROM shifting_events se
        JOIN shifting_event_impacts im ON im.shifting_event_id = se.shifting_event_id
        WHERE se.applied_at IS NOT NULL AND se.event_status = 'completed'
        GROUP BY 1,2,3
    ) so
    UNION ALL
    SELECT tenant_id, shed_id, event_date,
           0, 0, 0, 0, 0, approvals_pending
    FROM (
        SELECT ca.tenant_id, se.source_shed_id AS shed_id,
               (ca.raised_at AT TIME ZONE 'Asia/Kolkata')::date AS event_date,
               COUNT(*)::bigint AS approvals_pending
        FROM counts_approval_requests ca
        LEFT JOIN shifting_events se ON se.shifting_event_id = ca.shifting_event_id
        WHERE ca.status = 'pending'
        GROUP BY 1,2,3
    ) a
)
SELECT
    e.tenant_id                                   AS tenant_id,
    e.event_date                                  AS event_date,
    pk.name                                       AS park_label,
    sh.name                                       AS shed_label,
    SUM(e.births)::bigint                         AS births,
    SUM(e.deaths)::bigint                         AS deaths,
    SUM(e.transfers_out)::bigint                  AS transfers_out,
    SUM(e.shifts_in)::bigint                      AS shifts_in,
    SUM(e.shifts_out)::bigint                     AS shifts_out,
    SUM(e.approvals_pending)::bigint              AS approvals_pending
FROM events e
LEFT JOIN locations sh ON sh.location_id = e.shed_id
LEFT JOIN locations pk ON pk.location_id = sh.parent_location_id
GROUP BY e.tenant_id, e.event_date, e.shed_id, sh.name, pk.name;

CREATE VIEW ceo_ai.vaccination_operator_status AS
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
