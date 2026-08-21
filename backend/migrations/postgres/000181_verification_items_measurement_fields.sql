-- +goose Up
-- BLIND PER-ITEM MEASUREMENT ENTRY on a verification item (maintainer decision 2026-08-21).
--
-- Feed packing verification changes shape: instead of showing the verifier the frozen expected
-- ration ("Maize 12.5 kg · Soya 4 kg") and asking "does the video match?", the item now names ONLY
-- the feed items and the verifier types the quantity she can see packed for each one, then presses
-- Approve once (the approve carries the numbers -- the 2026-08-20 "THE APPROVE CARRIES THE NUMBER"
-- rule, extended from one value to one value per field). The planned quantities are deliberately
-- HIDDEN from her so she cannot copy them; the intended-vs-entered variance is computed by the
-- producing module and surfaced only on the leadership feed analytics execution view.
--
-- measurement_fields is the generic half: an ORDERED array of backend-composed {key, label} pairs
-- the PRODUCING module attaches at enqueue time -- for a feed packing item, one per feed item in
-- that pen-session's frozen sheet. `key` is the producer's stable token (posted back verbatim on
-- the verdict's measurement entries and resolved by the producer's applier); `label` is the
-- display string the entry box is captioned with. Verification stays ignorant of what the numbers
-- mean, exactly as with context_rows one column over: it stores the fields, enforces that an
-- approve on a required-for-approve category fills every one, and hands the entries to the
-- category's registered measurement applier.
--
-- Composed AT ENQUEUE and stored, like context_rows, so re-authoring the feed config cannot change
-- which fields a verifier is asked to fill for work already submitted.
--
-- Lock safety: constant DEFAULT (metadata-only on PG11+); array CHECK added NOT VALID then
-- validated so the write lock never spans a scan.
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '30s';

-- seed-migration-guard:ignore owner=manohark issue=feed-packing-blind-entry reason=no-seed-impact-additive-default-backed-jsonb-column-populated-only-at-runtime-enqueue expiry=2026-10-31
ALTER TABLE public.verification_items
  ADD COLUMN IF NOT EXISTS measurement_fields jsonb DEFAULT '[]'::jsonb NOT NULL;

-- seed-migration-guard:ignore owner=manohark issue=feed-packing-blind-entry reason=no-seed-impact-additive-default-backed-jsonb-column-populated-only-at-runtime-enqueue expiry=2026-10-31
ALTER TABLE public.verification_items
  DROP CONSTRAINT IF EXISTS verification_items_measurement_fields_is_array;
-- seed-migration-guard:ignore owner=manohark issue=feed-packing-blind-entry reason=no-seed-impact-additive-default-backed-jsonb-column-populated-only-at-runtime-enqueue expiry=2026-10-31
ALTER TABLE public.verification_items
  ADD CONSTRAINT verification_items_measurement_fields_is_array
  CHECK (jsonb_typeof(measurement_fields) = 'array') NOT VALID;
-- seed-migration-guard:ignore owner=manohark issue=feed-packing-blind-entry reason=no-seed-impact-additive-default-backed-jsonb-column-populated-only-at-runtime-enqueue expiry=2026-10-31
ALTER TABLE public.verification_items
  VALIDATE CONSTRAINT verification_items_measurement_fields_is_array;

COMMENT ON COLUMN public.verification_items.measurement_fields IS
  'Ordered [{"key","label"}] of per-item measurement entry fields the producing module attached at enqueue (e.g. one per feed item of a packing pen-session). key is the producer''s stable token echoed back on verdict measurement entries; label is the backend-owned caption. Empty for categories whose measurement is a single value or absent.';

-- +goose Down
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '30s';

-- seed-migration-guard:ignore owner=manohark issue=feed-packing-blind-entry reason=no-seed-impact-additive-default-backed-jsonb-column-populated-only-at-runtime-enqueue expiry=2026-10-31
ALTER TABLE public.verification_items
  DROP CONSTRAINT IF EXISTS verification_items_measurement_fields_is_array;
-- seed-migration-guard:ignore owner=manohark issue=feed-packing-blind-entry reason=no-seed-impact-additive-default-backed-jsonb-column-populated-only-at-runtime-enqueue expiry=2026-10-31
ALTER TABLE public.verification_items
  DROP COLUMN IF EXISTS measurement_fields;
