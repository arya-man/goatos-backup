-- +goose Up
ALTER TABLE analytics.app_events ADD COLUMN IF NOT EXISTS client_event_id text;

CREATE UNIQUE INDEX IF NOT EXISTS app_events_tenant_client_event_id_idx
  ON analytics.app_events (tenant_id, client_event_id)
  WHERE client_event_id IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS app_events_tenant_client_event_id_idx;
ALTER TABLE analytics.app_events DROP COLUMN IF EXISTS client_event_id;
