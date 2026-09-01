-- +goose Up
-- stg-zero-downtime: compatible because it adds optional procurement detail columns and a child table; new constraints allow NULL for existing rows, and old services ignore the added schema.

-- PROCUREMENT LOAD COST LINES: what the three cost buckets on a load are MADE OF.
--
-- Maintainer decision 2026-09-01. The Sales "Purchase value" was reading as the ex-farm animal
-- price alone. The FORMULA was never wrong -- procurement/domain.loadPurchaseValue has always been
-- animal + transport + other -- but only the animal figure was ever recorded, because the source
-- (the farm's Procurement DB sheet) carries cost as ONE ROW PER EVENT PER LOAD and the import took
-- the Purchase row only. A load's real landed cost also carries Transport, Booking, Labour,
-- Transit and Transition Feed rows. Across the sheet that is Rs 12.1L of transport, Rs 5.0L of
-- booking, Rs 1.3L of labour, Rs 0.8L of transit and Rs 0.35L of transition feed sitting outside
-- the number the farm was shown; on the eight live loads it understated purchase value by
-- Rs 2.99L (5.8%).
--
-- The maintainer's shape decision, in their words: "keep table like that only on clicking show
-- detailed data". So the LIST stays three columns -- animal / transport / other -- and the
-- itemisation appears when a load is opened. This table is that itemisation.
--
-- WHY LINES ARE THE SOURCE AND THE COLUMNS ARE A ROLL-UP. Two writable places for one number is
-- how the two come to disagree, so there is exactly one rule: procurement_loads.animal_cost /
-- transport_cost / other_cost are MAINTAINED FROM THESE LINES in the same transaction that writes
-- them (procurement/adapters/postgres.rollUpLoadCostLines). A load with no lines keeps whatever
-- its columns already say -- that is every load costed by hand before today, and they must not
-- silently become uncosted. A manual bucket edit through the cost drawer REPLACES that bucket's
-- lines with a single line of the bucket's own kind, so the detail can never claim a breakdown
-- that does not add up to the figure beside it.
CREATE TABLE public.procurement_load_cost_lines (
    tenant_id uuid NOT NULL,
    load_id uuid NOT NULL,
    line_id uuid NOT NULL DEFAULT gen_random_uuid(),
    -- The farm's own vocabulary, taken from the sheet's Record Type column so an imported line and
    -- a hand-entered one are the same kind of thing. 'animal' is the sheet's "Purchase" row,
    -- renamed here to match the column it rolls into; the rest keep the sheet's words.
    kind text NOT NULL,
    amount numeric(14, 2) NOT NULL,
    -- Free text from the source row (the sheet's Problems column, a payment note). Never composed
    -- for display -- the client renders the KIND through backend-owned copy, not this.
    note text NOT NULL DEFAULT '',
    -- Where this line came from, so an imported figure is never mistaken for one a person typed.
    source text NOT NULL DEFAULT 'app',
    recorded_by uuid,
    recorded_at timestamp with time zone NOT NULL DEFAULT now(),
    CONSTRAINT procurement_load_cost_lines_pkey PRIMARY KEY (tenant_id, line_id),
    CONSTRAINT procurement_load_cost_lines_load_fkey
        FOREIGN KEY (tenant_id, load_id) REFERENCES public.procurement_loads (tenant_id, load_id) ON DELETE CASCADE,
    -- A cost line is money that was actually spent. Zero is allowed (the sheet carries recorded
    -- zero-value Unloaded/Vaccination rows and dropping them would lose the fact that the step
    -- happened at no charge); negative is not -- a refund is a business event nobody has defined.
    CONSTRAINT procurement_load_cost_lines_amount_nonneg CHECK (amount >= 0),
    CONSTRAINT procurement_load_cost_lines_kind_known CHECK (
        kind IN ('animal', 'transport', 'booking', 'labour', 'transit', 'transition_feed', 'other')
    ),
    CONSTRAINT procurement_load_cost_lines_source_known CHECK (source IN ('app', 'sheet_import'))
);

-- PURCHASE WEIGHT: the load's total LIVE weight at purchase, in kg.
--
-- Maintainer request 2026-09-01, same conversation: the farm reads a load's economics as
-- "landing price per live kg", not as a lump sum -- it is the number that compares one vendor's
-- deal against another's when the animals differ in size. That figure is
-- landed cost / live kg, so the cost half (above) is only half the answer; this is the other half.
-- The source sheet has carried it as the Purchase row's "Total Weight(Kg)" all along, beside a
-- hand-maintained "Per kg cost" column that -- checked against all eight live loads -- is exactly
-- landed cost divided by this weight. That agreement is what pins the definition: the farm was
-- already computing landing price per live kg by hand, and GoatOS now derives the same number.
--
-- NULL means NOT WEIGHED, and the read model reports it that way. It is deliberately not
-- defaulted to zero: a zero would make the per-kg figure a division by zero, and treating an
-- unweighed load as weightless would print an infinite price per kg.
ALTER TABLE public.procurement_loads
    ADD COLUMN purchase_weight_kg numeric(12, 2);

ALTER TABLE public.procurement_loads
    ADD CONSTRAINT procurement_loads_purchase_weight_positive
        -- Zero is refused rather than allowed-and-guarded: a load that weighed nothing is not a
        -- fact the farm can record, and permitting it would push a divide-by-zero decision onto
        -- every reader of the column.
        CHECK (purchase_weight_kg IS NULL OR purchase_weight_kg > 0);

-- THE SALE SIDE AND THE FATTENING CLOCK (maintainer request 2026-09-01, same conversation).
--
-- Purchase & barn compares each load's buying against its selling, and the farm reads that
-- comparison per ANIMAL and per KILOGRAM, not as lump sums: how heavy an animal came in, how heavy
-- it went out, what a kilogram cost to land, what a kilogram fetched, and how many days of
-- fattening sat between. The purchase half is above; these are the sale half and the clock.
--
-- WHY THESE ARE IMPORTED FACTS RATHER THAN DERIVED. GoatOS cannot compute them: the legacy sales
-- were never tagged to their animals (goat_sale_allocations resolves NONE of these loads), so no
-- join reaches from a load to the deals that emptied it. They come from the legacy salesDB, and
-- they are stored as what they are -- a measured sample, not a total.
--
-- THE SAMPLE IS THE POINT, and sold_weighed_animals is what makes it honest. Some legacy sales
-- recorded no weight at all: load 101 sold 67 animals but only 26 of them were weighed, so
-- dividing its weight by 67 would report a 13 kg sale animal that never existed. The denominator
-- must range over the SAME rows as the numerator -- so the count of animals that actually carry a
-- weight is stored beside the weight, and the average is weight / weighed animals.
-- sold_weighed_value is that same restriction applied to money, so price-per-kg divides a value
-- and a weight drawn from ONE set of sales.
--
-- SOLD VALUE IS DELIBERATELY NOT TOUCHED (maintainer decision, same day). The Sold value and
-- profit already on screen come from a different legacy source that disagrees with salesDB on
-- revenue; the maintainer chose to keep them. sold_weighed_value therefore feeds the per-kg
-- figure ONLY and must never be summed into the sold-value column -- two sources for one number
-- is the drift that decision exists to avoid.
ALTER TABLE public.procurement_loads
    -- The day the animals REACHED THE FARM, which is not the purchase date: the farm warms animals
    -- up at the source, so a load is bought a day or more before it is unloaded here. The
    -- fattening clock starts on arrival, so this is the date it counts from.
    ADD COLUMN arrived_on date,
    ADD COLUMN sold_weight_kg numeric(12, 2),
    ADD COLUMN sold_weighed_animals integer,
    ADD COLUMN sold_weighed_value numeric(14, 2),
    -- Days between arrival and sale, ANIMAL-WEIGHTED across the load's sales: a load that leaves
    -- in four batches over four months has no single sale date, and weighting by how many animals
    -- left on each one answers "how long was the average animal fattened" rather than "when did
    -- the last straggler go". Stored because the per-sale animal counts it is computed from never
    -- reached GoatOS; recomputing it here would need the sales tagged to their animals first.
    ADD COLUMN fattening_days integer;

ALTER TABLE public.procurement_loads
    ADD CONSTRAINT procurement_loads_sold_weight_positive
        CHECK (sold_weight_kg IS NULL OR sold_weight_kg > 0),
    -- A weight with no denominator cannot produce an average, and an average is the only thing
    -- this column exists for; refuse the half-recorded pair at the schema.
    ADD CONSTRAINT procurement_loads_sold_weight_needs_animals
        CHECK ((sold_weight_kg IS NULL) = (sold_weighed_animals IS NULL)),
    ADD CONSTRAINT procurement_loads_sold_weighed_animals_positive
        CHECK (sold_weighed_animals IS NULL OR sold_weighed_animals > 0),
    ADD CONSTRAINT procurement_loads_sold_weighed_value_nonneg
        CHECK (sold_weighed_value IS NULL OR sold_weighed_value >= 0),
    -- Negative fattening days would mean the load sold before it arrived. Zero is allowed: a load
    -- sold the day it landed is a real, if unusual, event.
    ADD CONSTRAINT procurement_loads_fattening_days_nonneg
        CHECK (fattening_days IS NULL OR fattening_days >= 0);

-- The read is always "every line of THIS load", and the roll-up groups them by kind.
CREATE INDEX procurement_load_cost_lines_load_idx
    ON public.procurement_load_cost_lines (tenant_id, load_id, kind);

-- +goose Down
DROP TABLE IF EXISTS public.procurement_load_cost_lines;
ALTER TABLE public.procurement_loads
    DROP CONSTRAINT IF EXISTS procurement_loads_purchase_weight_positive,
    DROP CONSTRAINT IF EXISTS procurement_loads_sold_weight_positive,
    DROP CONSTRAINT IF EXISTS procurement_loads_sold_weight_needs_animals,
    DROP CONSTRAINT IF EXISTS procurement_loads_sold_weighed_animals_positive,
    DROP CONSTRAINT IF EXISTS procurement_loads_sold_weighed_value_nonneg,
    DROP CONSTRAINT IF EXISTS procurement_loads_fattening_days_nonneg,
    DROP COLUMN IF EXISTS purchase_weight_kg,
    DROP COLUMN IF EXISTS arrived_on,
    DROP COLUMN IF EXISTS sold_weight_kg,
    DROP COLUMN IF EXISTS sold_weighed_animals,
    DROP COLUMN IF EXISTS sold_weighed_value,
    DROP COLUMN IF EXISTS fattening_days;
