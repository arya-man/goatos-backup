-- +goose Up
-- seed-fixture-guard:ignore: operational Weighing free-flow kernel work items are written by the Weighing publish transaction and the kernel worker; they do not change the Vaccination HRMS seed contract
--
-- ROOT-CAUSE FIX: B11 — Pre-existing campaigns never enter the kernel.
--
-- 000059_weighing_kernel_work_items.sql:40 created weighing_work_items with IF NOT EXISTS,
-- but only the publish transaction (createWorkItemsForPublishTx in kernel.go:85)
-- materializes work items. Campaigns published BEFORE 000059 deployed have NO work items
-- and therefore NO durable obligation, NO cadence triggering, and NO calendar/control tower
-- visibility — they are permanently invisible to the operational kernel.
--
-- No self-healing exists: the existing goat_created_recovery stage is vaccination-only,
-- and cmd/backfill-goat-created has no weighing path.
--
-- FIX: idempotent backfill inserting one weighing_work_items row per existing
-- weighing_campaign_sheds row, using the SAME day-offset calculation as createWorkItemsForPublishTx
-- (kernel.go:85-134). Work state is inferred from the bucket's current status, respecting the
-- unique constraint (tenant_id, campaign_shed_id) so re-run is safe and idempotent.
--
-- projection-review: producer = weighing_campaign_sheds (tenant_id, campaign_id, location_id, status),
-- joined to weighing_campaigns (tenant_id, campaign_id, start_business_date, planned_cap_per_day, park_id).
-- Consumer = weighing_work_items (tenant_id, campaign_shed_id, work_state).
-- Row multiplicity: weighing_campaign_sheds -> weighing_campaigns is many:1 per (tenant_id, campaign_id);
-- both sides contribute 1 row per shed, so join is 1:1. No fan-out, no double-counting.
-- Day offset numerator (running bucket size) and denominator (planned_cap_per_day) both range over
-- (tenant_id, campaign_id, operator_user_id), identical for 1:1 correspondence.
--
-- Inferred work_state mapping:
--   canceled   -> work_state='canceled'
--   completed  -> work_state='completed'
--   closed     -> work_state='closed'
--   pending    -> work_state='scheduled'
--   in_progress-> work_state='scheduled'
-- (An in_progress bucket will roll forward on next kernel sweep; a pending bucket is still actionable.)
INSERT INTO weighing_work_items (
  work_item_id, tenant_id, campaign_id, campaign_shed_id, park_id, operator_user_id,
  weighing_category, shed_label, shed_location_id, planned_business_date, due_business_date, work_state,
  created_at, updated_at
)
SELECT gen_random_uuid(),
       planned.tenant_id,
       planned.campaign_id,
       planned.campaign_shed_id,
       planned.park_id,
       planned.operator_user_id,
       planned.weighing_category,
       planned.display_name,
       planned.location_id,
       planned.start_business_date + planned.day_offset,
       planned.start_business_date + planned.day_offset,
       CASE planned.status
         WHEN 'canceled' THEN 'canceled'
         WHEN 'completed' THEN 'completed'
         WHEN 'closed' THEN 'closed'
         ELSE 'scheduled'
       END,
       NOW(),
       NOW()
FROM (
  SELECT cs.tenant_id,
         cs.campaign_id,
         cs.campaign_shed_id,
         c.park_id,
         cs.operator_user_id,
         cs.weighing_category,
         cs.display_name,
         cs.location_id,
         cs.status,
         c.start_business_date,
         floor(
           COALESCE(
             sum(GREATEST(cs.expected_animal_count, 1)) OVER (
               PARTITION BY cs.tenant_id, cs.campaign_id, cs.operator_user_id
               ORDER BY cs.display_name, cs.campaign_shed_id
               ROWS BETWEEN UNBOUNDED PRECEDING AND 1 PRECEDING
             ), 0
           )::numeric / GREATEST(c.planned_cap_per_day, 1)::numeric
         )::int AS day_offset
  FROM weighing_campaign_sheds cs
  JOIN weighing_campaigns c
    ON c.tenant_id = cs.tenant_id
   AND c.campaign_id = cs.campaign_id
  WHERE cs.status NOT IN ('canceled')
) planned
ON CONFLICT (tenant_id, campaign_shed_id) DO NOTHING;

-- +goose Down
-- Removal is not possible without risking live state: work items may have been
-- rolled forward, claimed, or completed post-backfill. A DOWN that deletes
-- backfilled rows cannot distinguish them from originally-published rows, so
-- DOWN is a no-op. Future removal (if needed) requires explicit audit of
-- created_at vs deployment timestamp and a separate removal migration.
