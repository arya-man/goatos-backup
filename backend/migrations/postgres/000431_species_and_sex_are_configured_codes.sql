-- +goose Up
-- seed-fixture-guard:ignore: loosens goat/sheep and female/male CHECKs to not-blank guards and
-- adds admin-ui revision triggers; every seed still writes goat/sheep/female/male, which the
-- lookups carry as built-ins, so no seed command, fixture or projection changes.
--
-- OPEN UP TO NEW SPECIES (maintainer decision 2026-09-25): a species or gender added on
-- Configuration > Items & settings (species_lookup / sex_lookup, migration 000346) is usable
-- everywhere -- every species/sex dropdown on web and phone is compiled from those lists, and
-- every write path checks the code against the tenant's ACTIVE rows (platform/animalvocab),
-- refusing an unknown or archived one with a farm-worded field error. A new species simply has
-- no vaccination schedule or sale price until one is authored for it.
--
-- 000346 already dropped goats_species_check / goats_sex_check. These are the other CHECKs that
-- still pinned the two species and two sexes, so an animal of a configured third species could be
-- recorded in the register and then refused by a purchase, a sale-price row or a vaccination rule
-- selector. Each becomes a not-blank guard under the same name; membership is the lookup's, checked
-- by the application (no FK: see 000346 on why the lookups are not referenced by constraint).
--
--   animal_purchase_candidates.species / .sex        goat|sheep, male|female  -> not blank
--   growth_sale_price_assumptions.species            goat|sheep               -> not blank
--   growth_sale_price_assumptions.sex                ''|female|male           -> '' or a code shape
--                                                    ('' is the species-default row, not a sex)
--   protocol_rule_dimensions.sex                     female|male|all          -> not blank
--
-- NOT touched: goats (no species/sex CHECK since 000346; the identity write path validates against
-- the lookup) and shed_profiles.sex (a PEN's intended sex incl. 'mixed', written by nothing since
-- the Pens register dropped the column on 2026-09-22).
--
-- Every stored row is already goat/sheep and female/male (or '' / 'all'), so each looser check
-- validates without rewriting anything.
--
-- LOCK SAFETY: none of these tables is on the hot-table list. DROP CONSTRAINT and ADD ... NOT VALID
-- take a brief ACCESS EXCLUSIVE lock with no scan; VALIDATE CONSTRAINT scans under SHARE UPDATE
-- EXCLUSIVE, which does not block writes. lock_timeout bounds every wait.
SET lock_timeout = '5s';

ALTER TABLE public.animal_purchase_candidates DROP CONSTRAINT IF EXISTS animal_purchase_candidates_species_check;
ALTER TABLE public.animal_purchase_candidates
  ADD CONSTRAINT animal_purchase_candidates_species_check CHECK (btrim(species) <> '') NOT VALID;
ALTER TABLE public.animal_purchase_candidates VALIDATE CONSTRAINT animal_purchase_candidates_species_check;

ALTER TABLE public.animal_purchase_candidates DROP CONSTRAINT IF EXISTS animal_purchase_candidates_sex_check;
ALTER TABLE public.animal_purchase_candidates
  ADD CONSTRAINT animal_purchase_candidates_sex_check CHECK (btrim(sex) <> '') NOT VALID;
ALTER TABLE public.animal_purchase_candidates VALIDATE CONSTRAINT animal_purchase_candidates_sex_check;

ALTER TABLE public.growth_sale_price_assumptions DROP CONSTRAINT IF EXISTS growth_sale_price_assumptions_species_check;
ALTER TABLE public.growth_sale_price_assumptions
  ADD CONSTRAINT growth_sale_price_assumptions_species_check CHECK (btrim(species) <> '') NOT VALID;
ALTER TABLE public.growth_sale_price_assumptions VALIDATE CONSTRAINT growth_sale_price_assumptions_species_check;

ALTER TABLE public.growth_sale_price_assumptions DROP CONSTRAINT IF EXISTS growth_sale_price_assumptions_sex_check;
ALTER TABLE public.growth_sale_price_assumptions
  ADD CONSTRAINT growth_sale_price_assumptions_sex_check CHECK (sex = '' OR sex ~ '^[a-z][a-z0-9_]{0,39}$') NOT VALID;
ALTER TABLE public.growth_sale_price_assumptions VALIDATE CONSTRAINT growth_sale_price_assumptions_sex_check;

