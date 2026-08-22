-- Herd Signals: telemetry before an animal is mapped is DEVICE METRICS ONLY.
--
-- MAINTAINER RULE (2026-08-22). A BLE tag is commissioned, powered up and broadcasting long
-- before it is attached to an animal. Today the tags are bench units on a desk; on staging none
-- of them will be mapped at all. Everything they emit in that period is real, worth storing and
-- worth showing -- but it is telemetry ABOUT A DEVICE, not observation of an animal.
--
-- ANIMAL MONITORING STARTS AT THE MOMENT OF MAPPING, NOT BEFORE. The instant a tag is bound to a
-- goat_identifier, that tag's packets start meaning something about that animal. Everything
-- earlier is device history: the counter was advancing because someone carried the tag in a
-- pocket, jostled a bench, or drove it to a farm.
--
-- Why this needs a column rather than a convention: every animal-attributed number in this module
-- is computed over a WINDOW of history -- the p75 per-animal baseline (24h), the quiet/inactive
-- pattern (1-3h), the spike comparison, and every correlation against vaccination, feed, weighing
-- and health records. If those windows are allowed to reach back past the mapping instant they
-- silently blend bench movement into an animal's baseline, and the first thing the system would
-- tell a farm about a newly tagged goat would be derived from a tag rattling in a box.
--
-- animal_monitoring_since is that boundary, stamped when the mapping is made:
--   * packets BEFORE it  -> device telemetry. Still stored, still shown on device/gateway views.
--                           Never feeds a baseline, a pattern, an alert, or a correlation.
--   * packets AFTER it   -> animal observation. Everything animal-attributed reads from here on.
--
-- NULL means "not mapped to an animal yet", which is the normal state today and the state
-- staging ships in. A NULL monitoring boundary means NO animal-attributed value should be
-- produced for that tag at all -- not a zero, not a default: the tag simply has no animal.
--
-- The identifier carries the authoritative stamp (smart_tag_mapped_at) because mapping is an
-- identifier-level fact -- an animal can carry several identifiers, and re-tagging an animal
-- starts a NEW monitoring period for the new tag while the old one stops. herd_signal_tag_latest
-- carries a denormalised copy so the hot read path does not have to join to decide whether a
-- number may be attributed to an animal.

-- +goose Up
ALTER TABLE public.goat_identifiers
  ADD COLUMN IF NOT EXISTS smart_tag_mapped_at timestamptz;

COMMENT ON COLUMN public.goat_identifiers.smart_tag_mapped_at IS
  'When this identifier was bound to a smart tag. Animal monitoring starts here; packets before it are device telemetry only.';

ALTER TABLE public.herd_signal_tag_latest
  ADD COLUMN IF NOT EXISTS animal_monitoring_since timestamptz;

COMMENT ON COLUMN public.herd_signal_tag_latest.animal_monitoring_since IS
  'Denormalised from goat_identifiers.smart_tag_mapped_at. NULL = unmapped: device telemetry only, no animal-attributed value may be produced.';

-- Backfill: any identifier already flagged smart-tag-capable was mapped at some point we did not
-- record. Use its own updated_at rather than now() -- honest about when we learned of the binding,
-- and it keeps existing local/OCI test mappings from claiming their whole packet history as
-- animal observation.
UPDATE public.goat_identifiers
   SET smart_tag_mapped_at = COALESCE(smart_tag_mapped_at, updated_at)
 WHERE smart_tag_capable IS TRUE
   AND smart_tag_mapped_at IS NULL;

-- Partial index: the read path only asks this question for tags that ARE mapped.
CREATE INDEX IF NOT EXISTS herd_signal_tag_latest_monitoring_since_idx
  ON public.herd_signal_tag_latest (tenant_id, animal_monitoring_since)
  WHERE animal_monitoring_since IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS public.herd_signal_tag_latest_monitoring_since_idx;
ALTER TABLE public.herd_signal_tag_latest DROP COLUMN IF EXISTS animal_monitoring_since;
ALTER TABLE public.goat_identifiers DROP COLUMN IF EXISTS smart_tag_mapped_at;
