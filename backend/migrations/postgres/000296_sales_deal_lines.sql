-- +goose Up

-- ONE SALE, MANY LINES (maintainer decision 2026-09-12).
--
-- A buyer takes sheep AND goats, of more than one breed, in one deal -- one date, one buyer, one
-- advance, one status, one set of receipts. Until now sales_deals carried ONE product_type and ONE
-- breed, so the desk either recorded a mixed sale as several deals (fragmenting the money: the
-- advance and the receipts belong to the whole deal, not to one breed of it) or filed the whole
-- sale under whichever breed was largest and lost the rest.
--
-- The deal keeps everything that is about the TRANSACTION. What was sold moves to lines:
-- product, breed, animals, male/female split, weight and value, each per line. The value is per
-- line ON PURPOSE: the Sold page's price-per-kg bands are keyed by (product, breed), and a single
-- lump value over a mixed sale cannot be divided between two breeds honestly.
--
-- THE DEAL-LEVEL COLUMNS STAY AND BECOME A ROLLUP maintained in the same transaction as the
-- lines: counts and weight and sales_value are sums; product_type and breed are the single value
-- when every line agrees and 'Mixed' otherwise. Every existing reader (the ledger, tagging animals
-- to a sale, the load-wise sale revenue, the sex-count sync) keeps reading the deal row it read
-- before and stays correct; only the by-breed reads switch to the lines.
--
-- BACKFILL: every deal recorded so far IS one line, so each gets exactly one line copied from its
-- own columns. Nothing changes in what those deals report.

CREATE TABLE public.sales_deal_lines (
    line_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    deal_id uuid NOT NULL,
    -- The order the desk entered the lines in; the detail views render them in this order.
    line_no integer NOT NULL,

    product_type text NOT NULL,
    breed text NOT NULL,

    animal_count numeric,
    male_count numeric,
    female_count numeric,
    total_weight_kg numeric,
    sales_value numeric DEFAULT 0 NOT NULL,

    created_at timestamp with time zone DEFAULT now() NOT NULL,

    CONSTRAINT sales_deal_lines_pkey PRIMARY KEY (line_id),
    CONSTRAINT sales_deal_lines_deal_fk
        FOREIGN KEY (deal_id) REFERENCES public.sales_deals (id) ON DELETE CASCADE,
    CONSTRAINT sales_deal_lines_line_no_positive CHECK (line_no >= 1),
    CONSTRAINT sales_deal_lines_product_type_check CHECK (product_type IN ('Sheep', 'Goat', 'Manure')),
    CONSTRAINT sales_deal_lines_breed_not_blank CHECK (btrim(breed) <> ''),
    CONSTRAINT sales_deal_lines_sales_value_nonneg CHECK (sales_value >= 0),
    CONSTRAINT sales_deal_lines_animal_count_nonneg CHECK (animal_count IS NULL OR animal_count >= 0),
    CONSTRAINT sales_deal_lines_weight_nonneg CHECK (total_weight_kg IS NULL OR total_weight_kg >= 0),
    CONSTRAINT sales_deal_lines_deal_line_no_uq UNIQUE (tenant_id, deal_id, line_no)
);

-- The one access path: every line of the deals on one ledger page, `deal_id = ANY(...)`.
CREATE INDEX sales_deal_lines_deal_idx ON public.sales_deal_lines (tenant_id, deal_id, line_no);

-- Every recorded deal so far is a single-line sale.
INSERT INTO public.sales_deal_lines (
    tenant_id, deal_id, line_no, product_type, breed,
    animal_count, male_count, female_count, total_weight_kg, sales_value, created_at
)
SELECT d.tenant_id, d.id, 1, d.product_type, d.breed,
       d.animal_count, d.male_count, d.female_count, d.total_weight_kg, d.sales_value, d.created_at
FROM public.sales_deals d
WHERE NOT EXISTS (
    SELECT 1 FROM public.sales_deal_lines l WHERE l.tenant_id = d.tenant_id AND l.deal_id = d.id
);

-- The deal-level rollup may now read 'Mixed' when the lines disagree. The LINE check above still
-- names only real products: 'Mixed' is a summary word, never something sold.
ALTER TABLE public.sales_deals DROP CONSTRAINT sales_deals_product_type_check;
ALTER TABLE public.sales_deals
    ADD CONSTRAINT sales_deals_product_type_check
    CHECK (product_type IN ('Sheep', 'Goat', 'Manure', 'Mixed'));

-- +goose Down

ALTER TABLE public.sales_deals DROP CONSTRAINT sales_deals_product_type_check;
-- A deal rolled up to 'Mixed' cannot go back to one product; its first line is the honest
-- single-product reading the old shape can hold.
UPDATE public.sales_deals d
SET product_type = l.product_type, breed = l.breed
FROM public.sales_deal_lines l
WHERE l.tenant_id = d.tenant_id AND l.deal_id = d.id AND l.line_no = 1 AND d.product_type = 'Mixed';
ALTER TABLE public.sales_deals
    ADD CONSTRAINT sales_deals_product_type_check
    CHECK (product_type IN ('Sheep', 'Goat', 'Manure'));
DROP TABLE public.sales_deal_lines;
