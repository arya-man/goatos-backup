-- +goose Up
-- Release-1 compatible-writer fix for the VACC-REV-10 open-uniqueness change.
--
-- The first draft of 000207 dropped the (tenant_id, idempotency_key) open index and added a
-- (tenant_id, goat_id) UNIQUE index. That shape is NOT migrate-first-safe: staging runs migrations
-- BEFORE updating the API/worker, so the still-live previous-release recorder runs
-- `ON CONFLICT (tenant_id, idempotency_key)` (index gone -> "no unique or exclusion constraint
-- matching") and its INSERT path could also violate the new (tenant_id, goat_id) UNIQUE index. The
-- current 000207 source keeps the old index; this migration is still the compatibility repair for
-- environments that crossed the unsafe draft and the stage-free key rollout for every environment.
--
-- This migration restores the compatible target and achieves per-goat open uniqueness through a
-- STAGE-FREE idempotency key instead (key = 'vacc-stage-review:'||tenant||':'||goat, which the new
-- recorder now emits). Both the old binary (stage-keyed key, same conflict target) and the new binary
-- (stage-free key, same conflict target) run without error. The hard (tenant_id, goat_id) UNIQUE index
-- is deferred to a FOLLOW-UP release once every writer emits the stage-free key.

-- Dedup + key-rewrite + index build. Migrations run in autocommit here (LOCK TABLE is not available),
-- so this is not a single locked transaction. That is safe: the dedup below removes existing dups, and
-- if a concurrent generation write races a new dup in before the unique index is built, CREATE UNIQUE
-- INDEX FAILS CLOSED (the migration errors and the deploy halts, retriable) rather than corrupting or
-- silently duplicating. This table is small and only written during periodic generation passes.

-- Collapse any duplicate OPEN items per (tenant, goat): keep the newest, resolve the rest.
UPDATE vaccination_stage_review_items v
SET status = 'resolved',
    resolved_at = now(),
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

-- Rewrite each surviving OPEN row's idempotency_key to the stage-free per-goat form the new recorder
-- emits, so a new-binary generation pass matches (updates) the existing row instead of inserting a
-- second one. Safe now that at most one open row exists per (tenant, goat).
UPDATE vaccination_stage_review_items
SET idempotency_key = 'vacc-stage-review:' || tenant_id::text || ':' || goat_id::text,
    updated_at = now()
WHERE status = 'open';

-- Ensure the compatible open-unique target exists and remove any old-binary-breaking (tenant, goat)
-- UNIQUE index left by the unsafe draft. With a stage-free per-goat key, this index enforces one open
-- item per goat.
DROP INDEX IF EXISTS vaccination_stage_review_items_open_goat_unique;
CREATE UNIQUE INDEX IF NOT EXISTS vaccination_stage_review_items_open_idem_unique
  ON vaccination_stage_review_items (tenant_id, idempotency_key)
  WHERE status = 'open';

-- Typed resolution mode (VACC-REV-10): the API requires an explicit corrected|exception mode + note.
-- Nullable so pre-existing/old-binary resolutions (which don't set it) remain valid.
ALTER TABLE vaccination_stage_review_items
  ADD COLUMN IF NOT EXISTS resolution_mode text;
ALTER TABLE vaccination_stage_review_items
  ADD CONSTRAINT vaccination_stage_review_items_resolution_mode_check
  CHECK (resolution_mode IS NULL OR resolution_mode IN ('corrected', 'exception'));

-- +goose Down
ALTER TABLE vaccination_stage_review_items
  DROP CONSTRAINT IF EXISTS vaccination_stage_review_items_resolution_mode_check;
ALTER TABLE vaccination_stage_review_items
  DROP COLUMN IF EXISTS resolution_mode;
DROP INDEX IF EXISTS vaccination_stage_review_items_open_idem_unique;
CREATE UNIQUE INDEX IF NOT EXISTS vaccination_stage_review_items_open_goat_unique
  ON vaccination_stage_review_items (tenant_id, goat_id)
  WHERE status = 'open';
