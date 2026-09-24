-- +goose Up
-- seed-fixture-guard:ignore: the three built-in product rows below are the exact vocabulary every
-- seed already writes as a literal ('Sheep', 'Goat', 'Manure'); no vaccination/HRMS seed contract
-- or source fixture schema changes.
--
-- WHAT THE FARM SELLS IS DATA, AND FEED IS ONE OF THE THINGS IT SELLS
-- (maintainer instruction 2026-09-23).
--
-- The farm sells feed it holds -- a trader takes two tonnes of maize off the CPT store -- and that
-- sale must come out of feed stock and be billed like any other sale. Two moves, and the first is
-- the one that makes the second cheap:
--
--   1. THE PRODUCT VOCABULARY STOPS BEING A CONSTANT. 'Sheep', 'Goat' and 'Manure' were written
--      three times over -- a CHECK here, ProductTypes in sales/domain, an option list in the page
--      contract -- so a fourth thing to sell was a migration plus a deploy. They become rows of
--      sellable_product_catalog, edited on Configuration -> Items and settings like every other
--      register, and the write path validates an entered product against the ACTIVE registry
--      INSIDE its own transaction (the feed_item_catalog rule, 000174 decision 2, one layer up).
--
--      It is NOT a foreign key, for the reason species_lookup is not one (000346): a product may be
--      renamed or archived while the sales already recorded under it keep the word they were sold
--      under. History is not re-written by a catalogue edit.
--
--   2. THE KIND IS THE ENGINE HOOK; THE LIST IS DATA. A row declares kind = animal | feed | other,
--      and only the kind reaches code: an animal line carries counts, a breed and live weight and
--      feeds the price-per-kg bands; a feed line names a feed item, draws that many kg out of the
--      store and is priced per kg; an other line (manure today) carries weight and revenue and no
--      animal count -- exactly what manure does now. A farm adding "Hay" or "Straw" as another
--      `other` product needs no developer. A NEW KIND does.
--
-- THE LINE'S SECOND DIMENSION WIDENS FROM breed TO VARIANT, keeping the column name. An animal
-- line's variant is its breed, a feed line's is the feed item ('Maize'), manure's is 'Manure' --
-- which is what that column has held for manure all along. Every (product, breed) reporting key,
-- band and chart therefore keeps working unchanged, and 'Feed / Maize' reads as naturally as
-- 'Goat / Sojat'. Renaming the column would have rewritten six read models to say a new word for
-- the same fact.

CREATE TABLE public.sellable_product_catalog (
    tenant_id    uuid    NOT NULL REFERENCES public.tenants (tenant_id),
    product_code text    NOT NULL CHECK (product_code ~ '^[a-z][a-z0-9_]{0,39}$'),
    -- The word STORED on a sale line. A sale records the name the farm sold under.
    name         text    NOT NULL CHECK (btrim(name) <> ''),
    kind         text    NOT NULL CHECK (kind IN ('animal', 'feed', 'other')),
    -- What one unit of it is. An animal product sells by the head, feed and manure by the kilogram.
    unit         text    NOT NULL DEFAULT 'kg' CHECK (btrim(unit) <> ''),
    -- An animal product's variants are the breeds of THIS species (the breeds register). A feed
    -- product's are the active feed_item_catalog. An other product has none. Nullable because only
    -- the animal kind uses it.
    species_code text,
    sort_order   integer NOT NULL DEFAULT 100,
    status       text    NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'archived')),
    -- The three the farm sells today. They may be renamed on screen; they are never archived or
    -- deleted, because the Sold page's KPIs, bands and buyer analytics still name them.
    is_builtin   boolean NOT NULL DEFAULT false,
    row_version  integer NOT NULL DEFAULT 1 CHECK (row_version >= 1),
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, product_code),
    CONSTRAINT sellable_product_species_only_for_animals
        CHECK (species_code IS NULL OR kind = 'animal')
);

-- The name is what a sale line stores, so two products may not share one.
CREATE UNIQUE INDEX sellable_product_catalog_tenant_name_uidx
    ON public.sellable_product_catalog (tenant_id, lower(btrim(name)));

COMMENT ON TABLE public.sellable_product_catalog IS
  'What the farm sells, as tenant rows edited on Configuration -> Items and settings. kind (animal|feed|other) is the only part code reads; the list itself is data. Validated at write time, never a foreign key: an archived or renamed product leaves already-recorded sales holding the word they were sold under.';

