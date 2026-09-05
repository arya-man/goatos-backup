-- +goose Up
-- +goose NO TRANSACTION
-- Feed analytics filter external consumption by tenant, park and feed_day.
-- The older indexes are item/date oriented; these are read-path indexes only.
CREATE INDEX CONCURRENTLY IF NOT EXISTS feed_external_consumption_park_day_idx
  ON public.feed_external_consumption (tenant_id, park_id, feed_day, feed_item_key)
  INCLUDE (feed_item_label, quantity_kg);

CREATE INDEX CONCURRENTLY IF NOT EXISTS milk_preparation_completions_feeding_serving_idx
  ON public.milk_preparation_completions (tenant_id, feeding_date, park_id, completion_id)
  INCLUDE (preparation_date, current_attempt_no)
  WHERE status <> 'retired' AND park_id IS NOT NULL;

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS public.milk_preparation_completions_feeding_serving_idx;
DROP INDEX CONCURRENTLY IF EXISTS public.feed_external_consumption_park_day_idx;
