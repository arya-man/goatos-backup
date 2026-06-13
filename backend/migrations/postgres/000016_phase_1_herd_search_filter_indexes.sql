-- +goose Up
-- +goose NO TRANSACTION
CREATE INDEX CONCURRENTLY IF NOT EXISTS goats_tenant_breed_display_idx
  ON goats(tenant_id, breed, display_id)
  WHERE identity_state <> 'merged';

CREATE INDEX CONCURRENTLY IF NOT EXISTS goats_tenant_breed_sex_display_idx
  ON goats(tenant_id, breed, sex, display_id)
  WHERE identity_state <> 'merged';

CREATE INDEX CONCURRENTLY IF NOT EXISTS goats_tenant_sex_display_idx
  ON goats(tenant_id, sex, display_id)
  WHERE identity_state <> 'merged';

CREATE INDEX CONCURRENTLY IF NOT EXISTS goats_tenant_lifecycle_display_idx
  ON goats(tenant_id, lifecycle_status, display_id)
  WHERE identity_state <> 'merged';

-- +goose Down
-- +goose NO TRANSACTION
DROP INDEX CONCURRENTLY IF EXISTS goats_tenant_lifecycle_display_idx;
DROP INDEX CONCURRENTLY IF EXISTS goats_tenant_sex_display_idx;
DROP INDEX CONCURRENTLY IF EXISTS goats_tenant_breed_sex_display_idx;
DROP INDEX CONCURRENTLY IF EXISTS goats_tenant_breed_display_idx;
