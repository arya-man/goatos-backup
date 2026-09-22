-- +goose Up
-- A treatment step names a medicine FROM THE CATALOG, never free text.
-- Canonical contract: docs/decisions/health-diagnosis-register-authoring.md.
--
-- Maintainer instruction 2026-09-21: nothing is assigned at random. A medicine, a pair of
-- gloves, any equipment -- it exists on /configuration/items first, and only then can a
-- protocol name it. Authoring by typing a name means two spellings of one medicine, a
-- medicine nobody stocks, and a dosage attached to something the store has never heard of.
--
-- WHAT WAS ACTUALLY THERE, and why this migration has a backfill rather than a constraint
-- and a shrug: `health_protocol_steps.medicine_name` is free text and carries 35 DISTINCT
-- names across 465 steps, while `inventory_items` held SEVEN rows, all vaccines, and not a
-- single medicine. So the catalog could not have supplied those names; the protocols are
-- where the farm's real medicine list lives, and this migration moves it to where it
-- belongs before making the link compulsory.
--
-- The 35 are imported UNDER THE BUILT-IN Medicines ROOT with their names exactly as the
-- protocols spell them. They are not de-duplicated by fuzzy match: "Meloxicam" and
-- "Meloxicam Paracetamol" may or may not be one medicine, and merging two dosage lines on a
-- guess is a clinical decision this migration has no business making. A vet can merge them
-- on screen afterwards; nothing here silently loses one.

BEGIN;

-- 1. Every medicine the protocols already use becomes a catalog item.
--
-- item_code is derived from the name so a re-run is a no-op and so the code reads as the
-- thing it names. `ON CONFLICT DO NOTHING` covers both unique indexes: a tenant that
-- already stocks one of these keeps its own row, and this import never overwrites it.
INSERT INTO public.inventory_items (tenant_id, item_code, name, category, base_unit, status, category_id)
SELECT DISTINCT ON (s.tenant_id, lower(btrim(s.medicine_name)))
       s.tenant_id,
       'med_' || regexp_replace(lower(btrim(s.medicine_name)), '[^a-z0-9]+', '_', 'g'),
       btrim(s.medicine_name),
       'medicine',
       'unit',
       'active',
       c.category_id
FROM public.health_protocol_steps s
JOIN public.item_categories c
  ON c.tenant_id = s.tenant_id
 AND c.parent_category_id IS NULL
 AND c.item_kind = 'medicine'
WHERE coalesce(btrim(s.medicine_name), '') <> ''
  AND NOT EXISTS (
    SELECT 1 FROM public.inventory_items i
    WHERE i.tenant_id = s.tenant_id
      AND lower(i.name) = lower(btrim(s.medicine_name))
  )
ORDER BY s.tenant_id, lower(btrim(s.medicine_name)), s.medicine_name
ON CONFLICT DO NOTHING;

-- 2. The step points AT the item it names.
--
-- Nullable, and deliberately so: an ACTION step has no medicine, and a medication step
-- authored before this migration is linked by the backfill below. The compulsory-ness lives
-- in the authoring validation, not in the column, because the column is also written by the
-- sheet importer whose rows this migration has just imported -- a NOT NULL here would make
-- the import that creates the items depend on the items already existing.
ALTER TABLE public.health_protocol_steps
  ADD COLUMN IF NOT EXISTS medicine_item_id uuid;

COMMENT ON COLUMN public.health_protocol_steps.medicine_item_id IS
  'The catalog item this step administers (inventory_items). NULL on an action or critical-action step, which has no medicine. medicine_name is kept as the label an operator reads on the phone, so a catalog rename does not silently rewrite a course a goat is mid-way through.';

UPDATE public.health_protocol_steps s
SET medicine_item_id = i.item_id
FROM public.inventory_items i
WHERE i.tenant_id = s.tenant_id
  AND lower(i.name) = lower(btrim(s.medicine_name))
  AND coalesce(btrim(s.medicine_name), '') <> ''
  AND s.medicine_item_id IS NULL;

ALTER TABLE public.health_protocol_steps
  ADD CONSTRAINT health_protocol_steps_medicine_item_fk
    FOREIGN KEY (tenant_id, medicine_item_id)
    REFERENCES public.inventory_items (tenant_id, item_id) ON DELETE RESTRICT;

-- The authoring picker's read: the active medicines of one tenant, by name.
CREATE INDEX IF NOT EXISTS inventory_items_kind_active_name_idx
  ON public.inventory_items (tenant_id, category, lower(name))
  WHERE status = 'active';

COMMIT;

-- +goose Down
BEGIN;
DROP INDEX IF EXISTS public.inventory_items_kind_active_name_idx;
ALTER TABLE public.health_protocol_steps
  DROP CONSTRAINT IF EXISTS health_protocol_steps_medicine_item_fk;
ALTER TABLE public.health_protocol_steps DROP COLUMN IF EXISTS medicine_item_id;
-- The imported items are LEFT IN PLACE. They are the farm's medicine list, they are now
-- editable on /configuration/items, and other rows may already reference them; deleting
-- them to undo a column would throw away real reference data.
COMMIT;
