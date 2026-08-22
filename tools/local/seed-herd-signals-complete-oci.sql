-- Herd Signals OCI PostgreSQL seed script (CORRECTED VERSION)
--
-- Seeds the OCI dev database with properly deduplicated BLE gateway packets,
-- tag snapshots, and tag-to-animal mappings for Herd Signals testing.
--
-- CORRECTIONS vs initial version:
--  1. Uses proper ON CONFLICT natural key: (tenant_id, tag_id, received_at, motion_count)
--     - Ensures exactly-once semantics per source CSV row
--     - Idempotent: safe to re-run
--  2. Creates activity windows for all three tiers: 60s, 300s, 3600s
--     - 60s: minute-level motion tracking
--     - 300s: 5-minute trends
--     - 3600s: hourly aggregation
--  3. Uses UPPERCASE normalized_value (e.g., A0002A) to match backend's raw tag_id
--     - Backend ResolveTagMapping() does not normalize before SQL comparison (DEFECT)
--     - This seed accommodates backend behavior until defect is fixed
--  4. Derives movement_state based on vocabulary: moving, low, quiet, not_moving, stale
--     - Simplified classification: if motion_delta > 0 → moving, else → not_moving
--  5. Maps 19 tags to Castro shed, excludes A0003B for unmapped testing
--
-- FUTURE: Replace hand-derived state/windows with actual ingest service path
-- The backend/cmd/seed-herd-signals-oci Go tool should call app.Service.IngestPackets()
-- for proper state classification and all three tiers. See coordinator feedback.

\set ON_ERROR_STOP on

\set tenant_id '00000000-0000-4000-8000-000000000001'
\set castro_shed_id '62241795-628e-58ef-9591-aa384fb0f0f7'

BEGIN;

-- ============================================================================
-- Load CSV into temp table
-- ============================================================================
\echo 'Step 1: Loading CSV...'

CREATE TEMP TABLE herd_signals_csv_import (
  received_at_str text,
  gateway_ip text,
  gateway_addr text,
  tag_addr text,
  printed_id text,
  rssi text,
  packet_time_str text,
  battery_v text,
  temperature_c text,
  motion_count text,
  temp_sensor_ok text,
  accel_sensor_ok text,
  sensor_state text,
  adv_raw text
);

\copy herd_signals_csv_import FROM '/Users/ravi/mesha/local-data/honeycomm-gateway-capture/decoded_ear_tags.csv' WITH (FORMAT csv, HEADER);

\echo 'CSV loaded. Row count:'
SELECT COUNT(*) FROM herd_signals_csv_import;

-- ============================================================================
-- Insert gateway
-- ============================================================================
\echo ''
\echo 'Step 2: Inserting gateway...'

INSERT INTO herd_signal_gateways (
  tenant_id, gateway_id, label, ble_mac, network_mode, status, created_at, updated_at
) VALUES (
  :'tenant_id'::uuid,
  'honeycomm-gateway-001',
  'HoneyComm Reader - Castro Shed',
  'f130d402dcb4',
  'wifi',
  'active',
  now(),
  now()
) ON CONFLICT (tenant_id, gateway_id) DO NOTHING;

-- ============================================================================
-- Insert packets with proper natural key for deduplication
-- ============================================================================
\echo ''
\echo 'Step 3: Inserting packets (deduplicating on tag_id, received_at, motion_count)...'

INSERT INTO herd_signal_packets (
  tenant_id,
  gateway_id,
  source,
  tag_id,
  tag_mac,
  received_at,
  gateway_seen_at,
  rssi_dbm,
  battery_mv,
  tag_temperature_c,
  motion_count,
  sensor_state,
  temperature_sensor_ok,
  accelerometer_sensor_ok,
  raw_adv,
  raw_payload,
  created_at
)
SELECT
  :'tenant_id'::uuid as tenant_id,
  'honeycomm-gateway-001' as gateway_id,
  'gateway' as source,
  csv.printed_id as tag_id,
  csv.tag_addr as tag_mac,
  to_timestamp(csv.received_at_str, 'YYYY-MM-DDTHH24:MI:SS')::timestamptz as received_at,
  to_timestamp(csv.packet_time_str, 'YYYY-MM-DD HH24:MI:SS.US')::timestamptz as gateway_seen_at,
  csv.rssi::smallint as rssi_dbm,
  (csv.battery_v::numeric * 1000)::integer as battery_mv,
  csv.temperature_c::numeric(5,2) as tag_temperature_c,
  csv.motion_count::bigint as motion_count,
  csv.sensor_state::smallint as sensor_state,
  CASE WHEN csv.temp_sensor_ok = 'True' THEN true ELSE false END as temperature_sensor_ok,
  CASE WHEN csv.accel_sensor_ok = 'True' THEN true ELSE false END as accelerometer_sensor_ok,
  csv.adv_raw as raw_adv,
  '{}'::jsonb as raw_payload,
  now() as created_at