INSERT INTO public.sellable_product_catalog (tenant_id, product_code, name, kind, unit, species_code, sort_order, is_builtin)
SELECT t.tenant_id, v.code, v.name, v.kind, v.unit, v.species, v.sort, true
FROM public.tenants t
CROSS JOIN (VALUES
    ('sheep',  'Sheep',  'animal', 'head', 'sheep', 10),
    ('goat',   'Goat',   'animal', 'head', 'goat',  20),
    ('manure', 'Manure', 'other',  'kg',   NULL,    30)
) AS v(code, name, kind, unit, species, sort)
ON CONFLICT DO NOTHING;


-- ---------------------------------------------------------------------------
-- The line learns to carry a quantity at a rate, and which feed it drew on.
-- ---------------------------------------------------------------------------
-- quantity/unit/rate_per_unit are GENERIC, not feed columns: manure is sold by the kilogram at a
-- rate too, and the farm's next `other` product will be. sales_value stays the authoritative money
-- figure (it is what every existing read sums); for a line priced by rate it is quantity * rate,
-- computed once on write so a stored value can never disagree with the arithmetic beside it.
-- THE LINE IS STAMPED WITH THE PRODUCT IT WAS SOLD UNDER, three facts that answer three
-- different questions and must not be collapsed into one:
--
--   product_type  the NAME sold under -- what a reader is shown, and what every (product, breed)
--                 band and chart already keys on. A later rename does not rewrite it.
--   product_code  the registry row's stable identity. The Sold page's Sheep / Goat / Manure KPIs
--                 key on THIS, so renaming 'Sheep' to 'Mutton sheep' on screen moves the label and
--                 not the number out of the card.
--   product_kind  what CODE acts on: an animal line carries counts and feeds the price-per-kg
--                 bands, a feed line draws stock, an other line carries weight and revenue alone.
--                 Stamped at write time, so a read never re-derives it from a name.
ALTER TABLE public.sales_deal_lines
    ADD COLUMN product_code  text,
    ADD COLUMN product_kind  text,
    ADD COLUMN quantity      numeric,
    ADD COLUMN unit          text,
    ADD COLUMN rate_per_unit numeric;

-- There is deliberately NO feed_item_key on the line. The line already names the feed in its
-- variant, and the key that stock is actually drawn on lives on the depletion ledger below -- a
-- second key here would be a copy that could disagree, and on an animal line it would be a
-- normalised BREED sitting in a column called feed.

-- Every line recorded so far is one of the three built-ins, so each is stamped with what it
-- already was. Nothing any of them reports changes.
UPDATE public.sales_deal_lines SET product_code = lower(product_type),
       product_kind = CASE WHEN product_type = 'Manure' THEN 'other' ELSE 'animal' END
WHERE product_code IS NULL;

ALTER TABLE public.sales_deal_lines
    ALTER COLUMN product_code SET NOT NULL,
    ALTER COLUMN product_kind SET NOT NULL,
    ADD CONSTRAINT sales_deal_lines_product_kind_check CHECK (product_kind IN ('animal', 'feed', 'other')),
    ADD CONSTRAINT sales_deal_lines_product_code_check CHECK (product_code ~ '^[a-z][a-z0-9_]{0,39}$'),
    ADD CONSTRAINT sales_deal_lines_quantity_nonneg CHECK (quantity IS NULL OR quantity >= 0),
    ADD CONSTRAINT sales_deal_lines_rate_nonneg CHECK (rate_per_unit IS NULL OR rate_per_unit >= 0);

-- ---------------------------------------------------------------------------
-- The product CHECKs give way to the registry.
-- ---------------------------------------------------------------------------
-- A CHECK cannot ask a catalogue, and the catalogue is now the answer. What survives here is the
-- part a catalogue cannot enforce: a product is a non-blank word, and 'Mixed' stays a DEAL-level
-- rollup that a LINE may never claim (000296).
ALTER TABLE public.sales_deals DROP CONSTRAINT sales_deals_product_type_check;
ALTER TABLE public.sales_deals
    ADD CONSTRAINT sales_deals_product_type_check CHECK (btrim(product_type) <> '');

ALTER TABLE public.sales_deal_lines DROP CONSTRAINT sales_deal_lines_product_type_check;
ALTER TABLE public.sales_deal_lines
    ADD CONSTRAINT sales_deal_lines_product_type_check
    CHECK (btrim(product_type) <> '' AND product_type <> 'Mixed');


