-- +goose Up
-- A shifting into an ICU / quarantine KID pen now stamps that pen's tag (maintainer decision
-- 2026-08-15), and the animal's milk band is preserved on the way in.
--
-- Nothing in Go blocked this. resolveDestinationTag rejects a tag naming a clinical STATE --
-- MandatoryClinicalDeferStates: sick, under_treatment, recovering, quarantine, icu -- and
-- 'ICU-Kid' is not one of those: clinicalStageKey('ICU-Kid') = 'icu-kid', which matches no entry.
-- What actually held these tags back is this table. ResolveShiftingDestinationPenStage only
-- returns a stage that is in the WRITABLE vocabulary (an active animal_stage_lookup row), so an
-- unlisted pen tag resolved to "" = keep current stage. Listing them here is therefore the whole
-- change on the raise side.
--
-- The narrowing that matters: this adds the two KID tags ONLY. Bare 'ICU' and bare 'Quarantine'
-- stay unlisted AND stay rejected by resolveDestinationTag, so a movement still cannot invent a
-- clinical STATE for an animal -- that remains the owning clinical flow's decision. What a
-- movement may now say is which PEN the animal is in, including a clinical one.
--
-- age_band stays NULL, exactly as 000109 requires for clinical stages: applyRelocation guards the
-- band write on it being classified, so a move into one of these pens leaves the animal's existing
-- kid/adult band untouched rather than blanking or flipping it.
--
-- Idempotent: re-running (or running after a seed that already listed these) changes nothing.
INSERT INTO public.animal_stage_lookup (tenant_id, stage_code, name, status, age_band)
SELECT t.tenant_id, v.stage_code, v.name, 'active', NULL
FROM (SELECT DISTINCT tenant_id FROM public.animal_stage_lookup) t
CROSS JOIN (VALUES
  ('ICU-Kid', 'ICU-Kid'),
  ('Quarantine kids', 'Quarantine kids')
) AS v(stage_code, name)
WHERE NOT EXISTS (
  SELECT 1 FROM public.animal_stage_lookup existing
  WHERE existing.tenant_id = t.tenant_id
    AND lower(btrim(existing.stage_code)) = lower(btrim(v.stage_code))
);

-- A tenant that already carries the tag as inactive/retired scaffolding must have it ACTIVE, or the
-- writable-vocabulary check still declines it and the raise silently keeps the current stage.
UPDATE public.animal_stage_lookup
   SET status = 'active', updated_at = now()
 WHERE lower(btrim(stage_code)) IN ('icu-kid', 'icu- kid', 'quarantine kids', 'quarantine kid')
   AND status <> 'active';

-- +goose Down
-- Returning these to inactive restores keep-current behaviour for new raises. Stages already
-- stamped onto animals are left alone: they record where those animals actually were.
UPDATE public.animal_stage_lookup
   SET status = 'inactive', updated_at = now()
 WHERE lower(btrim(stage_code)) IN ('icu-kid', 'quarantine kids')
   AND status = 'active';
