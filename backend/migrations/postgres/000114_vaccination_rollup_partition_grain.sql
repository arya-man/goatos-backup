-- +goose Up
-- projection-review: membership=vaccination_eligibility_rollups keeps its existing per-tenant full-recompute membership (delete+reinsert from goats/location_operational_attributes/shed_profiles/animal_stage_lookup, unchanged); the ONLY change is that the recompute now also groups by the normalized partition key sourced 1:{0,1} from goat_shed_partitions (PK (tenant_id, goat_id)); group_key=previous grain (tenant_id, park_id, shed_id, species, management_stage, sex, breed, health_status, usable_for_vaccination) PLUS NULLIF(gsp.partition_label,'whole'), which splits a shed's rollup rows across its partitions and never multiplies them; join_cardinality=the added goat_shed_partitions join is on its own primary key, 1:{0,1}, no fan-out; pagination=none, this is a whole-tenant read model rebuilt off the request path; scope=tenant_id, with park/shed/partition exposed as columns
--
-- Closes the documented gap from migration 000111 (see its header and
-- docs/ceo-ai/coverage-matrix.md -> "Known gap"): ceo_ai.vaccination_shed_status
-- and ceo_ai.vaccination_dose_pickup answered at PARENT-SHED grain because
-- vaccination_eligibility_rollups had no partition column, so "how many
-- animals are due at Castro 1" answered with the whole-Castro total. That
-- table is this migration's owner; see backend/internal/platform/oploc for the
-- shared OperationalLocation primitive this closes the gap against.
--
-- OperationalLocation = park + physical shed + OPTIONAL partition. goats.shed_id
-- is always the PARENT physical shed; goat_shed_partitions (tenant_id, goat_id)
-- carries the actual sub-location. Not every shed has partitions -- a
-- non-partitioned shed must keep rendering as the bare shed name, never a
-- synthetic "whole" location; NULLIF(...,'whole') is used everywhere below so
-- the matching sentinel never leaks into a reporting column.
--
-- 1. vaccination_eligibility_rollups gains partition_label. The grain is
--    ADDITIVE and NEVER destructive: this ALTER only adds a nullable column,
--    and the row content is rebuilt (not migrated) the next time the existing
--    projector runs -- backend/cmd/vaccination-eligibility-rollup-recompute,
--    the SAME command that already fully rebuilds this table today (see its
--    package doc). No new command is introduced; the recompute SQL inside
--    Repository.RecomputeEligibilityRollup (backend/internal/vaccination/
--    adapters/postgres/repository.go) now groups by the normalized partition
--    key in the same delete+reinsert transaction it already runs. Until that
--    recompute is next invoked (it already runs after every seed/import per
--    docs/runbooks/initial-seed-migration-coupling.md, and is safe to run any
--    time), every existing row simply reads partition_label = NULL, which is
--    the correct "not yet split" state, not a data-loss state -- the shed-grain
--    total these rows have always carried remains exactly correct until the
--    next recompute narrows it.
-- 2. The grain-uniqueness index is rebuilt to include the partition so two
--    partitions of one shed with identical species/stage/sex/breed/health can
--    coexist as distinct rows without violating uniqueness.
-- 3. ceo_ai.vaccination_shed_status and ceo_ai.vaccination_dose_pickup are
--    rebuilt to emit the pre-existing bare-shed row UNCHANGED (still the
--    whole-shed total, exactly as every existing caller already reads it) PLUS
--    one additional row per partition, mirroring the additive convention
--    migration 000111 established for ceo_ai.shed_capacity_current. Obligation
--    counts (due/done/overdue/planned_sessions) are resolved per-partition by
--    joining obligation_instances to goat_shed_partitions on
--    obligation_instances.target_id -- a vaccination obligation's target_type is
--    always 'goat', so target_id IS the goat_id, and goat_shed_partitions' PK
--    is (tenant_id, goat_id) -- 1:{0,1} per goat -- so this join cannot fan out
--    the COUNT/FILTER aggregates. obligation_instances itself is NOT altered;
--    the partition is resolved via that join, never a schema change to
--    obligation_instances.
--
-- Lock-safety: the ALTER TABLE ... ADD COLUMN (nullable, no default) takes a
-- brief ACCESS EXCLUSIVE lock with NO TABLE REWRITE (Postgres 11+; this catalog
-- targets a modern Postgres). The index rebuild uses a plain CREATE/DROP INDEX
-- (not CONCURRENTLY) inside this migration, matching the existing convention in
-- 000001/000111 for this table's already-small row count (one row per
-- tenant/shed/species/stage/sex/breed/health grain, not per animal) -- see
-- Down for the exact rollback shape. The view replacements are
-- replace-in-place, ACCESS EXCLUSIVE for the instant of the catalog swap only,
-- no data movement.

