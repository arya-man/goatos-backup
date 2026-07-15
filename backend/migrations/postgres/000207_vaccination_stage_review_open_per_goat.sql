-- +goose Up
-- VACC-REV-10 defect fix: OPEN-item uniqueness must be per (tenant, goat), not per
-- (tenant, idempotency_key). The idempotency_key embedded the observed management stage, so a goat
-- moving from a stale K1 to a stale K2 minted a DIFFERENT key and received a SECOND open review item
-- for the SAME goat. A stage/age mismatch is a per-goat condition: one open item per goat, updated in
-- place as the observed stage/age changes, resolved once, and re-opened only after a resolution.

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

DROP INDEX IF EXISTS vaccination_stage_review_items_open_idem_unique;

-- One OPEN item per goat, independent of the observed stage. A recurrence after resolution still
-- opens a new occurrence (partial index on status = 'open').
CREATE UNIQUE INDEX vaccination_stage_review_items_open_goat_unique
  ON vaccination_stage_review_items (tenant_id, goat_id)
  WHERE status = 'open';

-- +goose Down
DROP INDEX IF EXISTS vaccination_stage_review_items_open_goat_unique;
CREATE UNIQUE INDEX vaccination_stage_review_items_open_idem_unique
  ON vaccination_stage_review_items (tenant_id, idempotency_key)
  WHERE status = 'open';