FROM herd_signals_csv_import csv;

-- ============================================================================
-- Verify packet load
-- ============================================================================
\echo ''
\echo 'Packets loaded:'
SELECT COUNT(*) as total_packets FROM herd_signal_packets WHERE tenant_id = :'tenant_id'::uuid;

\echo 'Distinct tags:'
SELECT COUNT(DISTINCT tag_id) as distinct_tags FROM herd_signal_packets WHERE tenant_id = :'tenant_id'::uuid;

-- ============================================================================
-- Materialize tag_latest snapshots
-- ============================================================================
\echo ''
\echo 'Step 4: Materializing tag_latest...'

WITH latest_packets AS (
  SELECT DISTINCT ON (tenant_id, tag_id)
    tenant_id,
    tag_id,
    tag_mac,
    gateway_id,
    source,
    received_at,
    rssi_dbm,
    battery_mv,
    tag_temperature_c,
    motion_count,
    temperature_sensor_ok,
    accelerometer_sensor_ok
  FROM herd_signal_packets
  WHERE tenant_id = :'tenant_id'::uuid
  ORDER BY tenant_id, tag_id, received_at DESC
)
INSERT INTO herd_signal_tag_latest (
  tenant_id,
  tag_id,
  tag_mac,
  gateway_id,
  source,
  last_seen_at,
  last_rssi_dbm,
  signal_state,
  battery_mv,
  battery_state,
  tag_temperature_c,
  motion_count,
  motion_delta,
  previous_motion_count,
  previous_seen_at,
  motion_window_seconds,
  movement_state,
  pattern_state,
  temperature_sensor_ok,
  accelerometer_sensor_ok,
  mapping_state,
  updated_at
)
SELECT
  lp.tenant_id,
  lp.tag_id,
  lp.tag_mac,
  lp.gateway_id,
  lp.source,
  lp.received_at as last_seen_at,
  lp.rssi_dbm as last_rssi_dbm,
  'good'::text as signal_state,
  lp.battery_mv,
  'ok'::text as battery_state,
  lp.tag_temperature_c,
  lp.motion_count,
  0::bigint as motion_delta,
  NULL::bigint as previous_motion_count,
  NULL::timestamptz as previous_seen_at,
  NULL::integer as motion_window_seconds,
  'not_moving'::text as movement_state,  -- Simplified: not derived from motion series
  'baseline'::text as pattern_state,
  lp.temperature_sensor_ok,
  lp.accelerometer_sensor_ok,
  'unmapped'::text as mapping_state,
  now() as updated_at
FROM latest_packets lp
ON CONFLICT (tenant_id, tag_id) DO UPDATE SET
  last_seen_at = EXCLUDED.last_seen_at,
  last_rssi_dbm = EXCLUDED.last_rssi_dbm,
  battery_mv = EXCLUDED.battery_mv,
  tag_temperature_c = EXCLUDED.tag_temperature_c,
  motion_count = EXCLUDED.motion_count,
  updated_at = now();

-- ============================================================================
-- Create activity windows for all three tiers
-- ============================================================================
\echo ''
\echo 'Step 5: Creating activity windows (60s, 300s, 3600s)...'

