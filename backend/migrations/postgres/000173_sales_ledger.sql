-- +goose Up

-- The SALES LEDGER: what the farm actually sold, to whom, and the demand pipeline behind it.
--
-- Maintainer decision 2026-08-17: the "Sales DB" Google Sheet moves into Postgres, which becomes
-- canonical. The sheet is imported ONCE (fixtures/sales-db-2026-08-17/sales-db.json, exported
-- 2026-08-17) and is then NOT synced -- no importer job, no write-back. New sales are recorded
-- through POST /sales/deals; the sheet is history.
--
-- WHY THIS IS ITS OWN MODULE AND NOT procurement/goats/counts
-- -----------------------------------------------------------
-- A sale is a COMMERCIAL fact about a transaction with a buyer, not a herd fact. The ledger's
-- animal rows carry sheet-era tag strings ("155", "V-90") that predate the RFID register and
-- resolve to no goat; forcing a join onto goats/goat_identifiers would either reject real history
-- or fabricate identity. Sales therefore owns its own tables and reads NOTHING from the herd,
-- vaccination, or procurement schemas. The terminal sold/transferred EXIT of a live animal stays
-- on its authoritative herd workflow; this ledger records the deal.
--
-- IMPORT PROVENANCE
-- -----------------
-- Every table carries source_row_no (1-based position in the exported sheet tab) as the import
-- natural key, unique per tenant WHERE NOT NULL so re-running the importer updates in place and a
-- row recorded in the app (source_row_no NULL) can never collide with it. source_sales_id /
-- source_purchase_id are the sheet's own reference numbers -- they repeat and are references,
-- never keys.

CREATE TABLE public.sales_deals (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,

    sale_date date NOT NULL,
    farm text NOT NULL,

    -- Sheet references. source_sales_id repeats across tabs (sold tags point back at it), so it is
    -- a reference column, never a grouping key.
    source_sales_id integer,
    source_purchase_id integer,
    source_row_no integer,

    buyer_name text NOT NULL,
    buyer_place text,

    product_type text NOT NULL,
    breed text NOT NULL,

    -- Counts are numeric rather than integer because the sheet stores them as decimals (23.0) and
    -- a manure deal has none at all. animal_count is the authoritative count when present; the
    -- male/female split is detail that may exist without it or alongside it.
    animal_count numeric,
    male_count numeric,
    female_count numeric,
    total_weight_kg numeric,

    advance_amount numeric,
    sales_value numeric DEFAULT 0 NOT NULL,

    status text DEFAULT 'Deal Closed' NOT NULL,
    feedback text,
    comments text,

    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,

    CONSTRAINT sales_deals_pkey PRIMARY KEY (id),
    CONSTRAINT sales_deals_farm_check CHECK (farm IN ('CBE', 'CPT')),
    CONSTRAINT sales_deals_product_type_check CHECK (product_type IN ('Sheep', 'Goat', 'Manure')),
    CONSTRAINT sales_deals_status_check
        CHECK (status IN ('Deal Closed', 'Deal Failed', 'In Discussion', 'Advance Paid')),
    CONSTRAINT sales_deals_buyer_name_not_blank CHECK (btrim(buyer_name) <> ''),
    CONSTRAINT sales_deals_breed_not_blank CHECK (btrim(breed) <> ''),
    CONSTRAINT sales_deals_sales_value_nonneg CHECK (sales_value >= 0)
);

-- Import idempotency: one ledger row per sheet row. Partial so app-recorded deals (NULL) never
-- collide.
CREATE UNIQUE INDEX sales_deals_source_row_uq
    ON public.sales_deals (tenant_id, source_row_no) WHERE source_row_no IS NOT NULL;

-- Keyset order for the ledger read: newest sale first, id as the tiebreaker.
CREATE INDEX sales_deals_keyset_idx ON public.sales_deals (tenant_id, sale_date DESC, id);

-- The farm facet the page filters by.
CREATE INDEX sales_deals_farm_idx ON public.sales_deals (tenant_id, farm, sale_date DESC);


-- Buyer demand pipeline: people who called or were called about animals. call_status NULL means
-- the lead has not been contacted yet -- the overview reports that bucket as "uncontacted".
CREATE TABLE public.sales_buyer_leads (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,

    recorded_date date,
    farm text,
    source_sales_id integer,
    source_row_no integer,

    buyer_name text NOT NULL,
    buyer_place text,
    animal_type text,
    breed text,
    call_status text,

    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,

    CONSTRAINT sales_buyer_leads_pkey PRIMARY KEY (id),
    CONSTRAINT sales_buyer_leads_buyer_name_not_blank CHECK (btrim(buyer_name) <> '')
);

