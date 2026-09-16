-- +goose Up
-- SHIFTING SOP rework rounds (coordinator follow-up to the 2026-09-17 E2E, docs/decisions/shifting-sop.md).
--
-- A verifier REJECT of a movement's evidence starts a new review round. The verifier item's key was
-- the event plus its capture refs only, so a resubmit naming the SAME capture (accepted, deploy-day
-- parity) collapsed onto the REJECTED item: the movement went back to unverified with nothing for
-- the verifier to judge. verification_round counts the rework verdicts applied to the movement; the
-- key folds it in only after the first rework (round 0 keeps the pre-SOP key byte for byte, so an
-- older phone's retry still collapses), and the rework bounce is fenced on it so a redelivered
-- verdict for an older round changes nothing.
--
-- Lock safety: ADD COLUMN with a constant DEFAULT is metadata-only on PostgreSQL 11+ (no rewrite);
-- the ACCESS EXCLUSIVE lock is held only for the catalog update, bounded by lock_timeout.
SET lock_timeout = '5s';
ALTER TABLE public.shifting_events
  ADD COLUMN IF NOT EXISTS verification_round integer NOT NULL DEFAULT 0;
COMMENT ON COLUMN public.shifting_events.verification_round IS
  'Rework verdicts applied to this movement''s evidence (0 until the first). Folded into the verifier item key after a rework so a resubmit of the same capture is a fresh review; the rework bounce is fenced on it (SHIFTING SOP, 2026-09-17).';

-- +goose Down
SET lock_timeout = '5s';
ALTER TABLE public.shifting_events DROP COLUMN IF EXISTS verification_round;