ALTER TABLE public.vaccination_eligibility_rollups
    ADD COLUMN IF NOT EXISTS partition_label text;

DROP INDEX IF EXISTS public.vaccination_eligibility_rollups_grain_uidx;

CREATE UNIQUE INDEX vaccination_eligibility_rollups_grain_uidx ON public.vaccination_eligibility_rollups
    USING btree (
        tenant_id,
        COALESCE(park_id, '00000000-0000-0000-0000-000000000000'::uuid),
        COALESCE(shed_id, '00000000-0000-0000-0000-000000000000'::uuid),
        COALESCE(partition_label, ''),
        species, management_stage, sex, breed, health_status, usable_for_vaccination
    );

-- ===========================================================================
-- ceo_ai.vaccination_shed_status -- add the partition dimension.
-- ===========================================================================
-- projection-review: membership=identical shed membership as before (every 'shed' location row); the ONLY change is one additional row per (shed, partition) attested by goat_shed_partitions, layered on top of the unchanged bare-shed row; group_key=obligation side keys on (tenant_id, scope_id, NULLIF(gsp.partition_label,'whole')) added to the existing (tenant_id, scope_id); rollup side keys on (tenant_id, shed_id, NULLIF(partition_label,'whole')) added to the existing (tenant_id, shed_id); join_cardinality=goat_shed_partitions joined on obligation_instances.target_id (=goat_id when target_type='goat') is 1:{0,1} per goat (PK (tenant_id, goat_id)), so neither added join can fan out a COUNT/SUM aggregate; pagination=none, whole-tenant reporting view; scope=tenant_id, with park/shed/partition exposed as columns
CREATE OR REPLACE VIEW ceo_ai.vaccination_shed_status AS
WITH obl AS (
    SELECT
        tenant_id,
        scope_id AS shed_id,
        COUNT(*) FILTER (WHERE status IN ('scheduled','due','in_progress'))                          AS due,
        COUNT(*) FILTER (WHERE status IN ('completed','accepted'))                                    AS done,
        COUNT(*) FILTER (WHERE status IN ('scheduled','due','in_progress','missed')
                          AND window_end < (now() AT TIME ZONE 'Asia/Kolkata')::date)                 AS overdue,
        MIN(due_at) FILTER (WHERE status IN ('scheduled','due','in_progress'))                        AS next_due,
        COUNT(DISTINCT batch_id) FILTER (WHERE batch_id IS NOT NULL)                                  AS planned_sessions
    FROM obligation_instances
    WHERE scope_type = 'shed'
    GROUP BY tenant_id, scope_id
),
-- partition_review: producer key = (tenant_id, scope_id, NULLIF(gsp.partition_label,'whole'));
-- obligation_instances.target_id is the goat_id for every target_type='goat' row (a
-- vaccination obligation is always per-animal); goat_shed_partitions PK is
-- (tenant_id, goat_id), 1:{0,1} per goat, so this join cannot fan out the FILTER aggregates.
obl_part AS (
    SELECT
        oi.tenant_id,
        oi.scope_id AS shed_id,
        NULLIF(gsp.partition_label, 'whole')                                                           AS partition_label,
        COUNT(*) FILTER (WHERE oi.status IN ('scheduled','due','in_progress'))                          AS due,
        COUNT(*) FILTER (WHERE oi.status IN ('completed','accepted'))                                   AS done,
        COUNT(*) FILTER (WHERE oi.status IN ('scheduled','due','in_progress','missed')
                          AND oi.window_end < (now() AT TIME ZONE 'Asia/Kolkata')::date)                AS overdue,
        MIN(oi.due_at) FILTER (WHERE oi.status IN ('scheduled','due','in_progress'))                    AS next_due,
        COUNT(DISTINCT oi.batch_id) FILTER (WHERE oi.batch_id IS NOT NULL)                              AS planned_sessions
    FROM obligation_instances oi
    JOIN goat_shed_partitions gsp ON gsp.tenant_id = oi.tenant_id AND gsp.goat_id = oi.target_id
    WHERE oi.scope_type = 'shed' AND oi.target_type = 'goat'
      AND NULLIF(gsp.partition_label, 'whole') IS NOT NULL
    GROUP BY oi.tenant_id, oi.scope_id, NULLIF(gsp.partition_label, 'whole')
),
anim AS (
    SELECT tenant_id, shed_id, SUM(animal_count) FILTER (WHERE usable_for_vaccination)::bigint AS animals
    FROM vaccination_eligibility_rollups
    GROUP BY tenant_id, shed_id
),
-- partition_review: producer key = (tenant_id, shed_id, NULLIF(partition_label,'whole'));
-- vaccination_eligibility_rollups now carries partition_label per grain
-- (migration 000114 recompute), so this is a straight GROUP BY extension on the
-- rollup's own rows, not a join -- no fan-out risk.
anim_part AS (
    SELECT tenant_id, shed_id, NULLIF(partition_label, 'whole') AS partition_label,
           SUM(animal_count) FILTER (WHERE usable_for_vaccination)::bigint AS animals
    FROM vaccination_eligibility_rollups
    WHERE NULLIF(partition_label, 'whole') IS NOT NULL
    GROUP BY tenant_id, shed_id, NULLIF(partition_label, 'whole')
),
owner_seat AS (
    SELECT wp.tenant_id, wp.scope_id AS shed_id, wm.display_name AS manager_label
    FROM workforce_positions wp
    JOIN workforce_members wm ON wm.workforce_member_id = wp.workforce_member_id
    WHERE wp.scope_type = 'shed' AND wp.is_backup_slot = false AND wp.status = 'active'
      AND now() >= wp.valid_from AND now() < COALESCE(wp.valid_to, 'infinity'::timestamptz)
),
backup_seat AS (
    SELECT wp.tenant_id, wp.scope_id AS shed_id, wm.display_name AS backup_label
    FROM workforce_positions wp
    JOIN workforce_members wm ON wm.workforce_member_id = wp.workforce_member_id
    WHERE wp.scope_type = 'shed' AND wp.is_backup_slot = true AND wp.status = 'active'
      AND now() >= wp.valid_from AND now() < COALESCE(wp.valid_to, 'infinity'::timestamptz)
),
-- partitions attested by a live goat's partition assignment in this shed --
-- same source-of-truth convention as ceo_ai.shed_capacity_current's `partitions`
-- CTE (migration 000111).
partitions AS (
    SELECT DISTINCT tenant_id, shed_id, NULLIF(partition_label, 'whole') AS partition_label
    FROM goat_shed_partitions
    WHERE NULLIF(partition_label, 'whole') IS NOT NULL
),
shed_grains AS (
    -- the pre-existing bare-shed row: partition_label NULL, whole-shed total,
    -- UNCHANGED from the pre-partition view.
    SELECT s.tenant_id, s.location_id AS shed_id, NULL::text AS partition_label
    FROM locations s
    WHERE s.location_type = 'shed'
    UNION ALL
    -- one additional row per real partition that shed has.
    SELECT p.tenant_id, p.shed_id, p.partition_label
    FROM partitions p
)
SELECT
    sg.tenant_id                                                    AS tenant_id,
    pk.name                                                        AS park_label,
    s.name                                                         AS shed_label,
    (CASE WHEN sg.partition_label IS NULL THEN COALESCE(anim.animals, 0) ELSE COALESCE(ap.animals, 0) END)::bigint AS animals,
    (CASE WHEN sg.partition_label IS NULL THEN COALESCE(obl.due, 0) ELSE COALESCE(op.due, 0) END)::bigint          AS due,
    (CASE WHEN sg.partition_label IS NULL THEN COALESCE(obl.done, 0) ELSE COALESCE(op.done, 0) END)::bigint        AS done,
    (CASE WHEN sg.partition_label IS NULL THEN COALESCE(obl.planned_sessions, 0) ELSE COALESCE(op.planned_sessions, 0) END)::bigint AS planned_sessions,
    ((CASE WHEN sg.partition_label IS NULL THEN obl.next_due ELSE op.next_due END) AT TIME ZONE 'Asia/Kolkata')::date AS next_due_date,
    om.manager_label                                               AS manager_label,
    bk.backup_label                                                AS backup_label,
    CASE
        WHEN (CASE WHEN sg.partition_label IS NULL THEN COALESCE(obl.overdue, 0) ELSE COALESCE(op.overdue, 0) END) > 0 THEN 'overdue'
        WHEN (CASE WHEN sg.partition_label IS NULL THEN COALESCE(obl.due, 0)     ELSE COALESCE(op.due, 0) END)     > 0 THEN 'due'
        WHEN (CASE WHEN sg.partition_label IS NULL THEN COALESCE(obl.done, 0)    ELSE COALESCE(op.done, 0) END)    > 0 THEN 'complete'
        ELSE 'no_work_due'
    END                                                           AS status,
    -- appended last: a replace-in-place view rebuild can only add trailing columns.
    sg.partition_label                                             AS partition_label
