-- +goose Up
CREATE INDEX goat_identity_events_tenant_recorded_at_idx
  ON goat_identity_events(tenant_id, recorded_at DESC);

-- +goose Down
DROP INDEX IF EXISTS goat_identity_events_tenant_recorded_at_idx;
