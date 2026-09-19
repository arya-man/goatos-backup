-- +goose Up
-- seed-fixture-guard:ignore: one configuration row per tenant for the Sales valuation; no
-- vaccination/HRMS seed contract, source fixture schema, or read-model change.
--
-- FARM VALUATION ASSUMPTIONS ARE DATA (maintainer instruction 2026-09-19, docs/decisions/
-- sales-valuation-assumptions.md). The Farm value page priced the live herd from a VALUES table
-- hard-coded inside the overview SQL (fattening at the measured weight x 450, adult females
-- 40 kg x 600, bucks 60 kg x 500, K0/K1 3 kg, K2 8 kg, K3 15 kg x 500), drew its "Over 35 kg"
-- line from a Go constant, and Load wise valued every unsold animal at the overall average sold
-- price. Every one of those is a figure someone DECIDED rather than measured, so each is now a
-- row edited on Sales Config and re-read per request.
--
-- ONE ROW PER TENANT, the buckets as a jsonb list (the bucket KEYS are the classification the
-- valuation SQL files animals into and are fixed; the label, fixed weight and rupees per kg of
-- each are edited). A NULL fixed weight means "price at the measured weight" (the fattening
-- bucket's rule). unsold_stock_price_rupees NULL keeps Load wise on the overall average sold
-- price; a figure replaces that fallback for every unsold animal.
CREATE TABLE IF NOT EXISTS public.sales_valuation_assumptions (
  tenant_id                 uuid PRIMARY KEY REFERENCES public.tenants (tenant_id),
  buckets                   jsonb NOT NULL,
  sale_ready_kg             numeric(6,2) NOT NULL DEFAULT 35,
  unsold_stock_price_rupees numeric(12,2),
  row_version               integer NOT NULL DEFAULT 1,
  updated_at                timestamptz NOT NULL DEFAULT now(),
  updated_by                uuid,
  CONSTRAINT sales_valuation_assumptions_buckets_check CHECK (jsonb_typeof(buckets) = 'array'),
  CONSTRAINT sales_valuation_assumptions_sale_ready_check CHECK (sale_ready_kg > 0 AND sale_ready_kg <= 200),
  CONSTRAINT sales_valuation_assumptions_unsold_check CHECK (unsold_stock_price_rupees IS NULL OR unsold_stock_price_rupees > 0)
);

-- Seed = exactly the figures the SQL carried, so no farm's valuation moves on deploy.
INSERT INTO public.sales_valuation_assumptions (tenant_id, buckets, sale_ready_kg, unsold_stock_price_rupees)
SELECT t.tenant_id, $seed$[
  {"bucket": "fattening", "label": "Fattening animals", "fixed_weight_kg": null, "price_per_kg": 450, "display_order": 1},
  {"bucket": "adult_female", "label": "Adult females", "fixed_weight_kg": 40, "price_per_kg": 600, "display_order": 2},
  {"bucket": "adult_male_buck", "label": "Adult males / bucks", "fixed_weight_kg": 60, "price_per_kg": 500, "display_order": 3},
  {"bucket": "K0", "label": "K0", "fixed_weight_kg": 3, "price_per_kg": 500, "display_order": 4},
  {"bucket": "K1", "label": "K1", "fixed_weight_kg": 3, "price_per_kg": 500, "display_order": 5},
  {"bucket": "K2", "label": "K2", "fixed_weight_kg": 8, "price_per_kg": 500, "display_order": 6},
  {"bucket": "K3", "label": "K3", "fixed_weight_kg": 15, "price_per_kg": 500, "display_order": 7}
]$seed$::jsonb, 35, NULL
FROM public.tenants t
ON CONFLICT (tenant_id) DO NOTHING;

-- +goose Down
DROP TABLE IF EXISTS public.sales_valuation_assumptions;