CREATE UNIQUE INDEX sales_buyer_leads_source_row_uq
    ON public.sales_buyer_leads (tenant_id, source_row_no) WHERE source_row_no IS NOT NULL;

CREATE INDEX sales_buyer_leads_tenant_idx ON public.sales_buyer_leads (tenant_id, farm);


-- FPO (farmer producer organisation) demand pipeline. Company-wide: no farm column exists in the
-- source, so the overview never pretends to filter it by farm.
CREATE TABLE public.sales_fpo_leads (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,

    source_row_no integer,

    fpo_name text NOT NULL,
    crops text,
    district text,
    taluk text,
    state text,
    call_status text,

    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,

    CONSTRAINT sales_fpo_leads_pkey PRIMARY KEY (id),
    CONSTRAINT sales_fpo_leads_fpo_name_not_blank CHECK (btrim(fpo_name) <> '')
);

CREATE UNIQUE INDEX sales_fpo_leads_source_row_uq
    ON public.sales_fpo_leads (tenant_id, source_row_no) WHERE source_row_no IS NOT NULL;

CREATE INDEX sales_fpo_leads_tenant_idx ON public.sales_fpo_leads (tenant_id, district);


-- Per-animal evidence behind sold deals: the tag list the field team recorded at handover. Tag
-- numbers are sheet-era strings that predate the RFID register; they resolve to no goat row and
-- are deliberately NOT joined to goat_identifiers.
CREATE TABLE public.sales_sold_animal_tags (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,

    source_row_no integer,

    animal_label text NOT NULL,
    tag_number text,
    weight_kg numeric,
    farm text,
    source_sales_id integer,

    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,

    CONSTRAINT sales_sold_animal_tags_pkey PRIMARY KEY (id),
    CONSTRAINT sales_sold_animal_tags_label_not_blank CHECK (btrim(animal_label) <> '')
);

CREATE UNIQUE INDEX sales_sold_animal_tags_source_row_uq
    ON public.sales_sold_animal_tags (tenant_id, source_row_no) WHERE source_row_no IS NOT NULL;

CREATE INDEX sales_sold_animal_tags_tenant_idx ON public.sales_sold_animal_tags (tenant_id, farm);


-- Weight audit: video-verified weight vs the book weight for sold animals -- the evidence panel
-- that says whether the scale can be trusted. Company-wide (the source carries no farm).
CREATE TABLE public.sales_weight_audit (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,

    source_row_no integer,

    video_weight_kg numeric NOT NULL,
    book_weight_kg numeric NOT NULL,
    farm_born boolean DEFAULT false NOT NULL,
    tag_number text,

    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,

    CONSTRAINT sales_weight_audit_pkey PRIMARY KEY (id)
);

CREATE UNIQUE INDEX sales_weight_audit_source_row_uq
    ON public.sales_weight_audit (tenant_id, source_row_no) WHERE source_row_no IS NOT NULL;

CREATE INDEX sales_weight_audit_tenant_idx ON public.sales_weight_audit (tenant_id);


-- Market benchmarks: what other sellers quote per kg, kept alongside the farm's own realized
-- price. market_price_per_kg is parsed out of the market string AT IMPORT TIME
-- ('Chennai -Sheep - 370 Rs Per kg' -> 370) so the read path never parses prose.
CREATE TABLE public.sales_market_benchmarks (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,

    source_row_no integer,

    market text,
    category text,
    breed text NOT NULL,
    source text,
    ex_farm_rate text,
    transport_rate text,
    landing_cost_per_kg numeric,
    market_price_per_kg numeric,

    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,

    CONSTRAINT sales_market_benchmarks_pkey PRIMARY KEY (id),
    CONSTRAINT sales_market_benchmarks_breed_not_blank CHECK (btrim(breed) <> '')
);

CREATE UNIQUE INDEX sales_market_benchmarks_source_row_uq
    ON public.sales_market_benchmarks (tenant_id, source_row_no) WHERE source_row_no IS NOT NULL;

CREATE INDEX sales_market_benchmarks_tenant_idx ON public.sales_market_benchmarks (tenant_id);


-- +goose Down

DROP TABLE IF EXISTS public.sales_market_benchmarks;
DROP TABLE IF EXISTS public.sales_weight_audit;
DROP TABLE IF EXISTS public.sales_sold_animal_tags;
DROP TABLE IF EXISTS public.sales_fpo_leads;
DROP TABLE IF EXISTS public.sales_buyer_leads;
DROP TABLE IF EXISTS public.sales_deals;