-- ---------------------------------------------------------------------------
-- feed_sale_depletions -- sold feed leaving the store
-- ---------------------------------------------------------------------------
-- The stock read (feeddirection analytics stockItemsSQL) already unions two consumption sources at
-- one grain -- locked-sheet directed kg, and feed_effective_external_consumption for feeds the
-- ration grid does not direct. Sold feed is a THIRD, and it arrives the same way: its own ledger,
-- at the same (park, feed, day) grain, written by Sales in the SAME transaction as the deal.
--
-- IT IS A LEDGER AND NOT A JOIN ON PURPOSE. feeddirection must not read a sales table to know its
-- own stock -- that is the module boundary the external-consumption ledger was built to respect --
-- and a ledger row is what makes an edited or cancelled deal repairable: the row is replaced with
-- the deal's lines, never recomputed from a join whose shape the two modules would have to agree on.
CREATE TABLE public.feed_sale_depletions (
    feed_sale_depletion_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id       uuid NOT NULL REFERENCES public.tenants (tenant_id),
    -- The deal and the line it came from: provenance, and the key the write path replaces on.
    deal_id         uuid NOT NULL REFERENCES public.sales_deals (id) ON DELETE CASCADE,
    line_id         uuid NOT NULL REFERENCES public.sales_deal_lines (line_id) ON DELETE CASCADE,
    -- Both, for the same reason feed_purchases keeps both: the stock read groups by farm label and
    -- matches consumption by park, and a farm may be sold from before its park row exists.
    park_id         uuid,
    farm_label      text NOT NULL CHECK (btrim(farm_label) <> ''),
    feed_item_label text NOT NULL CHECK (btrim(feed_item_label) <> ''),
    feed_item_key   text GENERATED ALWAYS AS (public.feed_config_norm(feed_item_label)) STORED,
    -- The sale's own date. Stock leaves the store on the day it is sold.
    feed_day        date NOT NULL,
    quantity_kg     numeric(12, 3) NOT NULL CHECK (quantity_kg >= 0),
    created_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT feed_sale_depletions_pkey PRIMARY KEY (feed_sale_depletion_id),
    -- One line depletes once. A re-recorded deal replaces its lines, and the cascade takes these.
    CONSTRAINT feed_sale_depletions_line_uq UNIQUE (tenant_id, line_id)
);

-- Serving read: the stock rollup scans a tenant's sold feed per item in day order, narrowed by park.
CREATE INDEX feed_sale_depletions_serve_idx
    ON public.feed_sale_depletions (tenant_id, feed_item_key, feed_day);

COMMENT ON TABLE public.feed_sale_depletions IS
  'Feed sold off the store, written by Sales in the deal transaction and unioned into the feed stock read beside locked-sheet directed kg and external consumption. A ledger rather than a join so feeddirection never reads a sales table.';


-- +goose Down

DROP TABLE IF EXISTS public.feed_sale_depletions;

ALTER TABLE public.sales_deal_lines DROP CONSTRAINT IF EXISTS sales_deal_lines_product_type_check;
ALTER TABLE public.sales_deal_lines DROP CONSTRAINT IF EXISTS sales_deal_lines_quantity_nonneg;
ALTER TABLE public.sales_deal_lines DROP CONSTRAINT IF EXISTS sales_deal_lines_rate_nonneg;
ALTER TABLE public.sales_deal_lines DROP CONSTRAINT IF EXISTS sales_deal_lines_product_kind_check;
ALTER TABLE public.sales_deal_lines DROP CONSTRAINT IF EXISTS sales_deal_lines_product_code_check;
ALTER TABLE public.sales_deal_lines
    DROP COLUMN rate_per_unit,
    DROP COLUMN unit,
    DROP COLUMN quantity,
    DROP COLUMN product_kind,
    DROP COLUMN product_code;

-- A line sold under a product the narrower CHECK does not name records what really happened, so the
-- old vocabulary is re-added NOT VALID and deliberately not validated.
ALTER TABLE public.sales_deal_lines
    ADD CONSTRAINT sales_deal_lines_product_type_check
    CHECK (product_type IN ('Sheep', 'Goat', 'Manure')) NOT VALID;

ALTER TABLE public.sales_deals DROP CONSTRAINT IF EXISTS sales_deals_product_type_check;
ALTER TABLE public.sales_deals
    ADD CONSTRAINT sales_deals_product_type_check
    CHECK (product_type IN ('Sheep', 'Goat', 'Manure', 'Mixed')) NOT VALID;

DROP TABLE IF EXISTS public.sellable_product_catalog;
