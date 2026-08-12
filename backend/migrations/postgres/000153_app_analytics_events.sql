CREATE SCHEMA IF NOT EXISTS analytics;

CREATE TABLE IF NOT EXISTS analytics.app_events (
  event_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NULL,
  actor_id uuid NULL,
  device_id text NOT NULL DEFAULT '',
  event_name text NOT NULL,
  properties jsonb NOT NULL DEFAULT '{}'::jsonb,
  client_event_time timestamptz NULL,
  received_at timestamptz NOT NULL DEFAULT now(),
  flavor text NOT NULL DEFAULT '',
  app_version_name text NOT NULL DEFAULT '',
  app_version_code integer NULL,
  request_id text NOT NULL DEFAULT '',
  trace_id text NOT NULL DEFAULT '',
  client_info jsonb NOT NULL DEFAULT '{}'::jsonb,
  CONSTRAINT app_events_event_name_present CHECK (btrim(event_name) <> '')
);

CREATE INDEX IF NOT EXISTS app_events_received_at_idx
  ON analytics.app_events (received_at DESC);

CREATE INDEX IF NOT EXISTS app_events_tenant_event_received_idx
  ON analytics.app_events (tenant_id, event_name, received_at DESC);

CREATE INDEX IF NOT EXISTS app_events_device_received_idx
  ON analytics.app_events (device_id, received_at DESC);