FROM shed_grains sg
JOIN locations s ON s.location_id = sg.shed_id AND s.tenant_id = sg.tenant_id
LEFT JOIN locations   pk ON pk.location_id = s.parent_location_id
LEFT JOIN obl         ON obl.tenant_id  = sg.tenant_id AND obl.shed_id  = sg.shed_id
LEFT JOIN obl_part op ON op.tenant_id = sg.tenant_id AND op.shed_id = sg.shed_id AND op.partition_label = sg.partition_label
LEFT JOIN anim        ON anim.tenant_id = sg.tenant_id AND anim.shed_id = sg.shed_id
LEFT JOIN anim_part ap ON ap.tenant_id = sg.tenant_id AND ap.shed_id = sg.shed_id AND ap.partition_label = sg.partition_label
LEFT JOIN owner_seat  om ON om.tenant_id = sg.tenant_id AND om.shed_id = sg.shed_id
LEFT JOIN backup_seat bk ON bk.tenant_id = sg.tenant_id AND bk.shed_id = sg.shed_id
WHERE s.location_type = 'shed';

-- ===========================================================================
-- ceo_ai.vaccination_dose_pickup -- add the partition dimension.
-- ===========================================================================
-- projection-review: membership=identical batch/shed obligation membership as before, UNIONed with one additional per-partition slice of the same rows; group_key=obl_part adds NULLIF(gsp.partition_label,'whole') to the existing (tenant_id, batch_id, scope_id, rule_id) GROUP BY; join_cardinality=goat_shed_partitions joined on obligation_instances.target_id (=goat_id when target_type='goat') is 1:{0,1} per goat, so the join cannot fan out the animals_due/animals_overdue FILTER aggregates; the obl_grains UNION ALL of the bare and per-partition CTEs cannot double count because each CTE pre-aggregates before the union and JOINs obligation_batches once per resulting row; pagination=none, whole-tenant reporting view; scope=tenant_id, with park/shed/partition exposed as columns
CREATE OR REPLACE VIEW ceo_ai.vaccination_dose_pickup AS
WITH obl AS (
    SELECT
        oi.tenant_id,
        oi.batch_id,
        oi.scope_id AS shed_id,
        oi.rule_id,
        NULL::text AS partition_label,
        COUNT(*) FILTER (WHERE oi.status IN ('scheduled','due','in_progress'))                       AS animals_due,
        COUNT(*) FILTER (WHERE oi.status IN ('scheduled','due','in_progress','missed')
                          AND oi.window_end < (now() AT TIME ZONE 'Asia/Kolkata')::date)             AS animals_overdue
    FROM obligation_instances oi
    WHERE oi.scope_type = 'shed' AND oi.batch_id IS NOT NULL
    GROUP BY oi.tenant_id, oi.batch_id, oi.scope_id, oi.rule_id
),
-- partition_review: producer key adds NULLIF(gsp.partition_label,'whole') to the
-- existing (tenant_id, batch_id, scope_id, rule_id) GROUP BY. obligation_instances
-- target_type is always 'goat' for a vaccination obligation, so target_id IS the
-- goat_id; goat_shed_partitions PK is (tenant_id, goat_id), 1:{0,1} per goat, so
-- this join cannot fan out the animals_due/animals_overdue FILTER aggregates.
obl_part AS (
    SELECT
        oi.tenant_id,
        oi.batch_id,
        oi.scope_id AS shed_id,
        oi.rule_id,
        NULLIF(gsp.partition_label, 'whole')                                                          AS partition_label,
        COUNT(*) FILTER (WHERE oi.status IN ('scheduled','due','in_progress'))                         AS animals_due,
        COUNT(*) FILTER (WHERE oi.status IN ('scheduled','due','in_progress','missed')
                          AND oi.window_end < (now() AT TIME ZONE 'Asia/Kolkata')::date)               AS animals_overdue
    FROM obligation_instances oi
    JOIN goat_shed_partitions gsp ON gsp.tenant_id = oi.tenant_id AND gsp.goat_id = oi.target_id
    WHERE oi.scope_type = 'shed' AND oi.batch_id IS NOT NULL AND oi.target_type = 'goat'
      AND NULLIF(gsp.partition_label, 'whole') IS NOT NULL
    GROUP BY oi.tenant_id, oi.batch_id, oi.scope_id, oi.rule_id, NULLIF(gsp.partition_label, 'whole')
),
-- obl_grains UNIONs the bare (whole-shed, unchanged) row with the per-partition
-- split -- additive, matching ceo_ai.shed_capacity_current's convention: the
-- pre-existing bare row keeps answering exactly as before; partitions are new
-- rows layered on top, never a replacement.
obl_grains AS (
    SELECT * FROM obl
    UNION ALL
    SELECT * FROM obl_part
)
SELECT
    ob.tenant_id                                                   AS tenant_id,
    bt.planned_date                                                AS business_date,
    pk.name                                                        AS park_label,
    sh.name                                                        AS shed_label,
    -- HUMAN label only; NULL (never a raw dose_code) when family unmapped.
    ceo_ai.vaccine_label_for(pr.dose_code)                         AS vaccine_label,
    -- doses_to_pick is a whole-BATCH reservation (obligation_batches.reserved_quantity);
    -- there is no per-partition dose-reservation column anywhere in the schema, so a
    -- partition row shows the SAME doses_to_pick as its parent batch/shed row, with only
    -- animals_due/animals_overdue scoped to that partition -- mirrors
    -- ceo_ai.shed_capacity_current's capacity column, which is likewise shed-grain-only,
    -- never invented/divided per partition.
    bt.reserved_quantity                                           AS doses_to_pick,
    ob.animals_due                                                 AS animals_due,
    ob.animals_overdue                                             AS animals_overdue,
    ownm.display_name                                              AS owner_label,
    bkm.display_name                                               AS backup_label,
    CASE
        WHEN ob.animals_overdue > 0 THEN 'catch_up_overdue'
        WHEN ob.animals_due     > 0 THEN 'pick_and_administer'
        ELSE 'no_action'
    END                                                           AS next_action,
    -- appended last: a replace-in-place view rebuild can only add trailing columns.
    ob.partition_label                                             AS partition_label
