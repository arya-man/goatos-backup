-- +goose Up
CREATE INDEX IF NOT EXISTS goat_location_history_tenant_from_location_idx
  ON goat_location_history (tenant_id, from_location_id, occurred_at DESC)
  WHERE from_location_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS goat_location_history_tenant_to_location_idx
  ON goat_location_history (tenant_id, to_location_id, occurred_at DESC);

-- +goose Down
DROP INDEX IF EXISTS goat_location_history_tenant_to_location_idx;
DROP INDEX IF EXISTS goat_location_history_tenant_from_location_idx;
