-- +goose Up
-- One antihistamine, one spelling.
--
-- Maintainer decision 2026-09-21: the correct spelling is "Chlorpheniramine maleate".
--
-- WHY THIS EXISTS. The farm's treatment protocols carried the same medicine under two
-- spellings -- "Chloropheniramine maleate" on 20 steps and "Chlorpheniramine maleate" on
-- 4 -- and as free text nobody could see it. Moving medicines into the item registry
-- (000384) made it visible as two rows in Items & categories, which is exactly what the
-- registry is for.
--
-- THE SOURCE IS FIXED TOO. context/source-findings/health-sop-v1.json now spells it one
-- way, so a fresh seed produces one medicine. This migration repairs the databases that
-- already ran the old snapshot. NOTE FOR WHOEVER RE-CAPTURES THE SHEET: the Google Sheet
-- this snapshot came from still has the old spelling, and a re-capture without fixing it
-- there will reintroduce both rows.
--
-- WHAT IT DELIBERATELY DOES NOT DO: guess. This is a SPELLING merge, named explicitly,
-- for a medicine a human confirmed is one medicine. It is not a fuzzy de-duplication pass
-- over the catalog -- "Meloxicam" and "Meloxicam Paracetamol" may or may not be one
-- medicine, and merging two dosage lines on a similarity score is a clinical decision a
-- migration has no business making.

BEGIN;

-- 1. Every step moves to the correct spelling, and to the correct item.
--
-- The name is rewritten as well as the link because the NAME is what an operator reads off
-- the phone mid-treatment; leaving it while repointing the id would show the old spelling
-- against the right medicine, which is the confusion this migration exists to end.
UPDATE public.health_protocol_steps s
SET medicine_name    = keep.name,
    medicine_item_id = keep.item_id
FROM public.inventory_items keep
WHERE keep.tenant_id = s.tenant_id
  AND lower(keep.name) = 'chlorpheniramine maleate'
  AND lower(btrim(s.medicine_name)) = 'chloropheniramine maleate';

-- 2. Anything else that pointed at the wrong item now points at the right one, so the
--    delete below cannot orphan a reference this migration did not think of.
UPDATE public.health_protocol_steps s
SET medicine_item_id = keep.item_id
FROM public.inventory_items keep, public.inventory_items drop_it
WHERE keep.tenant_id = s.tenant_id
  AND drop_it.tenant_id = s.tenant_id
  AND lower(keep.name) = 'chlorpheniramine maleate'
  AND lower(drop_it.name) = 'chloropheniramine maleate'
  AND s.medicine_item_id = drop_it.item_id;

-- 3. The misspelled catalog row goes.
--
-- DELETE rather than archive: an archived row stays in the registry as a second entry for
-- one medicine, which is the thing being fixed. ON DELETE RESTRICT on the step foreign key
-- is the safety net -- if anything still references it, this migration fails loudly rather
-- than leaving a dangling link.
DELETE FROM public.inventory_items i
WHERE lower(i.name) = 'chloropheniramine maleate'
  AND i.category = 'medicine'
  AND EXISTS (
    SELECT 1 FROM public.inventory_items keep
    WHERE keep.tenant_id = i.tenant_id
      AND lower(keep.name) = 'chlorpheniramine maleate'
  );

COMMIT;

-- +goose Down
-- Deliberately empty of data changes.
--
-- Down would have to decide which of the 24 steps were originally misspelled, and that is
-- information this migration destroyed on purpose. Re-running the corrected seed is the way
-- back, not an inverse UPDATE that would reintroduce a typo on rows chosen by guesswork.
SELECT 1;
