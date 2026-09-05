-- +goose Up
-- +goose NO TRANSACTION
-- Speeds Feed Analytics execution/stock read models that repeatedly collapse
-- feed_direction_issue_rows at issue-row grain. This is a serving index only.
CREATE INDEX CONCURRENTLY IF NOT EXISTS feed_direction_issue_rows_quantity_serving_idx
  ON public.feed_direction_issue_rows (
    tenant_id,
    feed_direction_issue_id,
    shed_id,
    partition_key,
    session_no,
    workflow,
    feed_item_key
  )
  INCLUDE (quantity_kg, feed_item_label)
  WHERE quantity_kg IS NOT NULL;

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS public.feed_direction_issue_rows_quantity_serving_idx;
