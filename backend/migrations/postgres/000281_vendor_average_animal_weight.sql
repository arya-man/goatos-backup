-- +goose Up
-- seed-fixture-guard:ignore: additive, nullable vendor-register column; no vaccination/HRMS seed
-- contract change.
--
-- VENDOR AVERAGE ANIMAL WEIGHT (maintainer decision 2026-09-08).
--
-- The selling half of the vendor register -- the agents, butchers, farmers, slaughter houses and
-- companies the farm sells to -- carries no note of WHAT SIZE of animal a buyer wants. The sales
-- desk plans which pens to offer a buyer against exactly that fact ("he takes 35 kg animals"), so a
-- vendor now carries:
--
--   average_animal_weight_kg   the average live weight per animal the buyer expects to take, in kg
--
-- OPTIONAL, and existing rows stay unrecorded (maintainer instruction, same day: "for rows which
-- are already there, you can keep it zero now"). That is stored as NULL, never a literal 0: a
-- buyer who wants zero-kilogram animals is not a fact the farm can hold, and 0 would render as a
-- weight someone recorded. The CHECK only refuses a non-positive value that IS entered.
--
-- One nullable column on the register rather than a buyer-side table: it is the same shape as
-- price_per_goat and capacity_quantity, edited on the same form, and it reads back on every
-- surface the register already reaches (web drawer, phone detail) with no join.
ALTER TABLE public.procurement_vendors
  ADD COLUMN IF NOT EXISTS average_animal_weight_kg numeric(8, 2);

ALTER TABLE public.procurement_vendors
  DROP CONSTRAINT IF EXISTS procurement_vendors_average_animal_weight_check,
  ADD CONSTRAINT procurement_vendors_average_animal_weight_check
    CHECK (average_animal_weight_kg IS NULL OR average_animal_weight_kg > 0);

COMMENT ON COLUMN public.procurement_vendors.average_animal_weight_kg IS
  'Average live weight per animal (kg) this counterparty expects when buying. NULL = not recorded; never 0.';

-- +goose Down
ALTER TABLE public.procurement_vendors
  DROP CONSTRAINT IF EXISTS procurement_vendors_average_animal_weight_check,
  DROP COLUMN IF EXISTS average_animal_weight_kg;