FROM obl_grains ob
JOIN obligation_batches bt ON bt.tenant_id = ob.tenant_id AND bt.batch_id = ob.batch_id
LEFT JOIN locations      sh ON sh.location_id = ob.shed_id
LEFT JOIN locations      pk ON pk.location_id = sh.parent_location_id
LEFT JOIN protocol_rules pr ON pr.rule_id = ob.rule_id
-- drive conductor (batch.conducted_by is a user_id -> workforce_members.user_id)
LEFT JOIN workforce_members ownm ON ownm.tenant_id = bt.tenant_id AND ownm.user_id = bt.conducted_by
-- backup: active backup seat at the shed (shed-grain -- no per-partition backup seat exists)
LEFT JOIN LATERAL (
    SELECT wm.display_name
    FROM workforce_positions wp
    JOIN workforce_members wm ON wm.workforce_member_id = wp.workforce_member_id
    WHERE wp.tenant_id = ob.tenant_id AND wp.scope_type = 'shed' AND wp.scope_id = ob.shed_id
      AND wp.is_backup_slot = true AND wp.status = 'active'
      AND now() >= wp.valid_from AND now() < COALESCE(wp.valid_to, 'infinity'::timestamptz)
    LIMIT 1
) bkm ON true;

-- +goose Down
-- NOTE: a replace-in-place view rebuild cannot drop columns, so both views are
-- DROPped and recreated at their pre-partition (000001 baseline) shape before
-- the underlying column is removed.
DROP VIEW IF EXISTS ceo_ai.vaccination_dose_pickup;
DROP VIEW IF EXISTS ceo_ai.vaccination_shed_status;

