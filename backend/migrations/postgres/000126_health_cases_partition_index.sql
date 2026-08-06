-- +goose Up
-- +goose NO TRANSACTION
-- Partition-aware read index for health_cases, split out of 000123 for the same reason 000125 is
-- split out of 000121: goose applies "-- +goose NO TRANSACTION" to the ENTIRE migration, so a
-- CREATE INDEX CONCURRENTLY sitting after transactional statements runs inside that transaction
-- and fails with:
--   ERROR: CREATE INDEX CONCURRENTLY cannot run inside a transaction block
-- health_cases is a live clinical table, so a blocking plain CREATE INDEX is not an alternative.
CREATE INDEX CONCURRENTLY IF NOT EXISTS health_cases_shed_partition_idx
    ON public.health_cases (tenant_id, shed_id, partition_label)
    WHERE shed_id IS NOT NULL;


-- +goose Down
-- +goose NO TRANSACTION
DROP INDEX CONCURRENTLY IF EXISTS public.health_cases_shed_partition_idx;
