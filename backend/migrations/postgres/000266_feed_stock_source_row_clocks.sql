-- +goose Up
-- +goose NO TRANSACTION
-- Stock analytics may cache only when every stock-driving source has a row clock.
-- These are additive metadata columns: no business data is rewritten.
ALTER TABLE public.feed_purchases
  ADD COLUMN IF NOT EXISTS updated_at timestamptz NOT NULL DEFAULT now();

ALTER TABLE public.feed_external_consumption
  ADD COLUMN IF NOT EXISTS updated_at timestamptz NOT NULL DEFAULT now();

CREATE INDEX CONCURRENTLY IF NOT EXISTS feed_purchases_stock_revision_idx
  ON public.feed_purchases (tenant_id, park_id, updated_at);

CREATE INDEX CONCURRENTLY IF NOT EXISTS feed_external_consumption_stock_revision_idx
  ON public.feed_external_consumption (tenant_id, park_id, updated_at);

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS public.feed_external_consumption_stock_revision_idx;
DROP INDEX CONCURRENTLY IF EXISTS public.feed_purchases_stock_revision_idx;

ALTER TABLE public.feed_external_consumption
  DROP COLUMN IF EXISTS updated_at;

ALTER TABLE public.feed_purchases
  DROP COLUMN IF EXISTS updated_at;