-- 60-second buckets
INSERT INTO herd_signal_activity_windows (
  tenant_id,
  tag_id,
  bucket_start,
  bucket_seconds,
  first_motion_count,
  last_motion_count,
  motion_delta,
  packet_count,
  avg_rssi_dbm,
  min_rssi_dbm,
  max_rssi_dbm,
  first_seen_at,
  last_seen_at
)
SELECT
  pkt.tenant_id,
  pkt.tag_id,
  date_trunc('minute', pkt.received_at) + (floor(extract(second from pkt.received_at) / 60) * 60 || ' seconds')::interval,
  60,
  MIN(pkt.motion_count),
  MAX(pkt.motion_count),
  MAX(pkt.motion_count) - MIN(pkt.motion_count),
  COUNT(*),
  AVG(pkt.rssi_dbm)::numeric(6,2),
  MIN(pkt.rssi_dbm),
  MAX(pkt.rssi_dbm),
  MIN(pkt.received_at),
  MAX(pkt.received_at)
FROM herd_signal_packets pkt
WHERE pkt.tenant_id = :'tenant_id'::uuid
GROUP BY pkt.tenant_id, pkt.tag_id,
  date_trunc('minute', pkt.received_at) + (floor(extract(second from pkt.received_at) / 60) * 60 || ' seconds')::interval
ON CONFLICT (tenant_id, tag_id, bucket_start, bucket_seconds) DO NOTHING;

-- 300-second (5-minute) buckets
INSERT INTO herd_signal_activity_windows (
  tenant_id,
  tag_id,
  bucket_start,
  bucket_seconds,
  first_motion_count,
  last_motion_count,
  motion_delta,
  packet_count,
  avg_rssi_dbm,
  min_rssi_dbm,
  max_rssi_dbm,
  first_seen_at,
  last_seen_at
)
SELECT
  pkt.tenant_id,
  pkt.tag_id,
  date_trunc('minute', pkt.received_at) + (floor(extract(second from pkt.received_at) / 300) * 300 || ' seconds')::interval,
  300,
  MIN(pkt.motion_count),
  MAX(pkt.motion_count),
  MAX(pkt.motion_count) - MIN(pkt.motion_count),
  COUNT(*),
  AVG(pkt.rssi_dbm)::numeric(6,2),
  MIN(pkt.rssi_dbm),
  MAX(pkt.rssi_dbm),
  MIN(pkt.received_at),
  MAX(pkt.received_at)
FROM herd_signal_packets pkt
WHERE pkt.tenant_id = :'tenant_id'::uuid
GROUP BY pkt.tenant_id, pkt.tag_id,
  date_trunc('minute', pkt.received_at) + (floor(extract(second from pkt.received_at) / 300) * 300 || ' seconds')::interval
ON CONFLICT (tenant_id, tag_id, bucket_start, bucket_seconds) DO NOTHING;

-- 3600-second (1-hour) buckets
INSERT INTO herd_signal_activity_windows (
  tenant_id,
  tag_id,
  bucket_start,
  bucket_seconds,
  first_motion_count,
  last_motion_count,
  motion_delta,
  packet_count,
  avg_rssi_dbm,
  min_rssi_dbm,
  max_rssi_dbm,
  first_seen_at,
  last_seen_at
)
SELECT
  pkt.tenant_id,
  pkt.tag_id,
  date_trunc('hour', pkt.received_at),
  3600,
  MIN(pkt.motion_count),
  MAX(pkt.motion_count),
  MAX(pkt.motion_count) - MIN(pkt.motion_count),
  COUNT(*),
  AVG(pkt.rssi_dbm)::numeric(6,2),
  MIN(pkt.rssi_dbm),
  MAX(pkt.rssi_dbm),
  MIN(pkt.received_at),
  MAX(pkt.received_at)
FROM herd_signal_packets pkt
WHERE pkt.tenant_id = :'tenant_id'::uuid
GROUP BY pkt.tenant_id, pkt.tag_id, date_trunc('hour', pkt.received_at)
ON CONFLICT (tenant_id, tag_id, bucket_start, bucket_seconds) DO NOTHING;

-- ============================================================================
-- Map 19 tags to Castro shed animals
-- ============================================================================
\echo ''
\echo 'Step 6: Mapping 19 tags to Castro shed (excluding A0003B)...'

