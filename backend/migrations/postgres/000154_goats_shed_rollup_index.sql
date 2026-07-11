-- +goose Up
-- +goose NO TRANSACTION
-- Backs the shed-wise vaccination rollup's alive-animal-per-shed aggregate (shedSummarySQL `alive` CTE:
-- tenant_id + lifecycle_status = 'alive' + GROUP BY shed_id). The partial predicate matches the query's
-- `merged_into_goat_id IS NULL` so the planner can use this index; built CONCURRENTLY so it never blocks
-- writes on the (large) goats table.
CREATE INDEX CONCURRENTLY IF NOT EXISTS goats_tenant_lifecycle_shed_idx
  ON goats(tenant_id, lifecycle_status, shed_id)
  WHERE merged_into_goat_id IS NULL;

-- +goose Down
-- +goose NO TRANSACTION
DROP INDEX CONCURRENTLY IF EXISTS goats_tenant_lifecycle_shed_idx;
