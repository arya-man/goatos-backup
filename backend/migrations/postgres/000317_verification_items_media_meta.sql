-- +goose Up
-- +goose NO TRANSACTION
-- EACH PROOF NAMES ITSELF (WEIGHING SOP, maintainer decision 2026-09-15).
--
-- The verifier's queue labelled and typed every proof BY POSITION from the category registry:
-- weighing_fasting declared ["Feed removal video", "Water removal video"], both video/mp4. That
-- was exact while the removal card was two fixed clips. The card's captures are now the pinned
-- SOP's authored slots -- any number, video / photo / either -- so on 2026-09-15 a water PHOTO
-- reached the drawer labelled "Water removal video" with mime video/mp4, and a third slot would
-- have been "Proof 3".
--
-- media_meta is the generic half, one column over from context_rows and measurement_fields: an
-- ORDERED array of backend-composed {label, kind} pairs, POSITIONAL against media_refs, attached
-- by the PRODUCING module at enqueue. Verification stays ignorant of what a slot means; it renders
-- the label and picks the player from the kind, falling back to the registry's positional copy for
-- an item (or a producer) that carries none -- which is every item written before this column.
--
-- Composed AT ENQUEUE and stored, like context_rows, so re-authoring the SOP cannot relabel
-- evidence already submitted under an earlier version.
--
-- Lock safety: constant DEFAULT (metadata-only), array CHECK added NOT VALID then validated in a
-- separate autocommit step so the write lock never spans a scan.
SET lock_timeout = '2s';
SET statement_timeout = '30s';
-- seed-migration-guard:ignore owner=manohark issue=weighing-sop-authored-slots reason=no-seed-impact-additive-default-backed-jsonb-column-populated-only-at-runtime-enqueue expiry=2026-11-30
ALTER TABLE public.verification_items
  ADD COLUMN IF NOT EXISTS media_meta jsonb DEFAULT '[]'::jsonb NOT NULL;
RESET lock_timeout;
RESET statement_timeout;

SET lock_timeout = '2s';
SET statement_timeout = '30s';
-- seed-migration-guard:ignore owner=manohark issue=weighing-sop-authored-slots reason=no-seed-impact-additive-default-backed-jsonb-column-populated-only-at-runtime-enqueue expiry=2026-11-30
ALTER TABLE public.verification_items
  DROP CONSTRAINT IF EXISTS verification_items_media_meta_is_array;
-- seed-migration-guard:ignore owner=manohark issue=weighing-sop-authored-slots reason=no-seed-impact-additive-default-backed-jsonb-column-populated-only-at-runtime-enqueue expiry=2026-11-30
ALTER TABLE public.verification_items
  ADD CONSTRAINT verification_items_media_meta_is_array
  CHECK (jsonb_typeof(media_meta) = 'array') NOT VALID;
RESET lock_timeout;
RESET statement_timeout;

SET lock_timeout = '2s';
SET statement_timeout = '30s';
-- seed-migration-guard:ignore owner=manohark issue=weighing-sop-authored-slots reason=no-seed-impact-additive-default-backed-jsonb-column-populated-only-at-runtime-enqueue expiry=2026-11-30
ALTER TABLE public.verification_items
  VALIDATE CONSTRAINT verification_items_media_meta_is_array;
RESET lock_timeout;
RESET statement_timeout;

COMMENT ON COLUMN public.verification_items.media_meta IS
  'Ordered [{"label","kind"}] positional against media_refs, attached by the producing module at enqueue: the backend-owned header and the media kind (video / photo) of each proof. Empty for items written before authored capture slots; the queue then falls back to the category registry''s positional copy.';

-- +goose Down
-- +goose NO TRANSACTION
SET lock_timeout = '2s';
SET statement_timeout = '30s';
-- seed-migration-guard:ignore owner=manohark issue=weighing-sop-authored-slots reason=no-seed-impact-additive-default-backed-jsonb-column-populated-only-at-runtime-enqueue expiry=2026-11-30
ALTER TABLE public.verification_items
  DROP CONSTRAINT IF EXISTS verification_items_media_meta_is_array;
-- seed-migration-guard:ignore owner=manohark issue=weighing-sop-authored-slots reason=no-seed-impact-additive-default-backed-jsonb-column-populated-only-at-runtime-enqueue expiry=2026-11-30
ALTER TABLE public.verification_items
  DROP COLUMN IF EXISTS media_meta;
RESET lock_timeout;
RESET statement_timeout;
