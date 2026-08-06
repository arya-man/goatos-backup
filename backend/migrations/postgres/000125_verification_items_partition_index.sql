-- +goose Up
-- +goose NO TRANSACTION
-- Partition-aware read index for verification_items, split out of 000121 so it can be built
-- CONCURRENTLY. goose applies "-- +goose NO TRANSACTION" to the ENTIRE migration, not to the
-- statement it precedes, so a concurrent index sitting after a transactional ALTER + backfill
-- runs inside that transaction and fails with:
--   ERROR: CREATE INDEX CONCURRENTLY cannot run inside a transaction block
-- verification_items is a live operational table, so a plain CREATE INDEX is not an option
-- either: it holds a write lock for the whole build and stalls proof capture during deploy.
--
-- Supports the filter/query paths that group by (shed_id, partition_label) -- see
-- ListQueue/ListQueueFilterOptions in backend/internal/verification/adapters/postgres.
CREATE INDEX CONCURRENTLY IF NOT EXISTS verification_items_shed_partition_idx
    ON public.verification_items (tenant_id, shed_id, partition_label)
    WHERE shed_id IS NOT NULL;

-- +goose Down
-- +goose NO TRANSACTION
DROP INDEX CONCURRENTLY IF EXISTS public.verification_items_shed_partition_idx;
