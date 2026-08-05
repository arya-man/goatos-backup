-- +goose Up
-- KID vs ADULT becomes a property of the COHORT TAG, and moves with a shifting.
--
-- Maintainer decision 2026-08-05. Nothing in this system ever promoted an animal from
-- kid to adult: goats.age_band was written once at seed/import and then never again by
-- any code path in backend/internal/**, and goats.management_stage only changes through
-- an admin edit or a shifting. A kid that stayed in its shed stayed a kid forever, and a
-- kid that WAS moved into an adult shed adopted the adult cohort tag while its age_band
-- kept saying "Kid".
--
-- The rule comes from the farm's own data (docs/runbooks/CBE-CPT-goats.json, 1,670 live
-- animals across CBE + CPT). Every one of the ten cohort tags in use maps to exactly one
-- age band, with ZERO counterexamples:
--
--     kid   : F2-Male 497, F2-Female 170, K2 91, K3 63, ICU-Kid 36, K1 4   = 861
--     adult : Non-Pregnant 764, Buck 39, Mother 5, Warmup 1               = 809
--
-- ONE DELIBERATE DEPARTURE FROM THAT DATA: Warmup is classified KID here, not adult (maintainer
-- decision 2026-08-05, stated after reviewing the table above). The single live Warmup animal in the
-- source is 59.9 weeks old and the sheet labels it Adult, so this assignment CONTRADICTS the source
-- and the backfill below flips that animal to kid. That is intended. Do NOT "repair" it back by
-- re-deriving Warmup from the sheet or from age -- the sheet is the record of the old
-- classification, and the maintainer's rule is the current one.
--
-- The mapping is deliberately NOT age-derived, and that is the whole point. Checking the
-- same rows against DOB shows F2-Male animals up to 67 weeks old that the farm still
-- calls kids, and K2 animals up to 55 weeks. "Kid" here means "not yet in a breeding /
-- adult cohort" -- an operational classification the farm makes by placement, not a
-- birthday. A DOB-based promotion (>20 weeks => adult) would have flipped 261 of these
-- animals against the farm's own record. So the tag is the authority, and the tag changes
-- when the animal is shifted.
--
-- WHY THE LOOKUP TABLE AND NOT GO CODE: animal_stage_lookup is already the tenant's
-- editable stage vocabulary (it is what a relocation validates its target against), so
-- adding another farm-managed cohort tag must not require a backend release. This is the
-- "business-managed vocabularies live in Postgres, not in backend code" rule from
-- AGENTS.md.
--
-- CLINICAL STAGES STAY NULL, BY DESIGN. ICU and Quarantine carry NULL age_band, so a move
-- that somehow resolved to one of them would leave age_band untouched rather than
-- reclassify a sick animal. (Those tags are already rejected earlier -- a raise resolves
-- them to "keep current" and identity.resolveDestinationTag returns ErrClinicalDestinationTag
-- -- so NULL here is the third, belt-and-braces layer, not the only one.)
ALTER TABLE public.animal_stage_lookup
  ADD COLUMN IF NOT EXISTS age_band text;

ALTER TABLE public.animal_stage_lookup
  DROP CONSTRAINT IF EXISTS animal_stage_lookup_age_band_check;

ALTER TABLE public.animal_stage_lookup
  ADD CONSTRAINT animal_stage_lookup_age_band_check
  CHECK (age_band IS NULL OR age_band IN ('kid', 'adult'));

-- Classify the seeded vocabulary. Matched case-insensitively on stage_code because the
-- vocabulary is authored free text ("Non-Pregnant", "non-pregnant") while age_band is a
-- closed two-value domain.
--
-- Kid cohorts. K0-K3 are the milk/weaning ladder; F2* is fattening/grow-out, which the
-- farm classifies as kid regardless of how old the animal gets (see above -- this is the
-- single most important row in the mapping and the one an age rule gets wrong). Warmup, the
-- post-arrival settling cohort, is kid by the maintainer override recorded above.
UPDATE public.animal_stage_lookup
   SET age_band = 'kid', updated_at = now()
 WHERE lower(btrim(stage_code)) IN ('k0', 'k1', 'k2', 'k3', 'f2', 'f2-male', 'f2-female', 'warmup')
   AND age_band IS DISTINCT FROM 'kid';

-- Adult cohorts. Buck/Mother/Milking/Pregnant/Non-Pregnant are breeding cohorts; M0
-- ("Mother newborn") is the DAM that has just delivered, not her kid.
UPDATE public.animal_stage_lookup
   SET age_band = 'adult', updated_at = now()
 WHERE lower(btrim(stage_code)) IN ('buck', 'mother', 'milking', 'm0', 'pregnant', 'non-pregnant')
   AND age_band IS DISTINCT FROM 'adult';

-- Backfill the live herd from its CURRENT cohort tag.
--
-- Without this the column would be trustworthy only for animals that happen to be shifted
-- after today, which is exactly the "migration ships a derived column and the app reads it
-- empty" defect the seed/migration coupling rule exists to prevent. It also repairs a live
-- miscount: public.herd_register_is_kid falls back to a 'K%' prefix test on
-- management_stage whenever age_band is not already 'kid'/'adult', so every F2-Male /
-- F2-Female animal (667 of them, all kids) has been counting as an ADULT in the Herd
-- Register. Populating age_band makes that fallback unreachable for classified animals.
--
-- Values are normalized to lowercase, including rows the source sheet imported as
-- 'Kid'/'Adult'. Every consumer already compares case-insensitively
-- (herd_register_is_kid lowercases; the health contract's enum is [adult, kid]), so this
-- costs nothing and leaves one convention in the column instead of two.
--
-- goats is a hot table, so the backfill runs under a bounded lock_timeout: if it cannot take its
-- row locks promptly it fails the deploy fast with a lock error rather than queueing behind a long
-- transaction and blocking every concurrent write to the herd. Re-running the migration is safe.
--
-- The UPDATE deliberately touches age_band, which is in the column list of
-- herd_register_goats_after_write_trg, so the Herd Register projection re-derives itself
-- as part of this migration rather than waiting for the next write to each animal.
-- row_version is left alone: it is the optimistic-lock token for admin writes, and a
-- schema backfill is not a business mutation that should invalidate a client's read.
SET lock_timeout = '5s';

UPDATE public.goats g
   SET age_band = a.age_band, updated_at = now()
  FROM public.animal_stage_lookup a
 WHERE a.tenant_id = g.tenant_id
   AND a.status = 'active'
   AND a.age_band IS NOT NULL
   AND lower(btrim(coalesce(g.management_stage, ''))) = lower(btrim(a.stage_code))
   AND g.merged_into_goat_id IS NULL
   AND g.exited_at IS NULL
   AND g.age_band IS DISTINCT FROM a.age_band;

RESET lock_timeout;

-- +goose Down
-- Only the vocabulary column is reversible. The goats.age_band backfill above overwrote
-- the previous per-animal values (mostly 'Kid'/'Adult' from the source import, or NULL),
-- and the pre-migration value is not recoverable from any surviving row -- rolling it back
-- would mean re-deriving the same answer a different way. Re-running the Up migration is
-- idempotent, so a roll-forward is the supported repair.
ALTER TABLE public.animal_stage_lookup
  DROP CONSTRAINT IF EXISTS animal_stage_lookup_age_band_check;

ALTER TABLE public.animal_stage_lookup
  DROP COLUMN IF EXISTS age_band;
