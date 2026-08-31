-- +goose Up

-- PROCUREMENT LOAD COSTS: what the farm paid for an animal load.
--
-- Maintainer decision 2026-08-31 (docs/decisions/sales-loadwise.md): the Sales page reports
-- load-wise economics -- purchased vs sold vs mortality counts and purchase value vs sold value per
-- procurement load. The loads table carried no money at all (the only price in the schema was the
-- vendor's indicative price_per_goat), so the purchase side of that report had nothing to read.
-- These columns are the recorded landed cost of one load, entered on the Sales page's load-wise
-- section by the procurement desk (dedicated permission procurement.load_cost.write, the same
-- read/write split feed_purchases uses for the same reason: operators work the source-entry
-- screens, and this is supplier money).
--
-- NULL animal_cost means COST NOT RECORDED, and the read model reports it exactly that way -- the
-- purchase-value bar for such a load renders "cost not recorded", never a fabricated zero.
-- transport_cost and other_cost are detail of a recorded cost, so they cannot exist without
-- animal_cost (enforced below). The landed purchase value of a load is
-- animal_cost + COALESCE(transport_cost, 0) + COALESCE(other_cost, 0), derived at read time --
-- deliberately NOT stored, so the parts cannot drift from their sum.
ALTER TABLE public.procurement_loads
    ADD COLUMN animal_cost numeric(14, 2),
    ADD COLUMN transport_cost numeric(14, 2),
    ADD COLUMN other_cost numeric(14, 2),
    ADD COLUMN cost_recorded_by uuid,
    ADD COLUMN cost_recorded_at timestamp with time zone;

ALTER TABLE public.procurement_loads
    ADD CONSTRAINT procurement_loads_animal_cost_nonneg
        CHECK (animal_cost IS NULL OR animal_cost >= 0),
    ADD CONSTRAINT procurement_loads_transport_cost_nonneg
        CHECK (transport_cost IS NULL OR transport_cost >= 0),
    ADD CONSTRAINT procurement_loads_other_cost_nonneg
        CHECK (other_cost IS NULL OR other_cost >= 0),
    -- Cost detail without the main figure is a half-recorded cost the report could neither show
    -- nor honestly call missing; refuse it at the schema so no write path can produce it.
    ADD CONSTRAINT procurement_loads_cost_requires_animal_cost
        CHECK (animal_cost IS NOT NULL OR (transport_cost IS NULL AND other_cost IS NULL));

-- +goose Down

ALTER TABLE public.procurement_loads
    DROP CONSTRAINT IF EXISTS procurement_loads_cost_requires_animal_cost,
    DROP CONSTRAINT IF EXISTS procurement_loads_other_cost_nonneg,
    DROP CONSTRAINT IF EXISTS procurement_loads_transport_cost_nonneg,
    DROP CONSTRAINT IF EXISTS procurement_loads_animal_cost_nonneg,
    DROP COLUMN IF EXISTS cost_recorded_at,
    DROP COLUMN IF EXISTS cost_recorded_by,
    DROP COLUMN IF EXISTS other_cost,
    DROP COLUMN IF EXISTS transport_cost,
    DROP COLUMN IF EXISTS animal_cost;
