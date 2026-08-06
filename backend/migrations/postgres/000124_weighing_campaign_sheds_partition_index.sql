-- +goose Up
-- +goose NO TRANSACTION
-- Partition-aware read index for weighing campaign sheds, split out of 000121 so it can be built
-- CONCURRENTLY. weighing_campaign_sheds is a populated operational table with live campaign
-- writes; a plain CREATE INDEX holds a write lock for the whole build and stalls those writes
-- during deploy. CREATE INDEX CONCURRENTLY cannot run inside a transaction, hence the separate
-- NO TRANSACTION migration.
CREATE INDEX CONCURRENTLY IF NOT EXISTS weighing_campaign_sheds_campaign_partition_idx
  ON weighing_campaign_sheds (tenant_id, campaign_id, location_id, COALESCE(partition_label, ''));

-- +goose Down
-- +goose NO TRANSACTION
DROP INDEX CONCURRENTLY IF EXISTS weighing_campaign_sheds_campaign_partition_idx;
