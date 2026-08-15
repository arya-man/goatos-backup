-- +goose Up
-- A shifting into a FLUSHING pen now stamps that pen's tag (maintainer decision 2026-08-15).
--
-- This reverses a standing carve-out. Flushing is a NUTRITION cohort -- a non-pregnant female put on
-- extra ration to prepare her for breeding -- and until now a movement deliberately refused to adopt
-- it, on the grounds that walking into a pen is a PLACEMENT decision and should not silently make a
-- FEEDING decision. The maintainer was shown that exact consequence and chose to accept it: a move
-- into a flushing pen puts the animal on flushing ration and re-keys her vaccination schedule.
--
-- Two things had to change together, because either alone is a no-op:
--
--   1. counts/domain.resolveConfiguredStage special-cased the string 'Flushing' and returned
--      keep-current. That branch is deleted.
--   2. THIS TABLE. ResolveShiftingDestinationPenStage only returns a stage that is in the WRITABLE
--      vocabulary (an active animal_stage_lookup row), and 'Flushing' was never listed -- the Go
--      comment said so explicitly. So even with the special case gone, an unlisted tag resolves to
--      "" = keep current. Listing it here is the other half.
--
-- Same shape as 000167, which did this for the two clinical KID pens.
--
-- age_band is NULL, DELIBERATELY. 000109 classifies each cohort kid/adult, and applyRelocation only
-- writes the band when the stage is classified. Flushing animals are adult females in practice, but
-- the maintainer approved adopting the TAG, not re-classifying kid/adult as a side effect of a move.
-- NULL means a move into a flushing pen records the pen's tag and leaves the animal's existing band
-- exactly as it was, which is the conservative reading of what was agreed.
--
-- Bare 'ICU' and 'Quarantine' are UNAFFECTED and stay blocked. They are clinical STATES: an animal in
-- one has her vaccinations postponed, so a placement action must never assert one. They are now
-- rejected at RAISE time as well (counts/domain.resolveConfiguredStage consults
-- protocol/domain.IsClinicalManagementStage), not only at the second gate, so the operator sees a
-- greyed-out option instead of a movement that dies after the video is already shot.
--
-- Idempotent: re-running (or running after a seed that already lists Flushing) changes nothing.
INSERT INTO public.animal_stage_lookup (tenant_id, stage_code, name, status, age_band)
SELECT t.tenant_id, 'Flushing', 'Flushing', 'active', NULL
FROM (SELECT DISTINCT tenant_id FROM public.animal_stage_lookup) t
WHERE NOT EXISTS (
  SELECT 1 FROM public.animal_stage_lookup existing
  WHERE existing.tenant_id = t.tenant_id
    AND lower(btrim(existing.stage_code)) = 'flushing'
);

-- A tenant that already carries the tag as inactive/retired scaffolding must have it ACTIVE, or the
-- writable-vocabulary check still declines it and the raise silently keeps the current stage.
UPDATE public.animal_stage_lookup
   SET status = 'active', updated_at = now()
 WHERE lower(btrim(stage_code)) = 'flushing'
   AND status <> 'active';

-- +goose Down
-- Returning it to inactive restores keep-current behaviour for new raises. Stages already stamped
-- onto animals are left alone: they record where those animals actually were.
UPDATE public.animal_stage_lookup
   SET status = 'inactive', updated_at = now()
 WHERE lower(btrim(stage_code)) = 'flushing'
   AND status = 'active';
