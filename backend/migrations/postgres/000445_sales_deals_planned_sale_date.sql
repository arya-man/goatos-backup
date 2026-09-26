-- +goose Up
-- seed-fixture-guard:ignore: one nullable history column on the sales ledger; no seed contract,
-- source fixture or read-model shape change.
--
-- A CLOSED SALE USES THE CLOSE DATE (maintainer decision 2026-09-25, docs/decisions/sales-sop.md).
-- A sale recorded open (In Discussion / Advance Paid) for a planned day and then closed is a sale
-- made on the CLOSE day: SetDealStatus stamps the close business date (Asia/Kolkata) as sale_date,
-- which dates its revenue and the feed store's depletion. The day it was originally recorded for
-- is kept here, written on the FIRST close only and never again; NULL on every sale recorded
-- already closed (its typed date IS its sale date) and on every existing row.
-- Nullable with no default: a metadata-only ALTER, no table rewrite.
ALTER TABLE public.sales_deals ADD COLUMN IF NOT EXISTS planned_sale_date date;
COMMENT ON COLUMN public.sales_deals.planned_sale_date IS
  'The sale date an OPEN deal was recorded for, kept when it closed and sale_date became the close date (2026-09-25). NULL for a deal recorded already closed.';

-- +goose Down
ALTER TABLE public.sales_deals DROP COLUMN IF EXISTS planned_sale_date;
