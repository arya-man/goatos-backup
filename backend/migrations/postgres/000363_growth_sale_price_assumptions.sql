-- +goose Up
-- seed-fixture-guard:ignore: additive tenant-scoped pricing assumption for the Weighing FCR and
-- Comparison tabs; no vaccination/HRMS seed contract change and no app-visible projection table.
--
-- ASSUMED LIVE-WEIGHT SALE PRICE (maintainer decision 2026-09-07).
--
-- The Weighing FCR tab values every kilogram a pen gains at an assumed sale price so feed spent
-- can be read against value gained. The maintainer's instruction: "take avg price of 425 per kg
-- ... and price don't fix it like keep it somewhere in db we need to be able to change it ...
-- try to keep for sheep and goat different".
--
-- So the price is DATA, not a constant and not page copy. One row per (tenant, species,
-- effective_from); rows are APPENDED, never edited in place, so a past window is valued at the
-- price that applied then and the tab can say who set the price and when. The maintainer edits
-- the rows directly for now (no UI write control was asked for); the read picks the newest row
-- whose effective_from is on or before the period being reported.
--
-- Both species seed at 425 because that is the one figure the maintainer named. The Comparison
-- (Load-wise) tab previously carried 430 sheep / 450 goat as hard-coded page copy; it now reads
-- these rows too, so the Weighing area has exactly one sale price.
CREATE TABLE public.growth_sale_price_assumptions (
  assumption_id     uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id         uuid NOT NULL REFERENCES tenants (tenant_id),
  species           text NOT NULL CHECK (species IN ('goat', 'sheep')),
  price_per_kg_inr  numeric(10, 2) NOT NULL CHECK (price_per_kg_inr > 0),
  effective_from    date NOT NULL,
  set_by            text NOT NULL DEFAULT '',
  note              text NOT NULL DEFAULT '',
  created_at        timestamptz NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, species, effective_from)
);

COMMENT ON TABLE public.growth_sale_price_assumptions IS
  'Assumed live-weight sale price per species, effective-dated and append-only. Read by the Weighing FCR and Comparison tabs to value weight gained and stock on hand. Maintainer-edited data, never a code constant.';

INSERT INTO public.growth_sale_price_assumptions (tenant_id, species, price_per_kg_inr, effective_from, set_by, note)
SELECT t.tenant_id, s.species, 425.00, DATE '2026-09-07', 'maintainer', 'Initial assumption (maintainer, 2026-09-07): ₹425 per kg live weight for both species.'
FROM tenants t
CROSS JOIN (VALUES ('goat'), ('sheep')) AS s(species)
ON CONFLICT (tenant_id, species, effective_from) DO NOTHING;

-- +goose Down
DROP TABLE IF EXISTS public.growth_sale_price_assumptions;
