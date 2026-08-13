-- +goose Up
-- Exact-shed cutover repair for reporting views originally widened during the
-- old parent-shed + partition-label model. Keep the trailing partition_label
-- columns for compatibility, but make live reporting grain the exact shed id.

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
    (((now() AT TIME ZONE 'Asia/Kolkata')::date) - COALESCE(g.dob, g.approx_dob))::int
                                                  AS age_days,
    NULL::text                                    AS partition_label
FROM goats g
LEFT JOIN locations pk ON pk.location_id = g.park_id
LEFT JOIN locations sh ON sh.location_id = g.shed_id
LEFT JOIN breeds    b  ON b.breed_id     = g.breed_id;

CREATE OR REPLACE VIEW ceo_ai.shed_capacity_current AS
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
    bk.backup_label                               AS backup_label,
    NULL::text                                    AS partition_label
FROM locations s
LEFT JOIN locations     pk ON pk.location_id = s.parent_location_id
LEFT JOIN shed_profiles sp ON sp.location_id = s.location_id
LEFT JOIN occ            ON occ.tenant_id = s.tenant_id AND occ.shed_id = s.location_id
LEFT JOIN owner_seat  o  ON o.tenant_id  = s.tenant_id AND o.shed_id  = s.location_id
LEFT JOIN backup_seat bk ON bk.tenant_id = s.tenant_id AND bk.shed_id = s.location_id
WHERE s.location_type = 'shed';

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
        ), 0)::bigint                                                                        AS overdue
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
    s.park_id                                                              AS park_id,
    pk.name                                                                AS park_label,
    s.shed_id                                                              AS shed_id,
    sh.name                                                                AS shed_label,
    s.planned_date                                                         AS planned_date,
    s.assigned_animals                                                     AS assigned_animals,
    s.due                                                                  AS due,
    s.done                                                                 AS done,
    s.overdue                                                              AS overdue,
    COALESCE(cap.max_per_day, 200)                                         AS daily_capacity,
    SUM(s.assigned_animals) OVER (
        PARTITION BY s.tenant_id, s.operator_id, s.planned_date
    )                                                                      AS operator_day_assigned,
    ROUND(
        (SUM(s.assigned_animals) OVER (
            PARTITION BY s.tenant_id, s.operator_id, s.planned_date
        ))::numeric / NULLIF(COALESCE(cap.max_per_day, 200), 0), 3
    )                                                                      AS utilization,
    CASE
        WHEN s.overdue > 0 THEN 'catch_up_overdue'
        WHEN (SUM(s.assigned_animals) OVER (
                 PARTITION BY s.tenant_id, s.operator_id, s.planned_date))
             > COALESCE(cap.max_per_day, 200) THEN 'rebalance_overloaded'
        WHEN s.due > 0 THEN 'run_drive'
        WHEN s.done > 0 AND s.due = 0 THEN 'completed'
        ELSE 'no_action'
    END                                                                    AS next_action,
    NULL::text                                                             AS partition_label
FROM assigned s
LEFT JOIN public.workforce_members wm ON wm.workforce_member_id = s.operator_id
LEFT JOIN public.locations         pk ON pk.location_id = s.park_id
LEFT JOIN public.locations         sh ON sh.location_id = s.shed_id
LEFT JOIN cap ON cap.tenant_id = s.tenant_id;

CREATE OR REPLACE VIEW ceo_ai.vaccination_shed_status AS
WITH obl AS (
    SELECT
        tenant_id,
        scope_id AS shed_id,
        COUNT(*) FILTER (WHERE status IN ('scheduled','due','in_progress'))           AS due,
        COUNT(*) FILTER (WHERE status IN ('completed','accepted'))                    AS done,
        COUNT(*) FILTER (WHERE status IN ('scheduled','due','in_progress','missed')
                          AND window_end < (now() AT TIME ZONE 'Asia/Kolkata')::date) AS overdue,
        MIN(due_at) FILTER (WHERE status IN ('scheduled','due','in_progress'))        AS next_due,
        COUNT(DISTINCT batch_id) FILTER (WHERE batch_id IS NOT NULL)                  AS planned_sessions
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
    COALESCE(obl.due, 0)::bigint                                   AS due,
    COALESCE(obl.done, 0)::bigint                                  AS done,
    COALESCE(obl.planned_sessions, 0)::bigint                      AS planned_sessions,
    (obl.next_due AT TIME ZONE 'Asia/Kolkata')::date               AS next_due_date,
    om.manager_label                                               AS manager_label,
    bk.backup_label                                                AS backup_label,
    CASE
        WHEN COALESCE(obl.overdue, 0) > 0 THEN 'overdue'
        WHEN COALESCE(obl.due, 0)     > 0 THEN 'due'
        WHEN COALESCE(obl.done, 0)    > 0 THEN 'complete'
        ELSE 'no_work_due'
    END                                                            AS status,
    NULL::text                                                     AS partition_label
FROM locations s
LEFT JOIN locations   pk ON pk.location_id = s.parent_location_id
LEFT JOIN obl         ON obl.tenant_id  = s.tenant_id AND obl.shed_id  = s.location_id
LEFT JOIN anim        ON anim.tenant_id = s.tenant_id AND anim.shed_id = s.location_id
LEFT JOIN owner_seat  om ON om.tenant_id = s.tenant_id AND om.shed_id = s.location_id
LEFT JOIN backup_seat bk ON bk.tenant_id = s.tenant_id AND bk.shed_id = s.location_id
WHERE s.location_type = 'shed';

CREATE OR REPLACE VIEW ceo_ai.vaccination_dose_pickup AS
WITH obl AS (
    SELECT
        oi.tenant_id,
        oi.batch_id,
        oi.scope_id AS shed_id,
        oi.rule_id,
        COUNT(*) FILTER (WHERE oi.status IN ('scheduled','due','in_progress'))           AS animals_due,
        COUNT(*) FILTER (WHERE oi.status IN ('scheduled','due','in_progress','missed')
                          AND oi.window_end < (now() AT TIME ZONE 'Asia/Kolkata')::date) AS animals_overdue
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
    END                                                            AS next_action,
    NULL::text                                                     AS partition_label
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

-- +goose Down
-- Reporting-only compatibility repair. The exact-shed cutover migrations remain
-- authoritative; Down intentionally leaves the exact-shed-safe view shape in place.
SELECT 1;