-- projection-review: membership=identical to the Up definition minus the partition dimension -- this is the ROLLBACK shape, restoring the pre-partition (000001 baseline) view verbatim; group_key=the original shed-grain key with no partition column; join_cardinality=unchanged from the Up definition, every join is a 1:{0,1} lookup on a primary key so no fan-out is introduced by the rollback; pagination=none; scope=tenant_id, unchanged
CREATE VIEW ceo_ai.vaccination_shed_status AS
WITH obl AS (
    SELECT
        tenant_id,
        scope_id AS shed_id,
        COUNT(*) FILTER (WHERE status IN ('scheduled','due','in_progress'))                          AS due,
        COUNT(*) FILTER (WHERE status IN ('completed','accepted'))                                    AS done,
        COUNT(*) FILTER (WHERE status IN ('scheduled','due','in_progress','missed')
                          AND window_end < (now() AT TIME ZONE 'Asia/Kolkata')::date)                 AS overdue,
        MIN(due_at) FILTER (WHERE status IN ('scheduled','due','in_progress'))                        AS next_due,
        COUNT(DISTINCT batch_id) FILTER (WHERE batch_id IS NOT NULL)                                  AS planned_sessions
    FROM obligation_instances
    WHERE scope_type = 'shed'
    GROUP BY tenant_id, scope_id
),
anim AS (
    SELECT tenant_id, shed_id, SUM(animal_count) FILTER (WHERE usable_for_vaccination)::bigint AS animals
    FROM vaccination_eligibility_rollups
    GROUP BY tenant_id, shed_id
),
owner_seat AS (
    SELECT wp.tenant_id, wp.scope_id AS shed_id, wm.display_name AS manager_label
    FROM workforce_positions wp
    JOIN workforce_members wm ON wm.workforce_member_id = wp.workforce_member_id
    WHERE wp.scope_type = 'shed' AND wp.is_backup_slot = false AND wp.status = 'active'
      AND now() >= wp.valid_from AND now() < COALESCE(wp.valid_to, 'infinity'::timestamptz)
),
backup_seat AS (
    SELECT wp.tenant_id, wp.scope_id AS shed_id, wm.display_name AS backup_label
    FROM workforce_positions wp
    JOIN workforce_members wm ON wm.workforce_member_id = wp.workforce_member_id
    WHERE wp.scope_type = 'shed' AND wp.is_backup_slot = true AND wp.status = 'active'
      AND now() >= wp.valid_from AND now() < COALESCE(wp.valid_to, 'infinity'::timestamptz)
)
SELECT
    s.tenant_id                                                    AS tenant_id,
    pk.name                                                        AS park_label,
    s.name                                                         AS shed_label,
    COALESCE(anim.animals, 0)                                      AS animals,
    COALESCE(obl.due, 0)                                           AS due,
    COALESCE(obl.done, 0)                                          AS done,
    COALESCE(obl.planned_sessions, 0)                              AS planned_sessions,
    (obl.next_due AT TIME ZONE 'Asia/Kolkata')::date              AS next_due_date,
    om.manager_label                                               AS manager_label,
    bk.backup_label                                                AS backup_label,
    CASE
        WHEN COALESCE(obl.overdue, 0) > 0 THEN 'overdue'
        WHEN COALESCE(obl.due, 0)     > 0 THEN 'due'
        WHEN COALESCE(obl.done, 0)    > 0 THEN 'complete'
        ELSE 'no_work_due'
    END                                                           AS status
