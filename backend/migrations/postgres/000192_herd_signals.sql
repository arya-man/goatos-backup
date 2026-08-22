-- Herd Signals BLE ear-tag telemetry backend.
--
-- Ingests live-streaming position, motion, battery, temperature sensor readings
-- from gateway-deployed HoneyComm BLE readers. Provides a shared tag-inventory
-- and signal-quality baseline for a future mobile/admin-web Herd Location
-- dashboard and health/clinical monitoring context.
--
-- Tables:
--   herd_signal_gateways: gateway registration, location, status, last-seen
--   herd_signal_packets: raw inbound BLE advertisement packets (immutable, write-once)
--   herd_signal_tag_latest: materialized per-tag snapshot (motion state, signal, battery)
--   herd_signal_activity_windows: per-tag motion aggregates over time windows (60s buckets)

-- +goose Up

-- Gateways: registration, location, network mode, last heartbeat.
-- Not indexed yet on network_mode, location_id alone (consider if herd-signals
-- becomes a live inventory dashboard). CONCURRENTLY not allowed inside a tx.
CREATE TABLE IF NOT EXISTS public.herd_signal_gateways (
  tenant_id uuid NOT NULL,
  gateway_id text NOT NULL,
  label text,
  park_id uuid,
  shed_id uuid,
  location_id uuid,
  wifi_mac text,
  ble_mac text,
  network_mode text,
  status text NOT NULL DEFAULT 'active',
  last_seen_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, gateway_id)
);

CREATE INDEX IF NOT EXISTS herd_signal_gateways_status_idx
  ON public.herd_signal_gateways (tenant_id, status);
CREATE INDEX IF NOT EXISTS herd_signal_gateways_location_idx
  ON public.herd_signal_gateways (tenant_id, location_id);
CREATE INDEX IF NOT EXISTS herd_signal_gateways_last_seen_idx
  ON public.herd_signal_gateways (tenant_id, last_seen_at DESC);

-- Raw BLE advertisement packets from gateways. Write-once, immutable.
-- No PK; packet_id is the unique identity but rows are append-only.
CREATE TABLE IF NOT EXISTS public.herd_signal_packets (
  packet_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  gateway_id text,
  source text NOT NULL DEFAULT 'gateway',
  tag_id text NOT NULL,
  tag_mac text,
  received_at timestamptz NOT NULL,
  gateway_seen_at timestamptz,
  rssi_dbm smallint,
  battery_mv integer,
  tag_temperature_c numeric(5,2),
  motion_count bigint,
  sensor_state smallint,
  temperature_sensor_ok boolean,
  accelerometer_sensor_ok boolean,
  raw_adv text,
  raw_payload jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS herd_signal_packets_tenant_tag_received_idx
  ON public.herd_signal_packets (tenant_id, tag_id, received_at DESC);
CREATE INDEX IF NOT EXISTS herd_signal_packets_tenant_gateway_received_idx
  ON public.herd_signal_packets (tenant_id, gateway_id, received_at DESC);
CREATE INDEX IF NOT EXISTS herd_signal_packets_tenant_received_idx
  ON public.herd_signal_packets (tenant_id, received_at DESC);

-- Per-tag snapshot: latest position, signal quality, motion state, battery.
-- Materialized from herd_signal_packets on ingest and on-read (keyset pagination).
-- PK ensures one row per tag; motion_delta and previous motion state enable delta
-- computation without per-tag state machines.
CREATE TABLE IF NOT EXISTS public.herd_signal_tag_latest (
  tenant_id uuid NOT NULL,
  tag_id text NOT NULL,
  tag_mac text,
  gateway_id text,
  source text,
  last_seen_at timestamptz NOT NULL,
  last_rssi_dbm smallint,
  signal_state text NOT NULL DEFAULT 'unknown',
  battery_mv integer,
  battery_state text NOT NULL DEFAULT 'unknown',
  tag_temperature_c numeric(5,2),
  motion_count bigint,
  motion_delta bigint,
  previous_motion_count bigint,
  previous_seen_at timestamptz,
  motion_window_seconds integer,
  movement_state text NOT NULL DEFAULT 'unknown',
  pattern_state text NOT NULL DEFAULT 'unknown',
  temperature_sensor_ok boolean,
  accelerometer_sensor_ok boolean,
  mapping_state text NOT NULL DEFAULT 'unmapped',
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, tag_id)
);

CREATE INDEX IF NOT EXISTS herd_signal_tag_latest_last_seen_idx
  ON public.herd_signal_tag_latest (tenant_id, last_seen_at DESC);
CREATE INDEX IF NOT EXISTS herd_signal_tag_latest_movement_idx
  ON public.herd_signal_tag_latest (tenant_id, movement_state, last_seen_at DESC);
CREATE INDEX IF NOT EXISTS herd_signal_tag_latest_mapping_idx
  ON public.herd_signal_tag_latest (tenant_id, mapping_state, last_seen_at DESC);

-- Per-tag activity windows: bucketed motion aggregates (default 60s buckets).
-- Supports timeline queries: "show me this tag's motion over the past 24 hours in 5-minute buckets".
-- Grain: (tenant_id, tag_id, bucket_start, bucket_seconds).
CREATE TABLE IF NOT EXISTS public.herd_signal_activity_windows (
  tenant_id uuid NOT NULL,
  tag_id text NOT NULL,
  bucket_start timestamptz NOT NULL,
  bucket_seconds integer NOT NULL DEFAULT 60,
  first_motion_count bigint,
  last_motion_count bigint,
  motion_delta bigint NOT NULL DEFAULT 0,
  packet_count integer NOT NULL DEFAULT 0,
  avg_rssi_dbm numeric(6,2),
  min_rssi_dbm smallint,
  max_rssi_dbm smallint,
  first_seen_at timestamptz,
  last_seen_at timestamptz,
  PRIMARY KEY (tenant_id, tag_id, bucket_start, bucket_seconds)
);

CREATE INDEX IF NOT EXISTS herd_signal_activity_windows_bucket_idx
  ON public.herd_signal_activity_windows (tenant_id, bucket_start DESC);
CREATE INDEX IF NOT EXISTS herd_signal_activity_windows_tag_idx
  ON public.herd_signal_activity_windows (tenant_id, tag_id, bucket_start DESC);

-- +goose Down

DROP TABLE IF EXISTS public.herd_signal_activity_windows;
DROP TABLE IF EXISTS public.herd_signal_tag_latest;
DROP TABLE IF EXISTS public.herd_signal_packets;
DROP TABLE IF EXISTS public.herd_signal_gateways;
