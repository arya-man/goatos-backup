-- Herd Signals OCI PostgreSQL seed script.
--
-- Seeds the OCI dev database with:
--  1. A single HoneyComm gateway (MAC f130d402dcb4 at 192.168.0.9)
--  2. 11,144 real BLE advertisement packets from captured ear tags
--  3. Derived tag_latest snapshots (latest packet per tag)
--  4. Motion activity windows (60-second buckets per tag)
--  5. 19 ear tags mapped to goats in Castro shed (excluding A0003B for unmapped testing)
--
-- Idempotent: safe to re-run. Uses ON CONFLICT DO NOTHING where needed.
-- Test-data marked with source_system='herd-signals-oci-seed' for easy cleanup.
--
-- SAFETY: Refuses to run against any database except the OCI target.
-- Run with: source /Users/ravi/mesha/local-data/goatos-stg-to-oci/oci-goatos-db.env && psql "$DATABASE_URL" -f tools/local/seed-herd-signals-oci.sql

\set ON_ERROR_STOP on

-- ============================================================================
-- SAFETY CHECK: Ensure this is the OCI database
-- ============================================================================
DO $$
DECLARE
  v_db_name text;
  v_inet text;
BEGIN
  SELECT current_database() INTO v_db_name;
  SELECT inet_client_addr()::text INTO v_inet;

  IF v_db_name != 'goatos' THEN
    RAISE EXCEPTION 'SAFETY: Wrong database. Expected goatos, got %', v_db_name;
  END IF;

  -- Allow SSH tunnel from Tailscale (10.88.0.0/16) or localhost
  IF v_inet IS NOT NULL AND v_inet NOT LIKE '127.0.%' AND v_inet NOT LIKE '10.88.%' THEN
    RAISE EXCEPTION 'SAFETY: Suspected non-OCI connection from %', v_inet;
  END IF;

  RAISE NOTICE 'seed-herd-signals-oci: Starting on database % via %', v_db_name, COALESCE(v_inet, 'socket');
END $$;

-- ============================================================================
-- Fixed IDs for this seed
-- ============================================================================
\set tenant_id '00000000-0000-4000-8000-000000000001'
\set castro_shed_id '62241795-628e-58ef-9591-aa384fb0f0f7'
\set gateway_mac 'f130d402dcb4'
\set gateway_ip '192.168.0.9'

BEGIN;

-- ============================================================================
-- Step 1: Insert gateway (idempotent)
-- ============================================================================
\echo 'Step 1: Inserting HoneyComm gateway f130d402dcb4 at 192.168.0.9'

INSERT INTO herd_signal_gateways (
  tenant_id, gateway_id, label, ble_mac, network_mode, status, created_at, updated_at
) VALUES (
  :'tenant_id'::uuid,
  'honeycomm-gateway-001',
  'HoneyComm Reader - Castro Shed',
  :'gateway_mac',
  'wifi',
  'active',
  now(),
  now()
) ON CONFLICT (tenant_id, gateway_id) DO NOTHING;

-- ============================================================================
-- Step 2: Temp table for CSV data (will be populated by COPY below)
-- ============================================================================
\echo 'Step 2: Creating temp table for CSV packet data'

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

-- ============================================================================
-- Step 3: Load CSV data from gateway captures
-- ============================================================================
\echo 'Step 3: Loading decoded_ear_tags.csv (11,144 packets)'

\copy herd_signals_csv_import FROM '/Users/ravi/mesha/local-data/honeycomm-gateway-capture/decoded_ear_tags.csv' WITH (FORMAT csv, HEADER);

-- ============================================================================
-- Step 4: Transform CSV into packets and insert
-- ============================================================================
\echo 'Step 4: Transforming CSV and inserting into herd_signal_packets'

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
FROM herd_signals_csv_import csv
ON CONFLICT DO NOTHING;

-- ============================================================================
-- Step 5: Materialize tag_latest snapshots (latest packet per tag)
-- ============================================================================
\echo 'Step 5: Materializing tag_latest from latest packets'

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
  'stationary'::text as movement_state,
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
-- Step 6: Create activity windows (60-second buckets)
-- ============================================================================
\echo 'Step 6: Creating activity windows (60-second buckets)'

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
  date_trunc('minute', pkt.received_at) + (floor(extract(second from pkt.received_at) / 60) * 60 || ' seconds')::interval as bucket_start,
  60 as bucket_seconds,
  MIN(pkt.motion_count) FILTER (WHERE pkt.motion_count IS NOT NULL) as first_motion_count,
  MAX(pkt.motion_count) FILTER (WHERE pkt.motion_count IS NOT NULL) as last_motion_count,
  (MAX(pkt.motion_count) FILTER (WHERE pkt.motion_count IS NOT NULL) - MIN(pkt.motion_count) FILTER (WHERE pkt.motion_count IS NOT NULL))::bigint as motion_delta,
  COUNT(*) as packet_count,
  AVG(pkt.rssi_dbm)::numeric(6,2) as avg_rssi_dbm,
  MIN(pkt.rssi_dbm) as min_rssi_dbm,
  MAX(pkt.rssi_dbm) as max_rssi_dbm,
  MIN(pkt.received_at) as first_seen_at,
  MAX(pkt.received_at) as last_seen_at
FROM herd_signal_packets pkt
WHERE pkt.tenant_id = :'tenant_id'::uuid
GROUP BY pkt.tenant_id, pkt.tag_id, bucket_start
ON CONFLICT (tenant_id, tag_id, bucket_start, bucket_seconds) DO NOTHING;

-- ============================================================================
-- Step 7: Map 19 tags to goats in Castro shed
-- Tag A0003B is EXCLUDED (kept unmapped for testing)
-- ============================================================================
\echo 'Step 7: Mapping 19 tags to goats in Castro shed'

