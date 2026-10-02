-- +goose Up
-- seed-fixture-guard:ignore: one new per-deal settlement table on the commercial sales ledger; no
-- vaccination/HRMS seed contract, source fixture or read-model change.
--
-- A FAILED SALE'S MONEY IS SETTLED, ONE OF TWO WAYS, CHOSEN PER CASE (maintainer decision
-- 2026-10-02, docs/decisions/sales-sop.md -> "An advance before anything is chosen").
--
-- When a sale the buyer paid towards never happens, the desk records what became of the money:
-- some or all of it handed back (refunded_rupees, with the day it went back), and whatever was not
-- handed back kept by the farm. Refunding nothing IS the "farm keeps it" decision, recorded as a
-- row so it reads differently from a failed sale nobody has looked at yet.
--
-- ONE row per deal: the settlement is a decision about the deal, and correcting it replaces the
-- row rather than stacking a second answer beside it. The amount is bounded by what the buyer
-- actually handed over in the service, inside the same transaction that locks the deal.
--
-- Tiny table (one row per failed paid sale); no index beyond the key is needed.
CREATE TABLE IF NOT EXISTS public.sales_deal_advance_settlements (
    tenant_id       uuid NOT NULL,
    deal_id         uuid NOT NULL,
    refunded_rupees numeric(14,2) NOT NULL,
    refunded_on     date,
    note            text,
    settled_by      text NOT NULL,
    created_at      timestamp with time zone DEFAULT now() NOT NULL,
    updated_at      timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT sales_deal_advance_settlements_pkey PRIMARY KEY (tenant_id, deal_id),
    CONSTRAINT sales_deal_advance_settlements_deal_fk FOREIGN KEY (deal_id)
        REFERENCES public.sales_deals (id) ON DELETE CASCADE,
    CONSTRAINT sales_deal_advance_settlements_refund_nonneg CHECK (refunded_rupees >= 0),
    -- A refund says when the money went back; keeping it all has no such day.
    CONSTRAINT sales_deal_advance_settlements_refund_dated
        CHECK ((refunded_rupees > 0) = (refunded_on IS NOT NULL))
);

-- +goose Down
DROP TABLE IF EXISTS public.sales_deal_advance_settlements;