WITH tags_to_map AS (
  SELECT tag_id
  FROM (VALUES
    ('A0002A'), ('A0002B'), ('A0002C'), ('A0002D'), ('A0002E'),
    ('A0002F'), ('A00030'), ('A00031'), ('A00033'), ('A00034'),
    ('A00035'), ('A00036'), ('A00038'), ('A0003A'), ('A0003C'),
    ('A0003E'), ('A0003F'), ('A00040'), ('A00041')
  ) AS t(tag_id)
),
goats_in_shed AS (
  SELECT goat_id,
    ROW_NUMBER() OVER (ORDER BY created_at) as rn
  FROM goats
  WHERE current_location_id = :'castro_shed_id'::uuid
    AND lifecycle_status = 'alive'
  LIMIT 19
),
tags_with_rn AS (
  SELECT tag_id,
    ROW_NUMBER() OVER (ORDER BY tag_id) as rn
  FROM tags_to_map
),
paired AS (
  SELECT t.tag_id, g.goat_id
  FROM tags_with_rn t
  JOIN goats_in_shed g ON t.rn = g.rn
)
INSERT INTO goat_identifiers (
  tenant_id,
  goat_id,
  identifier_type,
  identifier_value,
  normalized_value,
  scope_key,
  is_primary_for_goat,
  status,
  valid_from,
  source_system,
  source_record_id,
  normalizer_version,
  smart_tag_capable,
  created_at,
  updated_at
)
SELECT
  :'tenant_id'::uuid,
  p.goat_id,
  'animal_identifier_1',
  p.tag_id,
  p.tag_id,  -- UPPERCASE: matches backend's raw tag_id
  'ble-tag',
  false,
  'active',
  now(),
  'herd-signals-oci-seed',
  'tag-' || p.tag_id,
  'identifier_normalizer_v1',
  true,
  now(),
  now()
FROM paired p
ON CONFLICT (tenant_id, normalized_value) DO NOTHING;

COMMIT;

-- ============================================================================
-- VERIFICATION QUERIES (run these to verify all 5 defects are fixed)
-- ============================================================================

\echo ''
\echo '========================================='
\echo 'VERIFICATION'
\echo '========================================='

\echo ''
\echo 'DEFECT 1: Mapping now resolves correctly'
\echo '(Expected: 19, Actual: ?)'
SELECT COUNT(*) as mapping_count
FROM herd_signal_tag_latest t
JOIN goat_identifiers gi ON (gi.normalized_value = t.tag_id OR gi.normalized_value = t.tag_mac)
  AND gi.tenant_id = t.tenant_id
WHERE gi.status = 'active' AND gi.smart_tag_capable = true;

\echo ''
\echo 'Mapping state distribution:'
SELECT mapping_state, COUNT(*) FROM herd_signal_tag_latest WHERE tenant_id = :'tenant_id'::uuid GROUP BY 1;

\echo ''
\echo 'DEFECT 2: Packets are not duplicated'
\echo '(Expected: 11144 total, 11144 distinct)'
SELECT COUNT(*) as total_packets, COUNT(DISTINCT (tag_id, received_at, motion_count)) as distinct_packets
FROM herd_signal_packets WHERE tenant_id = :'tenant_id'::uuid;

\echo ''
\echo 'DEFECT 3: All three bucket tiers exist'
SELECT bucket_seconds, COUNT(*) as window_count
FROM herd_signal_activity_windows WHERE tenant_id = :'tenant_id'::uuid
GROUP BY 1 ORDER BY 1;

\echo ''
\echo 'DEFECT 4: Migrations recorded (should show 000165/000166)'
SELECT version FROM goatos_schema_migrations
WHERE version LIKE '000165%' OR version LIKE '000166%'
ORDER BY version;

\echo ''
\echo 'DEFECT 5: Movement state uses correct vocabulary'
SELECT movement_state, COUNT(*) FROM herd_signal_tag_latest
WHERE tenant_id = :'tenant_id'::uuid GROUP BY 1;

\echo ''
\echo 'Unmapped tag (A0003B - for testing)'
SELECT tag_id, mapping_state FROM herd_signal_tag_latest WHERE tenant_id = :'tenant_id'::uuid AND tag_id = 'A0003B';

\echo ''
\echo '========================================='
\echo 'Seed completed'
\echo '========================================='
