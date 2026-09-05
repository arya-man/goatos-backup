-- +goose Up
-- +goose NO TRANSACTION
-- Feed stock analytics prices every (day, park, item) bucket from the latest reached load whose
-- depletes_from is on or before that feed day. The generic delivery index only serves the in-transit
-- list; this partial index serves the lateral "latest stock price" lookup without adding a
-- projection table or changing any stock facts.
CREATE INDEX CONCURRENTLY IF NOT EXISTS feed_purchases_reached_stock_price_idx
  ON public.feed_purchases (
    tenant_id,
    park_id,
    feed_item_key,
    depletes_from DESC,
    purchase_date DESC,
    batch_no DESC
  )
  INCLUDE (per_kg_cost, total_cost, quantity_kg, stock_kg)
  WHERE delivery_status = 'reached';

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS public.feed_purchases_reached_stock_price_idx;