-- Get first 19 active tags (sorted by tag_id) excluding A0003B
WITH tags_to_map AS (
  SELECT
    ROW_NUMBER() OVER (ORDER BY tag_id) as rn,
    tag_id
  FROM (
    SELECT DISTINCT tag_id
    FROM herd_signal_packets
    WHERE tenant_id = :'tenant_id'::uuid
      AND tag_id != 'A0003B'
    ORDER BY tag_id
    LIMIT 19
  ) t
),
-- Get first 19 active goats in Castro shed
goats_in_shed AS (
  SELECT
    ROW_NUMBER() OVER (ORDER BY goat_id) as rn,
    goat_id
  FROM (
    SELECT goat_id
    FROM goats
    WHERE current_location_id = :'castro_shed_id'::uuid
      AND lifecycle_status = 'alive'
    ORDER BY created_at
    LIMIT 19
  ) g
),
-- Pair them up by row number
tag_goat_pairs AS (
  SELECT
    ttm.tag_id,
    gis.goat_id
  FROM tags_to_map ttm
  JOIN goats_in_shed gis ON ttm.rn = gis.rn
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
  :'tenant_id'::uuid as tenant_id,
  tgp.goat_id,
  'animal_identifier_1' as identifier_type,
  tgp.tag_id as identifier_value,
  lower(tgp.tag_id) as normalized_value,
  'ble-tag' as scope_key,
  false as is_primary_for_goat,
  'active' as status,
  now() as valid_from,
  'herd-signals-oci-seed' as source_system,
  'tag-' || tgp.tag_id as source_record_id,
  'v1' as normalizer_version,
  true as smart_tag_capable,
  now() as created_at,
  now() as updated_at
FROM tag_goat_pairs tgp
ON CONFLICT (tenant_id, normalized_value) DO NOTHING;

-- ============================================================================
-- Verify data was loaded
-- ============================================================================
\echo ''
\echo '============================================================================'
\echo 'Verification Results:'
\echo '============================================================================'

-- Packet count
\echo ''
\echo '1. Packet count loaded:'
SELECT COUNT(*) as packet_count FROM herd_signal_packets WHERE tenant_id = :'tenant_id'::uuid;

-- Distinct tags
\echo ''
\echo '2. Distinct tags in packets:'
SELECT COUNT(DISTINCT tag_id) as distinct_tags FROM herd_signal_packets WHERE tenant_id = :'tenant_id'::uuid;

-- Gateway
\echo ''
\echo '3. Gateway registration:'
SELECT gateway_id, label, ble_mac, status FROM herd_signal_gateways WHERE tenant_id = :'tenant_id'::uuid;

-- Tag latest with movement state
\echo ''
\echo '4. Tag latest snapshots (sample 5):'
SELECT
  tag_id,
  tag_mac,
  last_seen_at,
  last_rssi_dbm::text as rssi_dbm,
  battery_mv,
  motion_count,
  movement_state,
  mapping_state
FROM herd_signal_tag_latest
WHERE tenant_id = :'tenant_id'::uuid
ORDER BY last_seen_at DESC
LIMIT 5;

-- Activity window buckets
\echo ''
\echo '5. Activity window bucket count:'
SELECT COUNT(*) as window_count FROM herd_signal_activity_windows WHERE tenant_id = :'tenant_id'::uuid;

-- Activity windows per bucket tier
\echo ''
\echo '6. Activity windows per bucket size:'
SELECT bucket_seconds, COUNT(*) as window_count FROM herd_signal_activity_windows WHERE tenant_id = :'tenant_id'::uuid GROUP BY bucket_seconds;

-- Mapped tags to goats
\echo ''
\echo '7. Mapped tags (19 tags in Castro shed):'
SELECT
  gi.identifier_value as tag_id,
  g.display_id as goat_id,
  l.name as shed_name,
  gi.status,
  gi.smart_tag_capable
FROM goat_identifiers gi
  JOIN goats g ON gi.goat_id = g.goat_id
  JOIN locations l ON g.current_location_id = l.location_id
WHERE gi.tenant_id = :'tenant_id'::uuid
  AND gi.source_system = 'herd-signals-oci-seed'
  AND gi.smart_tag_capable = true
ORDER BY gi.identifier_value;

-- Unmapped tag (A0003B)
\echo ''
\echo '8. Unmapped tag (A0003B - for testing unmapped state):'
SELECT
  tag_id,
  tag_mac,
  mapping_state,
  last_seen_at,
  motion_count
FROM herd_signal_tag_latest
WHERE tenant_id = :'tenant_id'::uuid
  AND tag_id = 'A0003B';

-- Confirm that tag_to_goat query for mapped tags works
\echo ''
\echo '9. Tag-to-goat resolution (testing mapping lookup):'
SELECT
  tl.tag_id,
  tl.tag_mac,
  COALESCE(gi.goat_id::text, 'UNMAPPED') as goat_id,
  COALESCE(g.display_id, 'N/A') as goat_display_id,
  tl.mapping_state
FROM herd_signal_tag_latest tl
  LEFT JOIN goat_identifiers gi ON (
    tl.tenant_id = gi.tenant_id
    AND lower(tl.tag_id) = lower(gi.normalized_value)
    AND gi.status = 'active'
    AND gi.smart_tag_capable = true
  )
  LEFT JOIN goats g ON gi.goat_id = g.goat_id
WHERE tl.tenant_id = :'tenant_id'::uuid
ORDER BY tl.tag_id;

COMMIT;

\echo ''
\echo 'seed-herd-signals-oci: Completed successfully'
