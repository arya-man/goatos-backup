-- +goose Up
-- seed-fixture-guard:ignore: one optional column on the commercial feed-purchase ledger; no
-- vaccination/HRMS seed contract or read-model change.
--
-- DAYS OF STOCK ON A FEED LOAD (maintainer request 2026-09-19).
--
-- When the procurement desk records a load it may say how many DAYS of feeding the load is
-- meant to cover. It is OPTIONAL -- a number the buyer believes, not a fact the farm measured --
-- and sheet history never carried one, so every existing row stays NULL. Nothing on the write
-- path reads it; the Feed Analytics "Purchased vs consumed" tab shows it beside what actually
-- happened to the load (FIFO consumption off the locked feed sheets) so the two can be compared
-- per load: days said minus days consumed minus days left, highlighted when it does not come
-- out at zero and red when it is negative (the load ran out sooner than it was bought for).
ALTER TABLE public.feed_purchases
  ADD COLUMN IF NOT EXISTS days_of_stock integer;

ALTER TABLE public.feed_purchases
  DROP CONSTRAINT IF EXISTS feed_purchases_days_of_stock_check,
  ADD CONSTRAINT feed_purchases_days_of_stock_check
    CHECK (days_of_stock IS NULL OR days_of_stock > 0);

COMMENT ON COLUMN public.feed_purchases.days_of_stock IS
  'How many days of feeding the buyer said this load covers, entered optionally at purchase time. NULL when not stated (all sheet history). Reporting only: compared per load against FIFO consumption on /feed/analytics.';

-- +goose Down
ALTER TABLE public.feed_purchases
  DROP CONSTRAINT IF EXISTS feed_purchases_days_of_stock_check,
  DROP COLUMN IF EXISTS days_of_stock;
