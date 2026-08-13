-- +goose Up
-- +goose NO TRANSACTION
-- Adds the per-row "submitted" marker weighing_observations needs to
-- distinguish an un-submitted draft-round scan (may still be updated in
-- place on rescan) from a scan that was already accepted into a completed
-- round (a rescan of the same tag after ReopenScope must be treated as a
-- NEW, distinct capture, not silently merged into the old submitted row).
--
-- Nullable ADD COLUMN is a fast metadata-only change on Postgres (no table
-- rewrite, no long lock) so it does not need NO TRANSACTION on its own, but
-- this migration also adds a supporting CONCURRENTLY index for the
-- duplicate-scan lookup (tenant/campaign/shed/business-day/tag), which does
-- require running outside a transaction. Both statements are additive only:
-- no data is rewritten, no constraint narrows existing rows.
ALTER TABLE public.weighing_observations
  ADD COLUMN IF NOT EXISTS submitted_at timestamptz NULL;

-- BACKFILL — required, not optional.
--
-- The duplicate gate tests `submitted_at IS NULL` to mean "still the current
-- draft round". Without this backfill every observation that was already
-- accepted into a completed/closed bucket BEFORE this migration would read as
-- an un-submitted draft forever, because nothing else ever stamps a historic
-- row. Reopening such a bucket and rescanning the same tag would then take the
-- update-in-place branch and silently OVERWRITE the earlier submitted
-- observation -- which is exactly the data loss this column exists to prevent,
-- reappearing for every bucket that predates the deploy.
--
-- A bucket in 'completed' or 'closed' has, by definition, been submitted, so
-- its rows are stamped with the bucket's completion time (falling back to the
-- observation's own accepted_at when completed_at was never recorded). This is
-- a one-off additive stamp; it never clears or moves an existing value.
UPDATE public.weighing_observations wo
SET submitted_at = COALESCE(cs.completed_at, wo.accepted_at)
FROM public.weighing_campaign_sheds cs
WHERE cs.tenant_id = wo.tenant_id
  AND cs.campaign_shed_id = wo.campaign_shed_id
  AND cs.status IN ('completed', 'closed')
  AND wo.submitted_at IS NULL;

-- Supports the duplicate-scan gate: for a given bucket, is there already a
-- SUBMITTED row for this same (case-insensitive, trimmed) tag? The
-- expression index matches the lookup predicate exactly so the planner can
-- serve it without a table scan.
CREATE INDEX CONCURRENTLY IF NOT EXISTS weighing_observations_shed_submitted_tag_idx
ON public.weighing_observations (tenant_id, campaign_id, campaign_shed_id, (lower(btrim(scanned_identifier))))
WHERE submitted_at IS NOT NULL;

-- +goose Down
-- +goose NO TRANSACTION
DROP INDEX CONCURRENTLY IF EXISTS public.weighing_observations_shed_submitted_tag_idx;
ALTER TABLE public.weighing_observations
  DROP COLUMN IF EXISTS submitted_at;
