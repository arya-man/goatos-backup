-- +goose Up
-- +goose NO TRANSACTION
-- Directed feed analytics collapses issue rows by day, item and pen grain.
-- The execution serving index omits head_count/shed_tag_key/breed_key, so this
-- keeps the directed rollup index-only without introducing projection tables.
CREATE INDEX CONCURRENTLY IF NOT EXISTS feed_direction_issue_rows_directed_serving_idx
  ON public.feed_direction_issue_rows (
    tenant_id,
    feed_direction_issue_id,
    feed_item_key,
    shed_id,
    partition_key,
    shed_tag_key,
    breed_key
  )
  INCLUDE (quantity_kg, head_count, feed_item_label)
  WHERE quantity_kg IS NOT NULL;

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS public.feed_direction_issue_rows_directed_serving_idx;
