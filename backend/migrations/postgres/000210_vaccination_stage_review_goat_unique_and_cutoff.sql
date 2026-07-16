-- +goose Up
-- VACC-REV-10 hardening:
-- (a) Enforce one OPEN review item per (tenant, goat) at the DATABASE layer, so uniqueness holds across
--     ALL live writers during a rollout — not just the bridge writer that takes the advisory lock. A
--     predecessor writer's duplicate insert now FAILS CLOSED against this unique index (error, no dup)
--     instead of silently creating a second open row. The bridge writer's update-else-insert (and its
--     ON CONFLICT DO NOTHING insert) respects this index, so it never errors on it.
-- (b) Persist the age cutoff that RAISED each item, so the 'corrected' re-check evaluates against the
--     exact effective procurement policy that flagged the goat — not a value re-derived from MIN across
--     all published versions (which wrongly includes expired / superseded / differently-scoped ones).

ALTER TABLE vaccination_stage_review_items
  ADD COLUMN IF NOT EXISTS age_cutoff_weeks integer;

-- Dedup existing OPEN rows per (tenant, goat) before adding the unique index: keep the newest, resolve
-- the rest. Migrations here run in autocommit (no LOCK TABLE); a concurrent duplicate would make the
-- unique index build fail closed (safe, retriable) rather than corrupt.
UPDATE vaccination_stage_review_items v
SET status = 'resolved', resolved_at = now(),
    resolution_note = 'auto-resolved: superseded; open uniqueness is per (tenant, goat)',
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

CREATE UNIQUE INDEX IF NOT EXISTS vaccination_stage_review_items_open_goat_unique
  ON vaccination_stage_review_items (tenant_id, goat_id)
  WHERE status = 'open';

-- The (tenant, idempotency_key) open index is now superseded by the stronger per-goat index.
DROP INDEX IF EXISTS vaccination_stage_review_items_open_idem_unique;

-- +goose Down
CREATE UNIQUE INDEX IF NOT EXISTS vaccination_stage_review_items_open_idem_unique
  ON vaccination_stage_review_items (tenant_id, idempotency_key)
  WHERE status = 'open';
DROP INDEX IF EXISTS vaccination_stage_review_items_open_goat_unique;
ALTER TABLE vaccination_stage_review_items DROP COLUMN IF EXISTS age_cutoff_weeks;
