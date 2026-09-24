-- +goose Up
-- seed-fixture-guard:ignore: widens the tenant-scoped pricing assumption (000363) with an optional
-- stage x sex override; no vaccination/HRMS seed contract change and no app-visible projection table.
--
-- SALE PRICE PER STAGE AND SEX (maintainer decision 2026-09-24).
--
-- 000363 priced live weight per SPECIES only. The maintainer asked for the price to be set per
-- management stage and sex of the animal, keeping goat and sheep apart. Three decisions:
--
--   1. A row is keyed (species, management_stage, sex). The existing per-species row -- stage and
--      sex both '' -- stays, and is the SPECIES DEFAULT.
--   2. An animal whose (species, stage, sex) has no price of its own is valued at its species
--      default, so nothing that was valued yesterday goes blank the day this ships.
--   3. Both readers (FCR tab: weight gained; Load-wise tab: stock on hand) value each animal at
--      its own combination, head-weighted across a pen or a load.
--
-- Still APPEND-ONLY and effective-dated. Taking an override away is itself a row: an override row
-- with NO price means "from this date, this combination uses the species default again", so the
-- history of what was in force on any past day stays readable. A species default can never be
-- cleared -- it is the floor every animal falls back to.
ALTER TABLE public.growth_sale_price_assumptions
  ADD COLUMN management_stage text NOT NULL DEFAULT '',
  ADD COLUMN sex              text NOT NULL DEFAULT '';

-- The 000363 unique key and price check carry Postgres-generated names, and the unique key's name
-- is past the 63-byte identifier limit, so it is looked up rather than guessed.
-- +goose StatementBegin
DO $$
DECLARE c record;
BEGIN
  FOR c IN
    SELECT conname FROM pg_constraint
    WHERE conrelid = 'public.growth_sale_price_assumptions'::regclass AND contype IN ('u', 'c')
  LOOP
    EXECUTE format('ALTER TABLE public.growth_sale_price_assumptions DROP CONSTRAINT %I', c.conname);
  END LOOP;
END $$;
-- +goose StatementEnd

ALTER TABLE public.growth_sale_price_assumptions
  ALTER COLUMN price_per_kg_inr DROP NOT NULL,
  ADD CONSTRAINT growth_sale_price_assumptions_species_check CHECK (species IN ('goat', 'sheep')),
  ADD CONSTRAINT growth_sale_price_assumptions_sex_check CHECK (sex IN ('', 'female', 'male')),
  -- An override names BOTH a stage and a sex; the default names neither.
  ADD CONSTRAINT growth_sale_price_assumptions_override_shape_check CHECK ((management_stage = '') = (sex = '')),
  -- A price is positive; only an override may carry none (the "cleared" row).
  ADD CONSTRAINT growth_sale_price_assumptions_price_check CHECK (
    (price_per_kg_inr IS NOT NULL AND price_per_kg_inr > 0)
    OR (price_per_kg_inr IS NULL AND management_stage <> '')
  ),
  ADD CONSTRAINT growth_sale_price_assumptions_key UNIQUE (tenant_id, species, management_stage, sex, effective_from);

COMMENT ON TABLE public.growth_sale_price_assumptions IS
  'Assumed live-weight sale price per species, optionally overridden per (management_stage, sex); effective-dated and append-only. stage and sex both empty = the species default. An override row with a NULL price clears that override from its date. Read by the Weighing FCR and Load-wise tabs.';

-- +goose Down
DELETE FROM public.growth_sale_price_assumptions WHERE management_stage <> '';
ALTER TABLE public.growth_sale_price_assumptions
  DROP CONSTRAINT IF EXISTS growth_sale_price_assumptions_key,
  DROP CONSTRAINT IF EXISTS growth_sale_price_assumptions_price_check,
  DROP CONSTRAINT IF EXISTS growth_sale_price_assumptions_override_shape_check,
  DROP CONSTRAINT IF EXISTS growth_sale_price_assumptions_sex_check;
ALTER TABLE public.growth_sale_price_assumptions
  ALTER COLUMN price_per_kg_inr SET NOT NULL,
  ADD CONSTRAINT growth_sale_price_assumptions_price_per_kg_inr_check CHECK (price_per_kg_inr > 0),
  ADD CONSTRAINT growth_sale_price_assumptions_species_effective_key UNIQUE (tenant_id, species, effective_from);
ALTER TABLE public.growth_sale_price_assumptions
  DROP COLUMN sex,
  DROP COLUMN management_stage;
