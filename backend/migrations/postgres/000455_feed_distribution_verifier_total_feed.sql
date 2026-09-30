-- +goose Up
-- seed-fixture-guard:ignore: nullable verifier-reading columns on an existing operational table,
-- a sampling-policy cleanup and a pending-queue field backfill; no vaccination/HRMS seed contract.
--
-- THE DISTRIBUTION VERIFIER RECORDS THE TOTAL FEED (maintainer decision 2026-09-28).
--
-- A feed-distribution verification item now carries ONE blind entry box, "Total feed given (kg)":
-- the verifier types the combined feed weight she reads off the operator's weight photo / video, and
-- Approve is held until she does. Distribution is not packing -- the feed is MIXED by the time it
-- reaches the trough -- so there is one number per pen-session, stored on the completion row itself.
-- A reading more than 5% from the planned pen-session total is warned once (direction only); the
-- plan it was checked against and her confirmation are stored beside it.
--
-- THREE PARTS, and the second and third are the lessons 000384 recorded for weighing:
--
--   1. The columns. All nullable: every row submitted before today carries no reading and none can
--      be invented for it. A rework re-submit clears them in the write path.
--   2. SAMPLING IS NOW LOCKED AT 100% for feed_distribution. SamplingWaivable() derives from
--      MeasurementCorrection.RequiredForApprove, so the panel and the write path lock it from the
--      code; but the queue predicate (samplingsql.InSample) still reads whatever row is stored, and
--      an undrawn item would be settled by nobody -- pending forever, its pen-session never
--      completed. So the stored rows are DELETED, not rewritten to 100 (a row reads as a setting).
--   3. THE ITEMS ALREADY WAITING. measurement_fields is composed AT ENQUEUE and stored, so every
--      distribution item already in the queue has an empty field list -- which the verification
--      service reads as a judge-the-video approve (the fail-open for an unreadable packing sheet).
--      Left alone, the first items she opens after the deploy would approve without a reading. So
--      the PENDING ones get the same one box a new item gets. Decided items keep what they were
--      decided on.

SET lock_timeout = '5s';

-- seed-migration-guard:ignore owner=manohark issue=feed-distribution-verifier-total-feed reason=nullable-verifier-reading-columns-written-only-at-verdict-no-seed-impact expiry=2026-12-31
ALTER TABLE public.feed_distribution_completions
  ADD COLUMN IF NOT EXISTS verified_feed_kg numeric(10,3),
  ADD COLUMN IF NOT EXISTS verified_planned_feed_kg numeric(10,3),
  ADD COLUMN IF NOT EXISTS verified_feed_variance_acknowledged boolean NOT NULL DEFAULT false,
  ADD COLUMN IF NOT EXISTS verified_feed_recorded_by uuid,
  ADD COLUMN IF NOT EXISTS verified_feed_recorded_at timestamptz;

-- seed-migration-guard:ignore owner=manohark issue=feed-distribution-verifier-total-feed reason=check-on-new-nullable-column-no-seed-impact expiry=2026-12-31
ALTER TABLE public.feed_distribution_completions
  DROP CONSTRAINT IF EXISTS feed_distribution_completions_verified_feed_kg_check,
  ADD CONSTRAINT feed_distribution_completions_verified_feed_kg_check
    CHECK (verified_feed_kg IS NULL OR (verified_feed_kg >= 0 AND verified_feed_kg <= 20000));

COMMENT ON COLUMN public.feed_distribution_completions.verified_feed_kg IS
  'The verifier''s reading of the COMBINED feed given to this pen-session (kg), typed at approve time (2026-09-28). NULL until approved with a reading; cleared on a rework re-submit.';
COMMENT ON COLUMN public.feed_distribution_completions.verified_planned_feed_kg IS
  'The planned pen-session total the reading was checked against at approve time (packing snapshot, else frozen sheet). NULL when no plan was readable. Never shown to the verifier.';
COMMENT ON COLUMN public.feed_distribution_completions.verified_feed_variance_acknowledged IS
  'True only when the reading sat more than 5% from the plan and the verifier confirmed it after the warning.';

-- The literal is the CATEGORY token (feeddirection/domain.VerificationCategoryFeed), pinned by
-- TestDistributionSamplingLockMigrationNamesTheRealCategory -- never the 'feed_direction' module key.
DELETE FROM public.verification_sampling_policies
WHERE category = 'feed_distribution';

-- seed-migration-guard:ignore owner=manohark issue=feed-distribution-verifier-total-feed reason=pending-verification-queue-field-backfill-new-items-compose-the-field-at-enqueue expiry=2026-12-31
UPDATE public.verification_items
SET measurement_fields = '[{"key": "total_feed", "label": "Total feed given (kg)"}]'::jsonb
WHERE category = 'feed_distribution'
  AND status = 'pending'
  AND measurement_fields = '[]'::jsonb;

-- +goose Down
-- The deleted sampling rows are not restored: they were settings for a category that can no
-- longer be sampled. The backfilled pending field lists are left in place (harmless without the
-- applier, which the down-migrated code does not register).
ALTER TABLE public.feed_distribution_completions
  DROP CONSTRAINT IF EXISTS feed_distribution_completions_verified_feed_kg_check,
  DROP COLUMN IF EXISTS verified_feed_recorded_at,
  DROP COLUMN IF EXISTS verified_feed_recorded_by,
  DROP COLUMN IF EXISTS verified_feed_variance_acknowledged,
  DROP COLUMN IF EXISTS verified_planned_feed_kg,
  DROP COLUMN IF EXISTS verified_feed_kg;
