-- +goose Up
-- seed-fixture-guard:ignore: relaxes two NOT NULLs on the commercial sales ledger and adds one
-- shape check; no vaccination/HRMS seed contract, source fixture or read-model change.
--
-- AN ADVANCE CAN BE TAKEN BEFORE THE SALE IS DECIDED (maintainer decision 2026-10-02,
-- docs/decisions/sales-sop.md -> "An advance before anything is chosen").
--
-- A buyer hands over money for a future sale before anyone knows whether it will be animals, feed
-- or manure. The desk records it as an ADVANCE-ONLY sale: buyer, farm, date and the advance, with
-- NO product lines and NO sale value. The lines are added to the SAME sale later, and only then
-- does its workflow open (sales.deal.recorded is emitted by the add-lines write, not the record).
--
-- An advance-only sale stores product_type and breed as NULL -- not a placeholder word, which
-- would leak onto every screen and every report that groups by product. The existing not-blank
-- checks already pass a NULL; only the NOT NULLs refused it.
--
-- The shape check keeps the two columns together and keeps such a sale out of revenue: a sale
-- with no product is never Deal Closed and is worth nothing yet. Every stored row has both
-- columns set, so the check validates without rewriting anything.
--
-- LOCK SAFETY: DROP NOT NULL is metadata-only; ADD ... NOT VALID takes a brief ACCESS EXCLUSIVE
-- lock with no scan; VALIDATE scans the (few-thousand-row) ledger under SHARE UPDATE EXCLUSIVE.
SET lock_timeout = '5s';

ALTER TABLE public.sales_deals ALTER COLUMN product_type DROP NOT NULL;
ALTER TABLE public.sales_deals ALTER COLUMN breed DROP NOT NULL;
ALTER TABLE public.sales_deals
  ADD CONSTRAINT sales_deals_advance_only_shape CHECK (
    (product_type IS NULL) = (breed IS NULL)
    AND (product_type IS NOT NULL OR (status <> 'Deal Closed' AND sales_value = 0))
  ) NOT VALID;
ALTER TABLE public.sales_deals VALIDATE CONSTRAINT sales_deals_advance_only_shape;

-- +goose Down
-- Restoring the NOT NULLs would fail on any advance-only sale recorded since; those rows are real
-- money the buyer handed over and are not deleted to make a rollback pass.
SET lock_timeout = '5s';
ALTER TABLE public.sales_deals DROP CONSTRAINT IF EXISTS sales_deals_advance_only_shape;
