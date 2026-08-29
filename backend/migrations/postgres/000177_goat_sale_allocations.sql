-- +goose Up

-- GOAT SALE ALLOCATIONS: which real animals a recorded sale is made of.
--
-- Maintainer decision 2026-08-20. The Sales page gained a "Tag animals to sale" flow:
-- pick a park, a shed and its pens, multi-select animals across as many sheds as the
-- sale spans, review the picked RFIDs shed-wise, confirm, and the animals become sold.
--
-- WHY THIS TABLE LIVES ON THE HERD SIDE AND NOT IN THE SALES MODULE
-- -----------------------------------------------------------------
-- 000173_sales_ledger.sql states the sales lock: "Sales therefore owns its own tables
-- and reads NOTHING from the herd, vaccination, or procurement schemas." A deal->animal
-- mapping has to carry a goat_id, so putting it in the sales module would break that
-- lock outright. It lives here instead, keyed by goat_id, and carries the sales deal as
-- an OPAQUE REFERENCE. Sales still stores no goat_id and still reads no herd table; the
-- Sales page reads this mapping through an identity endpoint. Both modules keep their
-- own tables and the lock is untouched.
--
-- sales_deal_id is deliberately NOT a foreign key to sales_deals. A cross-module FK
-- would couple identity's write path to the sales schema and make a sales migration able
-- to block a herd write -- the same coupling the lock exists to prevent. The reference is
-- validated by the application against the deal read, not by the database.
--
-- WHY THE LOCATION IS SNAPSHOTTED
-- --------------------------------
-- park_id / shed_id / partition_label / tag_number record where the animal was AT THE
-- MOMENT IT WAS SOLD. goats.shed_id keeps moving and a sold animal's row eventually says
-- nothing useful about the sale, so reading the location back off the goat would make an
-- old sale silently re-describe itself. The snapshot is history and is never updated.
--
-- seed-fixture-guard:ignore: operational sale-to-animal ledger, written only by runtime sale confirmation

CREATE TABLE public.goat_sale_allocations (
    allocation_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,

    goat_id uuid NOT NULL,

    -- The sales ledger's deal id, as an opaque reference. See above for why this is not
    -- a foreign key.
    sales_deal_id uuid NOT NULL,

    -- Where the animal stood when it was tagged to the sale. History, never updated.
    park_id uuid,
    shed_id uuid,
    partition_label text,
    -- The identifier the picker showed the person doing the tagging, so the confirmation
    -- screen and the audit trail name the same string the operator read off the animal.
    tag_number text,

    -- tagged  -> picked and confirmed; the goat exit has been applied.
    -- released -> the tagging was undone; the row is kept as history and stops being live.
    status text DEFAULT 'tagged' NOT NULL,

    allocated_at timestamp with time zone DEFAULT now() NOT NULL,
    allocated_by uuid,
    released_at timestamp with time zone,
    released_by uuid,
    release_reason text,

    -- Write-path idempotency, per AGENTS.md's mandatory idempotency contract. The key is
    -- derived from (deal, goat) so a retried confirm cannot tag the same animal twice.
    idempotency_key text NOT NULL,

    row_version integer DEFAULT 1 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,

    CONSTRAINT goat_sale_allocations_pkey PRIMARY KEY (allocation_id),
    CONSTRAINT goat_sale_allocations_goat_fkey
        FOREIGN KEY (goat_id) REFERENCES public.goats (goat_id),
    CONSTRAINT goat_sale_allocations_status_check
        CHECK (status IN ('tagged', 'released')),
    CONSTRAINT goat_sale_allocations_released_check
        CHECK ((status = 'tagged' AND released_at IS NULL)
            OR (status = 'released' AND released_at IS NOT NULL)),
    CONSTRAINT goat_sale_allocations_row_version_check CHECK (row_version >= 1)
);

-- ONE LIVE SALE PER ANIMAL. An animal cannot be sold to two buyers, and the partial
-- index is what makes that true in the database rather than only in the service: a
-- released row is history and must not block a later, corrected sale of the same animal.
CREATE UNIQUE INDEX goat_sale_allocations_live_goat_uq
    ON public.goat_sale_allocations (tenant_id, goat_id) WHERE status = 'tagged';

-- Idempotency replay lookup.
CREATE UNIQUE INDEX goat_sale_allocations_idempotency_uq
    ON public.goat_sale_allocations (tenant_id, idempotency_key);

-- The read the Sales page makes: every animal behind one deal, shed-wise.
CREATE INDEX goat_sale_allocations_deal_idx
    ON public.goat_sale_allocations (tenant_id, sales_deal_id, shed_id);

-- +goose Down

DROP TABLE IF EXISTS public.goat_sale_allocations;