FROM locations s
LEFT JOIN locations   pk ON pk.location_id = s.parent_location_id
LEFT JOIN obl         ON obl.tenant_id  = s.tenant_id AND obl.shed_id  = s.location_id
LEFT JOIN anim        ON anim.tenant_id = s.tenant_id AND anim.shed_id = s.location_id
LEFT JOIN owner_seat  om ON om.tenant_id = s.tenant_id AND om.shed_id = s.location_id
LEFT JOIN backup_seat bk ON bk.tenant_id = s.tenant_id AND bk.shed_id = s.location_id
WHERE s.location_type = 'shed';

-- projection-review: membership=identical to the Up definition minus the partition dimension -- this is the ROLLBACK shape, restoring the pre-partition (000001 baseline) view verbatim; group_key=the original shed-grain key with no partition column; join_cardinality=unchanged from the Up definition, every join is a 1:{0,1} lookup on a primary key so no fan-out is introduced by the rollback; pagination=none; scope=tenant_id, unchanged
CREATE VIEW ceo_ai.vaccination_dose_pickup AS
WITH obl AS (
    SELECT
        oi.tenant_id,
        oi.batch_id,
        oi.scope_id AS shed_id,
        oi.rule_id,
        COUNT(*) FILTER (WHERE oi.status IN ('scheduled','due','in_progress'))                       AS animals_due,
        COUNT(*) FILTER (WHERE oi.status IN ('scheduled','due','in_progress','missed')
                          AND oi.window_end < (now() AT TIME ZONE 'Asia/Kolkata')::date)             AS animals_overdue
    FROM obligation_instances oi
    WHERE oi.scope_type = 'shed' AND oi.batch_id IS NOT NULL
    GROUP BY oi.tenant_id, oi.batch_id, oi.scope_id, oi.rule_id
)
SELECT
    ob.tenant_id                                                   AS tenant_id,
    bt.planned_date                                                AS business_date,
    pk.name                                                        AS park_label,
    sh.name                                                        AS shed_label,
    ceo_ai.vaccine_label_for(pr.dose_code)                         AS vaccine_label,
    bt.reserved_quantity                                           AS doses_to_pick,
    ob.animals_due                                                 AS animals_due,
    ob.animals_overdue                                             AS animals_overdue,
    ownm.display_name                                              AS owner_label,
    bkm.display_name                                               AS backup_label,
    CASE
        WHEN ob.animals_overdue > 0 THEN 'catch_up_overdue'
        WHEN ob.animals_due     > 0 THEN 'pick_and_administer'
        ELSE 'no_action'
    END                                                           AS next_action