-- seed-migration-guard:ignore owner=manohark issue=open-up-to-new-species reason=loosens-the-rule-dimension-sex-CHECK-to-not-blank-every-seeded-selector-female-male-all-still-passes expiry=2026-12-31
ALTER TABLE public.protocol_rule_dimensions DROP CONSTRAINT IF EXISTS protocol_rule_dimensions_sex_check;
-- seed-migration-guard:ignore owner=manohark issue=open-up-to-new-species reason=loosens-the-rule-dimension-sex-CHECK-to-not-blank-every-seeded-selector-female-male-all-still-passes expiry=2026-12-31
ALTER TABLE public.protocol_rule_dimensions
  ADD CONSTRAINT protocol_rule_dimensions_sex_check CHECK (btrim(sex) <> '') NOT VALID;
-- seed-migration-guard:ignore owner=manohark issue=open-up-to-new-species reason=loosens-the-rule-dimension-sex-CHECK-to-not-blank-every-seeded-selector-female-male-all-still-passes expiry=2026-12-31
ALTER TABLE public.protocol_rule_dimensions VALIDATE CONSTRAINT protocol_rule_dimensions_sex_check;

-- The admin-web bootstrap compiles its species / sex pickers from these lookups, so an edit on
-- Configuration must refresh the contract at once (the same trigger every other compiled family
-- carries), not wait for an unrelated revision bump.
CREATE TRIGGER admin_ui_species_lookup_revision_trg
  AFTER INSERT OR DELETE OR UPDATE ON public.species_lookup
  FOR EACH ROW EXECUTE FUNCTION public.admin_ui_bump_row_family_trg('species');
CREATE TRIGGER admin_ui_sex_lookup_revision_trg
  AFTER INSERT OR DELETE OR UPDATE ON public.sex_lookup
  FOR EACH ROW EXECUTE FUNCTION public.admin_ui_bump_row_family_trg('sexes');

SELECT public.admin_ui_bump_config_family(
  tenant_id,
  'species',
  NULL,
  'migration:000431_species_and_sex_are_configured_codes',
  '{"reason":"species and sex pickers compiled from Configuration"}'::jsonb
)
FROM (SELECT DISTINCT tenant_id FROM public.species_lookup) AS tenants;

-- +goose Down
SET lock_timeout = '5s';

DROP TRIGGER IF EXISTS admin_ui_sex_lookup_revision_trg ON public.sex_lookup;
DROP TRIGGER IF EXISTS admin_ui_species_lookup_revision_trg ON public.species_lookup;

-- seed-migration-guard:ignore owner=manohark issue=open-up-to-new-species reason=loosens-the-rule-dimension-sex-CHECK-to-not-blank-every-seeded-selector-female-male-all-still-passes expiry=2026-12-31
ALTER TABLE public.protocol_rule_dimensions DROP CONSTRAINT IF EXISTS protocol_rule_dimensions_sex_check;
-- seed-migration-guard:ignore owner=manohark issue=open-up-to-new-species reason=loosens-the-rule-dimension-sex-CHECK-to-not-blank-every-seeded-selector-female-male-all-still-passes expiry=2026-12-31
ALTER TABLE public.protocol_rule_dimensions
  ADD CONSTRAINT protocol_rule_dimensions_sex_check CHECK (sex = ANY (ARRAY['female'::text, 'male'::text, 'all'::text])) NOT VALID;

ALTER TABLE public.growth_sale_price_assumptions DROP CONSTRAINT IF EXISTS growth_sale_price_assumptions_sex_check;
ALTER TABLE public.growth_sale_price_assumptions
  ADD CONSTRAINT growth_sale_price_assumptions_sex_check CHECK (sex IN ('', 'female', 'male')) NOT VALID;

ALTER TABLE public.growth_sale_price_assumptions DROP CONSTRAINT IF EXISTS growth_sale_price_assumptions_species_check;
ALTER TABLE public.growth_sale_price_assumptions
  ADD CONSTRAINT growth_sale_price_assumptions_species_check CHECK (species IN ('goat', 'sheep')) NOT VALID;

ALTER TABLE public.animal_purchase_candidates DROP CONSTRAINT IF EXISTS animal_purchase_candidates_sex_check;
ALTER TABLE public.animal_purchase_candidates
  ADD CONSTRAINT animal_purchase_candidates_sex_check CHECK (sex IN ('male', 'female')) NOT VALID;

ALTER TABLE public.animal_purchase_candidates DROP CONSTRAINT IF EXISTS animal_purchase_candidates_species_check;
ALTER TABLE public.animal_purchase_candidates
  ADD CONSTRAINT animal_purchase_candidates_species_check CHECK (species IN ('goat', 'sheep')) NOT VALID;
