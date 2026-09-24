-- +goose Up
-- stg-zero-downtime: reporting-view replacement only; no product write path or table lock.
-- CEO operator status is animal-grain truth. Batch status is not completion truth when one
-- assignment contains obligations carried from several source batches or closes incrementally.
CREATE OR REPLACE VIEW ceo_ai.vaccination_operator_status AS
-- projection-review: membership=active vaccination_drive_assignment_members joined to live goat obligation_instances, plus legacy assignments only when no active members exist; group_key=tenant/operator/park/shed/partition/planned_date after collapsing member obligations to one goat row in member_animal; join_cardinality=assignment members join one obligation row by obligation_id, completion is an EXISTS scalar, capacity config is tenant 0..1, and workforce/location labels are 0..1 lookups after aggregation; pagination=none, this is a reporting view with complete operator-day totals; scope=tenant_id is carried through every CTE and park/shed/partition remain grouping dimensions.
WITH member_animal AS (
    SELECT
        a.tenant_id, a.operator_id, a.park_id, a.shed_id,
        NULLIF(a.partition_label, 'whole') AS partition_label,
        a.planned_date, oi.target_id AS goat_id,
        bool_and(
            oi.status = 'completed'
            OR EXISTS (
                SELECT 1 FROM public.vaccination_completions vc
                WHERE vc.tenant_id = oi.tenant_id
                  AND vc.obligation_id = oi.obligation_id
                  AND vc.status IN ('recorded', 'accepted')
            )
        ) AS all_done,
        bool_or(a.capacity_status IN ('over_cap_required','capacity_action')) AS any_over_cap
    FROM public.vaccination_drive_assignments a
    JOIN public.vaccination_drive_assignment_members m
      ON m.tenant_id = a.tenant_id
     AND m.assignment_id = a.assignment_id
     AND m.canceled_at IS NULL
    JOIN public.obligation_instances oi
      ON oi.tenant_id = m.tenant_id
     AND oi.obligation_id = m.obligation_id
     AND oi.target_type = 'goat'
     AND oi.status NOT IN ('canceled', 'waived', 'superseded')
    WHERE a.operator_id IS NOT NULL
    GROUP BY a.tenant_id, a.operator_id, a.park_id, a.shed_id,
             NULLIF(a.partition_label, 'whole'), a.planned_date, oi.target_id
),
member_assigned AS (
    SELECT tenant_id, operator_id, park_id, shed_id, partition_label, planned_date,
           COUNT(*)::bigint AS assigned_animals,
           COUNT(*) FILTER (WHERE NOT all_done)::bigint AS due,
           COUNT(*) FILTER (WHERE all_done)::bigint AS done,
           COUNT(*) FILTER (
               WHERE NOT all_done
                 AND planned_date < (now() AT TIME ZONE 'Asia/Kolkata')::date
           )::bigint AS overdue,
           bool_or(any_over_cap) AS any_over_cap
    FROM member_animal
    GROUP BY tenant_id, operator_id, park_id, shed_id, partition_label, planned_date
),
legacy_assigned AS (
    SELECT a.tenant_id, a.operator_id, a.park_id, a.shed_id,
           NULLIF(a.partition_label, 'whole') AS partition_label, a.planned_date,
           COALESCE(MAX(a.animal_count), 0)::bigint AS assigned_animals,
           COALESCE(MAX(a.animal_count) FILTER (WHERE b.status IN ('planned','in_progress')), 0)::bigint AS due,
           COALESCE(MAX(a.animal_count) FILTER (WHERE b.status = 'completed'), 0)::bigint AS done,
           COALESCE(MAX(a.animal_count) FILTER (
               WHERE b.status IN ('planned','in_progress')
                 AND a.planned_date < (now() AT TIME ZONE 'Asia/Kolkata')::date
           ), 0)::bigint AS overdue,
           bool_or(a.capacity_status IN ('over_cap_required','capacity_action')) AS any_over_cap
    FROM public.vaccination_drive_assignments a
    LEFT JOIN public.obligation_batches b
      ON b.tenant_id = a.tenant_id AND b.batch_id = a.batch_id
    WHERE a.operator_id IS NOT NULL
      AND NOT EXISTS (
          SELECT 1 FROM public.vaccination_drive_assignment_members m
          WHERE m.tenant_id = a.tenant_id
            AND m.assignment_id = a.assignment_id
            AND m.canceled_at IS NULL
      )
    GROUP BY a.tenant_id, a.operator_id, a.park_id, a.shed_id,
             NULLIF(a.partition_label, 'whole'), a.planned_date
),
assigned AS (
    SELECT * FROM member_assigned
    UNION ALL
    SELECT * FROM legacy_assigned
),
cap AS (
    SELECT tenant_id, max_per_day
    FROM public.vaccination_capacity_config
    WHERE capacity_scope = 'tenant'
)
SELECT s.tenant_id, s.operator_id, wm.display_name AS operator_label,
       s.park_id, pk.name AS park_label, s.shed_id, sh.name AS shed_label,
       s.planned_date, s.assigned_animals, s.due, s.done, s.overdue,
       COALESCE(cap.max_per_day, 200) AS daily_capacity,
       SUM(s.assigned_animals) OVER (
           PARTITION BY s.tenant_id, s.operator_id, s.planned_date
       ) AS operator_day_assigned,
       ROUND((SUM(s.assigned_animals) OVER (
           PARTITION BY s.tenant_id, s.operator_id, s.planned_date
       ))::numeric / NULLIF(COALESCE(cap.max_per_day, 200), 0), 3) AS utilization,
       CASE
           WHEN s.overdue > 0 THEN 'catch_up_overdue'
           WHEN SUM(s.assigned_animals) OVER (
                    PARTITION BY s.tenant_id, s.operator_id, s.planned_date
                ) > COALESCE(cap.max_per_day, 200) THEN 'rebalance_overloaded'
           WHEN s.due > 0 THEN 'run_drive'
           WHEN s.done > 0 AND s.due = 0 THEN 'completed'
           ELSE 'no_action'
       END AS next_action,
       s.partition_label
FROM assigned s
LEFT JOIN public.workforce_members wm ON wm.tenant_id = s.tenant_id AND wm.workforce_member_id = s.operator_id
LEFT JOIN public.locations pk ON pk.tenant_id = s.tenant_id AND pk.location_id = s.park_id
LEFT JOIN public.locations sh ON sh.tenant_id = s.tenant_id AND sh.location_id = s.shed_id
LEFT JOIN cap ON cap.tenant_id = s.tenant_id;

-- +goose Down
-- Reporting truth corrections are intentionally forward-only. Reapplying an older migration or
-- restoring batch-status inference would reintroduce misleading CEO counts.
