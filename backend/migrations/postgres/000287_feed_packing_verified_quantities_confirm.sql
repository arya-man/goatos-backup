-- +goose Up
-- THE VERIFIER IS WARNED, NOT TOLD (maintainer decision 2026-09-09).
--
-- Feed packing review stays blind per-item entry (000182): the verifier reads each packed weight off
-- the video and never sees the plan. A reading more than 500 g away from the planned quantity is now
-- refused once with a direction-only warning ("more than 500 g above/below the plan"), and lands only
-- when she confirms she is sure. Two columns record that on the reading itself:
--
--   planned_kg            the planned quantity the reading was checked against at approve time,
--                         NULL when no plan could be read for that item (the packed-against snapshot
--                         on the completion, falling back to the frozen sheet). Frozen here so the
--                         record says what she was warned against even if the sheet is corrected later.
--   variance_acknowledged TRUE only on a reading that WAS flagged and that she then confirmed. A
--                         reading inside the tolerance is never acknowledged; there was nothing to
--                         confirm.
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '30s';

ALTER TABLE public.feed_packing_verified_quantities
  ADD COLUMN IF NOT EXISTS planned_kg numeric(10,3),
  ADD COLUMN IF NOT EXISTS variance_acknowledged boolean NOT NULL DEFAULT false;

COMMENT ON COLUMN public.feed_packing_verified_quantities.planned_kg IS
  'Planned quantity (kg) the verifier''s reading was checked against at approve time; NULL when no plan was readable for the item.';
COMMENT ON COLUMN public.feed_packing_verified_quantities.variance_acknowledged IS
  'TRUE when the reading was more than the confirm tolerance (500 g) away from planned_kg and the verifier confirmed it anyway.';

-- +goose Down
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '30s';

ALTER TABLE public.feed_packing_verified_quantities
  DROP COLUMN IF EXISTS variance_acknowledged,
  DROP COLUMN IF EXISTS planned_kg;
