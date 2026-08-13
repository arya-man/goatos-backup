-- +goose Up
-- +goose NO TRANSACTION
--
-- Batch the rework push PER SHED (maintainer decision 2026-08-03).
--
-- A rework verdict arrives per observation, asynchronously, one event at a time.
-- There is no "the verifier finished reviewing this shed" moment anywhere in the
-- system, so a verifier who rejects five of a shed's fifteen captures used to send
-- the operator FIVE separate pushes for one trip back to one shed. At real herd
-- sizes that is a notification storm for a single re-open.
--
-- The grouping moment is INVENTED here, and it is invented as DERIVED STATE rather
-- than as a queue: an observation that has been bounced but not yet told to the
-- operator is exactly `verification_status='rework' AND rework_notified_at IS NULL`.
-- A bounded sweeper (weighing-rework-digest, on the existing 5-minute operational
-- kernel lane) groups those rows by bucket, emits ONE weighing.observation.rework_digest
-- event per bucket naming the animals, and stamps rework_notified_at on exactly the
-- rows it named -- in the same transaction, so the stamp and the event cannot diverge.
--
-- Why a nullable column and not a digest table:
--   * the fact "this bounce has not reached the operator yet" belongs to the bounce,
--     not to a parallel outbox that can drift from it;
--   * a re-submission that is bounced AGAIN clears the stamp back to NULL in the same
--     UPDATE that sets verification_status='rework' (verification_verdict.go), so the
--     second bounce re-enters the digest with no reconciliation logic anywhere;
--   * nothing can be silently dropped: a rejection that lands after its shed's digest
--     already fired simply has rework_notified_at IS NULL and is picked up by the next
--     tick as its own (smaller) digest. There is no window in which a bounce exists
--     and no digest will ever name it.
--
-- LUMP-SUM IS DELIBERATELY NOT HERE. weighing_shed_observations carries ONE capture
-- for the whole shed, so its rework push is already one-per-shed and is left on the
-- immediate path. Routing it through a digest would only add latency.
--
-- LOCK SAFETY:
--   * ADD COLUMN of a NULLABLE column with NO DEFAULT is a catalog-only change in
--     PG11+ -- no table rewrite, ACCESS EXCLUSIVE held for the catalog update only.
--   * the sweeper's index is built CONCURRENTLY under NO TRANSACTION so it takes no
--     ACCESS EXCLUSIVE lock on weighing_observations, which is on the operator's hot
--     scan-and-submit write path.
--
-- ISOLATION: weighing is free-flow. This touches weighing_observations alone and
-- references no goat, herd, obligation, protocol, or vaccination object.
ALTER TABLE public.weighing_observations
  ADD COLUMN IF NOT EXISTS rework_notified_at timestamptz;

-- Serves the digest sweeper's claim query:
--
--   WHERE tenant_id = $1
--     AND verification_status = 'rework'
--     AND rework_notified_at IS NULL
--   ORDER BY campaign_shed_id, verified_at
--
-- PARTIAL on exactly the un-notified rework set, which is a handful of rows at any
-- instant even at 50k animals: the index stays tiny and every verified/pending
-- observation is excluded from it entirely. verified_at is in the key because the
-- sweeper's quiet-window and max-age decisions are both computed from it.
CREATE INDEX CONCURRENTLY IF NOT EXISTS weighing_observations_rework_undelivered_idx
ON public.weighing_observations (tenant_id, campaign_shed_id, verified_at)
WHERE verification_status = 'rework' AND rework_notified_at IS NULL;

-- +goose Down
-- +goose NO TRANSACTION
DROP INDEX CONCURRENTLY IF EXISTS public.weighing_observations_rework_undelivered_idx;
ALTER TABLE public.weighing_observations DROP COLUMN IF EXISTS rework_notified_at;
