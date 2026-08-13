-- +goose Up
-- Feed DISTRIBUTION gains a THIRD mandatory proof: the FEED WEIGHT PHOTO.
--
-- Maintainer decision 2026-08-11. A shed-session's distribution is now proved by three captures,
-- in this order:
--
--   1. feed_weight_proof_ref   -- PHOTO of the weighed feed, taken BEFORE it is given out.
--   2. distribution_proof_ref  -- VIDEO of the feed being distributed.       (unchanged)
--   3. water_proof_ref         -- VIDEO of the water being distributed.      (was photo OR video)
--
-- WHY THE WEIGHT PHOTO COMES FIRST, AND WHY IT IS A PHOTO
-- ------------------------------------------------------
-- The distribution video proves the feed reached the animals; it cannot prove HOW MUCH reached
-- them, because a scale reading is not legible in a clip of feed being poured. The weight photo is
-- the only capture that can be checked against the expected ration the verifier is already shown on
-- the item. It is ordered first because it must be taken while the feed is still on the scale --
-- after distribution there is nothing left to weigh, so a later capture could only ever be staged.
--
-- WHY WATER IS NOW VIDEO-ONLY
-- ---------------------------
-- A photo of a full trough proves a trough is full, not that this operator filled it today. The
-- distribution video was always held to that standard; water is now held to it too.
--
-- WHERE THE MEDIA-TYPE RULE LIVES (it is NOT here)
-- -----------------------------------------------
-- "photo" and "video" are properties of proof_artifacts, which belongs to the PROOF module. This
-- table stores only references, so a CHECK here could not read the type without reaching across a
-- module boundary. The type rule is enforced in the write path by feeddirection's proof validator
-- (adapters/proof.Validator.ValidateFeedProofMedia), which is where the existing live-camera rule
-- for transport already lives. What this migration enforces is PRESENCE.
--
-- GRANDFATHERING -- WHY THE CHECK IS `NOT VALID` AND MUST STAY THAT WAY
-- --------------------------------------------------------------------
-- Maintainer decision: sessions already submitted, and items sitting in the verifier queue right
-- now, keep their existing pair of proofs and are verdicted normally. Nobody re-shoots work already
-- done, and no verifier queue is emptied by a deploy.
--
-- `NOT VALID` is exactly that rule expressed in the database: the constraint binds every INSERT and
-- UPDATE from this point on, while the rows already on disk are never scanned or rejected. Do NOT
-- "tidy this up" with a later VALIDATE CONSTRAINT -- validating it would fail on precisely the
-- legacy rows this decision protects, and if it somehow passed it would mean the grandfathered rows
-- are gone and the constraint is doing nothing.
--
-- WHY THIS CHECK COVERS 'pending_verification' ONLY, AND NOT 'completed'
-- ----------------------------------------------------------------------
-- This is the subtle half, and getting it wrong breaks the grandfathering the paragraph above
-- promises. A NOT VALID check exempts the rows already on disk from the initial scan, but it IS
-- enforced on any later UPDATE of those rows. Verifier approval is an UPDATE
-- (status 'pending_verification' -> 'completed'). So a check that also covered 'completed' would
-- re-examine every grandfathered row at the exact moment a verifier tried to approve it, find the
-- NULL weight photo, and refuse -- making every item already in the queue on deploy day permanently
-- un-approvable. That is the opposite of grandfathering them, and it was caught by
-- TestGrandfatheredDistributionRowWithoutWeightPhotoStillVerifies rather than in review.
--
-- Narrowing to 'pending_verification' costs nothing, because 'completed' is reachable ONLY from
-- 'pending_verification': ApplyVerifiedDistribution reads the row FOR UPDATE, returns early unless
-- the status is 'pending_verification', and its UPDATE carries the same predicate again. A row
-- written by the current binary therefore cannot reach 'completed' without having passed through a
-- 'pending_verification' state this check already enforced. Covering 'completed' would add no
-- protection for new rows and would break every old one.
--
-- The re-submit path stays correctly enforced, and that IS intended: a rejected row goes to 'rework'
-- (exempt, so the proofs can be cleared for the re-shoot) and comes back to 'pending_verification',
-- where this check fires. A re-shoot is new work and is captured under the new rule -- including for
-- a grandfathered row, which is why TestGrandfatheredRowReSubmitMustCarryWeightPhoto exists.
--
-- If a future change ever lets something write 'completed' directly, this check must be widened to
-- cover it in the SAME change -- and that widening will need a plan for the legacy rows.
--
-- LOCK SAFETY
-- -----------
-- ADD COLUMN with no DEFAULT and no NOT NULL is a catalog-only change in PostgreSQL 11+ (no table
-- rewrite). ADD CONSTRAINT ... NOT VALID takes ACCESS EXCLUSIVE only for the catalog write and does
-- NOT scan the table. Both are O(1) against a table that already holds every distribution
-- completion the farm has ever submitted.

-- ---------------------------------------------------------------------------
-- 1. The weight photo reference
-- ---------------------------------------------------------------------------
-- Nullable, deliberately and permanently: a grandfathered row genuinely has no weight photo, and a
-- NOT NULL column would have to invent one. Presence is enforced by state below, not by the column.
ALTER TABLE public.feed_distribution_completions
  ADD COLUMN IF NOT EXISTS feed_weight_proof_ref text;

COMMENT ON COLUMN public.feed_distribution_completions.feed_weight_proof_ref IS
  'MANDATORY (from 2026-08-11) proof_id of the FEED WEIGHT PHOTO, captured before distribution. Server-minted via /app/proofs/*; the bytes live in GCS and only the reference is stored. NULL only on rows submitted before this rule existed -- see the NOT VALID check below.';

-- ---------------------------------------------------------------------------
-- 2. Presence, scoped to the SUBMITTED state only
-- ---------------------------------------------------------------------------
-- A row entering the verifier's queue must carry its weight photo. 'rework' is exempt so the proofs
-- can be cleared for a re-shoot, and 'completed' is exempt for the grandfathering reason set out
-- above -- it is reachable only from 'pending_verification', which this already guards.
--
-- Kept as a SEPARATE constraint rather than folded into feed_distribution_completions_proof_check so
-- the grandfathering is legible: the original pair check stays VALID and fully enforced on every
-- row, and only the new requirement carries the NOT VALID exemption and the narrower state set.
ALTER TABLE public.feed_distribution_completions
  ADD CONSTRAINT feed_distribution_completions_weight_proof_check CHECK (
    status <> 'pending_verification'
    OR (feed_weight_proof_ref IS NOT NULL AND btrim(feed_weight_proof_ref) <> '')
  ) NOT VALID;

-- +goose Down
-- +goose NO TRANSACTION
-- Forward-only. Dropping the column would discard weight photos already captured against it.
