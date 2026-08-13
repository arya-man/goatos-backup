-- +goose Up
-- +goose NO TRANSACTION
-- Reopening a lump-sum weighing bucket must WITHDRAW its submission, not erase it.
--
-- weighing_shed_observations holds one row per (tenant_id, campaign_shed_id) --
-- weighing_shed_observations_one_active_scope_uidx -- so a reopened bucket could
-- never be submitted again: the resubmit INSERT tripped 23505. The first fix for
-- that hard-DELETEd the row inside ReopenScope. That is wrong twice over:
--
--   1. It destroys an operator's rejected proof attempt. Rejected attempts are
--      immutable history (AGENTS.md): the record of what was submitted, by whom,
--      with which video, is exactly what a rework dispute is adjudicated on.
--   2. The row is the resource a verification item and an idempotency record both
--      point at. Deleting it leaves both dangling.
--
-- withdrawn_at supersedes instead of deleting. The submission stays on the table
-- as history; only the one-active-submission uniqueness is relaxed to the OPEN
-- submissions, so the bucket accepts a fresh one.
--
-- Nullable, no default, no backfill: a catalog-only change in Postgres (no table
-- rewrite, no long ACCESS EXCLUSIVE hold). Every existing row is an open
-- submission and stays one.
ALTER TABLE public.weighing_shed_observations
  ADD COLUMN IF NOT EXISTS withdrawn_at timestamptz;

COMMENT ON COLUMN public.weighing_shed_observations.withdrawn_at IS
  'Set by ReopenScope when leadership reopens the bucket. Non-NULL = superseded submission kept as history: it is not the bucket''s accepted work, it does not hold the one-open-submission slot, and it owes the verifier no verdict.';

-- The uniqueness that made the bucket resubmittable-once becomes uniqueness over
-- the OPEN submissions. Built CONCURRENTLY under its own name first, so the table
-- is never left without the guarantee: while both exist, every row has
-- withdrawn_at IS NULL, so the partial index covers exactly what the old one did.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS weighing_shed_observations_one_open_scope_uidx
  ON public.weighing_shed_observations (tenant_id, campaign_shed_id)
  WHERE withdrawn_at IS NULL;

DROP INDEX CONCURRENTLY IF EXISTS public.weighing_shed_observations_one_active_scope_uidx;

-- A withdrawn submission's verification item must stop being actionable in the
-- SAME breath: otherwise a verifier approves an item whose observation is no
-- longer the bucket's work, and the UI reports a success that decides nothing.
-- 'withdrawn' is a fourth terminal status rather than a new column because every
-- actionable path in the verification repository is already gated on
-- status = 'pending' (the verdict UPDATE, the queue's status filter, the
-- pending/approved/rejected roll-ups), so a withdrawn item drops out of all of
-- them without touching a single read.
--
-- NOT VALID + VALIDATE keeps the ACCESS EXCLUSIVE hold to the catalog flip; the
-- scan that follows takes only SHARE UPDATE EXCLUSIVE.
ALTER TABLE public.verification_items
  DROP CONSTRAINT IF EXISTS verification_items_status_check;

ALTER TABLE public.verification_items
  ADD CONSTRAINT verification_items_status_check
  CHECK (status = ANY (ARRAY['pending'::text, 'approved'::text, 'rejected'::text, 'withdrawn'::text]))
  NOT VALID;

ALTER TABLE public.verification_items
  VALIDATE CONSTRAINT verification_items_status_check;

-- +goose Down
-- +goose NO TRANSACTION
UPDATE public.verification_items SET status = 'rejected', verdict_reason = COALESCE(verdict_reason, 'withdrawn')
WHERE status = 'withdrawn';

ALTER TABLE public.verification_items
  DROP CONSTRAINT IF EXISTS verification_items_status_check;

ALTER TABLE public.verification_items
  ADD CONSTRAINT verification_items_status_check
  CHECK (status = ANY (ARRAY['pending'::text, 'approved'::text, 'rejected'::text]))
  NOT VALID;

ALTER TABLE public.verification_items
  VALIDATE CONSTRAINT verification_items_status_check;

DELETE FROM public.weighing_shed_observations WHERE withdrawn_at IS NOT NULL;

CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS weighing_shed_observations_one_active_scope_uidx
  ON public.weighing_shed_observations (tenant_id, campaign_shed_id);

DROP INDEX CONCURRENTLY IF EXISTS public.weighing_shed_observations_one_open_scope_uidx;

ALTER TABLE public.weighing_shed_observations
  DROP COLUMN IF EXISTS withdrawn_at;
