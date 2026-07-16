-- +goose Up
-- VACC-REV-10 preparation: collapse existing duplicate OPEN items per (tenant, goat), but do NOT
-- change the open-item uniqueness indexes in this migration. The first shipped draft of this migration
-- temporarily dropped the predecessor writer's ON CONFLICT target and added a hard (tenant, goat)
-- unique index before the compatible writer was live. That is not migrate-first safe. The later
-- compatibility-aware migrations own the index rollout.

-- Collapse any existing duplicate OPEN items per (tenant, goat): keep the newest, resolve the rest so
-- the new unique index can be created and the operator sees one row per affected goat.
UPDATE vaccination_stage_review_items v
SET status = 'resolved',
    resolved_at = now(),
    resolution_note = 'auto-resolved: superseded; open uniqueness is now per (tenant, goat)',
    updated_at = now()
FROM (
  SELECT review_item_id,
         row_number() OVER (
           PARTITION BY tenant_id, goat_id
           ORDER BY created_at DESC, review_item_id DESC
         ) AS rn
  FROM vaccination_stage_review_items
  WHERE status = 'open'
) ranked
WHERE v.review_item_id = ranked.review_item_id
  AND ranked.rn > 1;

-- Intentionally retain vaccination_stage_review_items_open_idem_unique here. A still-live predecessor
-- binary uses ON CONFLICT (tenant_id, idempotency_key) WHERE status='open'; dropping that index before
-- the writer rollout causes 42P10 on every write. The hard goat-unique index is added later after the
-- stage-free idempotency repair.

-- +goose Down
-- No index rollback: this migration no longer changes indexes, and the duplicate-resolution update is
-- intentionally not reversed.
SELECT 1;