FROM obl ob
JOIN obligation_batches bt ON bt.tenant_id = ob.tenant_id AND bt.batch_id = ob.batch_id
LEFT JOIN locations      sh ON sh.location_id = ob.shed_id
LEFT JOIN locations      pk ON pk.location_id = sh.parent_location_id
LEFT JOIN protocol_rules pr ON pr.rule_id = ob.rule_id
LEFT JOIN workforce_members ownm ON ownm.tenant_id = bt.tenant_id AND ownm.user_id = bt.conducted_by
LEFT JOIN LATERAL (
    SELECT wm.display_name
    FROM workforce_positions wp
    JOIN workforce_members wm ON wm.workforce_member_id = wp.workforce_member_id
    WHERE wp.tenant_id = ob.tenant_id AND wp.scope_type = 'shed' AND wp.scope_id = ob.shed_id
      AND wp.is_backup_slot = true AND wp.status = 'active'
      AND now() >= wp.valid_from AND now() < COALESCE(wp.valid_to, 'infinity'::timestamptz)
    LIMIT 1
) bkm ON true;

-- The table is a fully DERIVED read model (rebuilt wholesale by
-- backend/cmd/vaccination-eligibility-rollup-recompute -- see the Up header).
-- Once the Up side has run a recompute, a shed with real partitions carries
-- MULTIPLE rows sharing one (tenant, park, shed, species, stage, sex, breed,
-- health, usable) key -- exactly the grain the old pre-partition unique index
-- forbids. Rebuilding that index over live partitioned data would fail with a
-- duplicate-key error, so this clears the derived rows first: it is the same
-- "clear then reinsert" step RecomputeEligibilityRollup already performs on
-- every run, run once here so the OLD index can be rebuilt cleanly. No
-- canonical/source data (goats, obligations, etc.) is touched -- only this
-- derived rollup, and the very next recompute regenerates it in full at the
-- restored pre-partition grain.
DELETE FROM public.vaccination_eligibility_rollups;

DROP INDEX IF EXISTS public.vaccination_eligibility_rollups_grain_uidx;

CREATE UNIQUE INDEX vaccination_eligibility_rollups_grain_uidx ON public.vaccination_eligibility_rollups
    USING btree (tenant_id, COALESCE(park_id, '00000000-0000-0000-0000-000000000000'::uuid), COALESCE(shed_id, '00000000-0000-0000-0000-000000000000'::uuid), species, management_stage, sex, breed, health_status, usable_for_vaccination);

ALTER TABLE public.vaccination_eligibility_rollups
    DROP COLUMN IF EXISTS partition_label;
