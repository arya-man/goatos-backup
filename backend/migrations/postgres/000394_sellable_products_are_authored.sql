-- +goose Up
--
-- WHAT THE FARM SELLS IS AUTHORED ON SALES CONFIG (maintainer instruction 2026-09-23, the second
-- half of 000393).
--
-- 000393 made the product vocabulary DATA; this makes it EDITABLE, where the maintainer asked for
-- it: "in future i will sell tags of sheep also so it should be configurable in sales config --
-- i will add item and how i will sell, whether in kg's or item numbers". So adding sheep tags is
-- a row somebody types on /sales/config, and it is in the record-sale dropdown the same minute.
--
-- ONE UNIT VOCABULARY, NOT TWO. The item form asks a plain question -- sold by the KILOGRAM or by
-- NUMBER -- so the stored vocabulary is exactly those two answers. The two built-in animal rows
-- were seeded 'head', which is the same fact in different words, and a person editing the list
-- should not have to learn that 'head' and 'number' are one thing. They are migrated, and nothing
-- behaves differently: an animal line does not read the unit at all (it carries a head count and a
-- negotiated lump value, maintainer decision 2026-09-23), so this moves a label and no arithmetic.
UPDATE public.sellable_product_catalog SET unit = 'number' WHERE unit = 'head';

ALTER TABLE public.sellable_product_catalog DROP CONSTRAINT IF EXISTS sellable_product_catalog_unit_check;
ALTER TABLE public.sellable_product_catalog
    ADD CONSTRAINT sellable_product_catalog_unit_check CHECK (unit IN ('kg', 'number'));

-- Who last touched the row, for a list a person maintains by hand.
ALTER TABLE public.sellable_product_catalog
    ADD COLUMN IF NOT EXISTS updated_by uuid;

COMMENT ON COLUMN public.sellable_product_catalog.unit IS
  'How one of it is sold: by the kilogram, or by number. Read only for a line priced per unit -- an animal line carries a head count and a negotiated lump value and never reads this.';

-- +goose Down

ALTER TABLE public.sellable_product_catalog DROP COLUMN IF EXISTS updated_by;
ALTER TABLE public.sellable_product_catalog DROP CONSTRAINT IF EXISTS sellable_product_catalog_unit_check;
-- The built-ins go back to the word they were seeded with. A product the farm added itself keeps
-- the unit it was authored under: it records what somebody chose, and there is no older word for it.
UPDATE public.sellable_product_catalog SET unit = 'head' WHERE unit = 'number' AND is_builtin;
