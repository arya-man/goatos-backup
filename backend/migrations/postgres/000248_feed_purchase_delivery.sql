-- +goose Up
-- seed-fixture-guard:ignore: additive delivery state on the commercial feed-purchase ledger; no
-- vaccination/HRMS seed contract change.
--
-- FEED PURCHASE DELIVERY (maintainer decision 2026-09-03).
--
-- Buying feed and RECEIVING it are days apart: the procurement desk records the load when the
-- money is committed, the truck takes three or four days, and only what comes off the truck is
-- feed the farm can use. Until now a recorded purchase went into stock on its purchase date, so
-- the stock and days-left cards counted feed still on the road, and the aflatoxin strip test was
-- born before there was anything to test.
--
-- A load now carries a delivery state:
--
--   purchased   bought, still in transit -- counted NOWHERE as stock, no toxin task yet
--   reached     arrived on reached_on -- counted as stock from that day, toxin test born then
--
-- The weight that goes into stock is the weight actually received (reached_weight_kg) when the
-- desk has entered it, and the buying weight (quantity_kg) until then. Entering the reached
-- weight is DEFERRABLE: the load reaches, feed starts being used, and the weighbridge figure is
-- typed in whenever it is known, correcting stock from that moment. stock_kg is that rule as one
-- GENERATED column so every stock read (feeddirection analytics, the low-stock alert) applies it
-- identically instead of each re-deriving the CASE.
--
-- Every row already present -- sheet history and the app rows recorded before this change --
-- is REACHED on its purchase date, exactly as it was being counted; nothing in stock or analytics
-- moves for history. Only purchases recorded from now on start as purchased. The column default
-- is 'reached' for the same reason: the sheet importer records history that arrived long ago,
-- and the app write names its own state explicitly.
ALTER TABLE public.feed_purchases
  ADD COLUMN IF NOT EXISTS delivery_status   text NOT NULL DEFAULT 'reached',
  ADD COLUMN IF NOT EXISTS reached_on        date,
  ADD COLUMN IF NOT EXISTS reached_weight_kg numeric(12, 3),
  ADD COLUMN IF NOT EXISTS reached_by        uuid;

UPDATE public.feed_purchases
SET reached_on = purchase_date
WHERE delivery_status = 'reached' AND reached_on IS NULL;

ALTER TABLE public.feed_purchases
  DROP CONSTRAINT IF EXISTS feed_purchases_delivery_status_check,
  ADD CONSTRAINT feed_purchases_delivery_status_check
    CHECK (delivery_status IN ('purchased', 'reached')),
  DROP CONSTRAINT IF EXISTS feed_purchases_reached_on_check,
  -- An in-transit load has no arrival day. A reached load with reached_on NULL is HISTORY that
  -- arrived on its purchase date (the sheet importer and every writer predating this column
  -- insert reached rows without one); readers COALESCE it to purchase_date. Requiring the day
  -- here would refuse the importer's rows outright.
  ADD CONSTRAINT feed_purchases_reached_on_check
    CHECK (delivery_status <> 'purchased' OR reached_on IS NULL),
  DROP CONSTRAINT IF EXISTS feed_purchases_reached_weight_check,
  -- A received weight is a weighbridge figure of a load that arrived: positive, and never on a
  -- load still on the road.
  ADD CONSTRAINT feed_purchases_reached_weight_check
    CHECK (reached_weight_kg IS NULL OR (reached_weight_kg > 0 AND delivery_status = 'reached'));

ALTER TABLE public.feed_purchases
  ADD COLUMN IF NOT EXISTS stock_kg numeric(12, 3)
    GENERATED ALWAYS AS (
      CASE WHEN delivery_status = 'reached' THEN COALESCE(reached_weight_kg, quantity_kg) ELSE 0 END
    ) STORED;

-- The ledger's in-transit view: "which loads have not reached yet" per tenant, newest first.
CREATE INDEX IF NOT EXISTS feed_purchases_delivery_idx
  ON public.feed_purchases (tenant_id, delivery_status, purchase_date DESC);

COMMENT ON COLUMN public.feed_purchases.delivery_status IS
  'purchased = bought and still in transit (not stock, no toxin test yet); reached = arrived on reached_on and counted as stock from that day. History predating this column is reached on its purchase date.';
COMMENT ON COLUMN public.feed_purchases.reached_on IS
  'Business date (IST) the load arrived at the farm. Set when the load is marked reached; depletes_from follows it. NULL while in transit; NULL on a reached row means it arrived on its purchase date (sheet history).';
COMMENT ON COLUMN public.feed_purchases.reached_weight_kg IS
  'Weight actually received, entered whenever it is known (deferrable). NULL means the buying weight stands in for it.';
COMMENT ON COLUMN public.feed_purchases.stock_kg IS
  'The kilograms this load contributes to stock: the received weight when entered, else the buying weight, and 0 while in transit. Every stock read uses this column.';

-- +goose Down
DROP INDEX IF EXISTS public.feed_purchases_delivery_idx;
ALTER TABLE public.feed_purchases
  DROP COLUMN IF EXISTS stock_kg,
  DROP CONSTRAINT IF EXISTS feed_purchases_reached_weight_check,
  DROP CONSTRAINT IF EXISTS feed_purchases_reached_on_check,
  DROP CONSTRAINT IF EXISTS feed_purchases_delivery_status_check,
  DROP COLUMN IF EXISTS reached_by,
  DROP COLUMN IF EXISTS reached_weight_kg,
  DROP COLUMN IF EXISTS reached_on,
  DROP COLUMN IF EXISTS delivery_status;
