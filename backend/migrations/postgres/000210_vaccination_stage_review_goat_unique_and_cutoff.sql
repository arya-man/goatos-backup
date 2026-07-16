-- +goose Up
-- VACC-REV-10 hardening:
-- (a) Enforce one OPEN review item per (tenant, goat) at the DATABASE layer, so uniqueness holds across
--     ALL live writers during a rollout — not just the bridge writer that takes the advisory lock. A
--     writer's DUPLICATE insert (a second open row for a goat that already has one) now FAILS CLOSED
--     against this unique index (error, no dup) instead of silently creating a second open row.
--
--     Migrate-first safety: we KEEP the pre-existing (tenant, idempotency_key) open index alongside the
--     new (tenant, goat) one. A still-live predecessor binary records via
--     `ON CONFLICT (tenant_id, idempotency_key) WHERE status='open'`; if that index were dropped, EVERY
--     predecessor write — including the first, non-duplicate insert for a goat — would fail with 42P10
--     ("no unique or exclusion constraint matching the ON CONFLICT specification"). Keeping both indexes
--     lets the predecessor create/update the FIRST open row normally, while the (tenant, goat) index
--     rejects only an actual SECOND (stage-keyed) open row for the same goat. The idempotency-key index
--     can be dropped in a LATER release once all predecessor binaries are drained. The bridge writer's
--     update-else-insert (and its `ON CONFLICT (tenant, goat) DO NOTHING` insert) respects both indexes.
-- (b) Persist the age cutoff that RAISED each item, so the 'corrected' re-check evaluates against the
--     exact effective procurement policy that flagged the goat — not a value re-derived from MIN across
--     all published versions (which wrongly includes expired / superseded / differently-scoped ones).
--     Existing / predecessor-written rows keep age_cutoff_weeks = NULL; a NULL cutoff is treated as
--     UNKNOWN and fails the 'corrected' re-check closed (never defaulted to an invented value).

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

-- NB: the (tenant_id, idempotency_key) open index is intentionally RETAINED this release for predecessor
-- ON CONFLICT compatibility (see header). Do not drop it here.

-- +goose Down
DROP INDEX IF EXISTS vaccination_stage_review_items_open_goat_unique;
ALTER TABLE vaccination_stage_review_items DROP COLUMN IF EXISTS age_cutoff_weeks;
