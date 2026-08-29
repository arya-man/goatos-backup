-- Herd Signals OCI PostgreSQL seed: EXTERNAL FACTS ONLY.
--
-- This file is stage 1 of a two-stage seed. It loads only things that happened
-- outside GoatOS and cannot be derived from anything else:
--
--   1. The HoneyComm BLE gateway registration (MAC f130d402dcb4 at 192.168.0.9)
--   2. The captured BLE advertisement packets (herd_signal_packets)
--   3. The tag-to-animal mapping rows (goat_identifiers, smart_tag_capable)
--
-- It deliberately does NOT write herd_signal_tag_latest or
-- herd_signal_activity_windows. Those are DERIVED read models, materialized from
-- herd_signal_packets by the shipped ingest service. Hand-filling them in SQL
-- re-implements the classifier a second time and drifts from it: the previous
-- version of this file invented a `stationary` movement_state that is not in the
-- vocabulary at all, and labelled 12-hour-old readings `not_moving` where the
-- real rule says `stale`. A seeded stand-in for derived state is a self-fulfilling
-- seed, and AGENTS.md forbids it.
--
-- Stage 2 computes the derived state through the real code path:
--
--   go run ./backend/cmd/seed-herd-signals-oci -tenant-id ... -gateway-id ...
--
-- Run both stages together with tools/local/seed-herd-signals-oci.sh, which also
-- enforces the OCI-only target guard. Full flow:
-- docs/runbooks/herd-signals-oci-seed.md
--
-- Idempotent: every write is ON CONFLICT DO NOTHING against a real unique key.
-- Packet dedup uses the natural key (tenant_id, tag_id, received_at, motion_count)
-- from migration 000192 (NULLS NOT DISTINCT).
--
-- SAFETY: refuses to run against anything but the OCI dev database. See the
-- guard block below and the host/port guard in seed-herd-signals-oci.sh.
--
-- Usage (prefer the wrapper):
--   GOATOS_HERD_SIGNALS_CAPTURE_CSV=/path/to/decoded_ear_tags.csv \
--     psql "$DATABASE_URL" -f tools/local/seed-herd-signals-oci.sql
--
-- The capture path arrives as an ENVIRONMENT VARIABLE read by a client-side
-- \copy FROM PROGRAM, not as a psql :variable, because psql does not expand
-- :variables inside \copy arguments (verified on psql 18.4: "\copy t FROM :f"
-- fails with ":f: No such file or directory"). Making the path injectable is
-- what lets the seed be pointed at a FROZEN copy of the capture -- the live
-- file is still being appended by the gateway, so two runs against it can never
-- be compared.

\set ON_ERROR_STOP on

-- ============================================================================
-- SAFETY CHECK: this must be the OCI dev database, never prod and never the
-- local 5433 stack. The wrapper checks host/port; this checks server-side.
-- ============================================================================
DO $$
DECLARE
  v_db_name text;
  v_inet text;
BEGIN
  SELECT current_database() INTO v_db_name;
  SELECT inet_client_addr()::text INTO v_inet;

  IF v_db_name != 'goatos' THEN
    RAISE EXCEPTION 'SAFETY: wrong database. Expected goatos, got %', v_db_name;
  END IF;

  -- The OCI Postgres binds 127.0.0.1 only and is reached through the SSH tunnel,
  -- so the server always sees a loopback (or Tailscale 10.88/16) client address.
  IF v_inet IS NOT NULL AND v_inet NOT LIKE '127.0.%' AND v_inet NOT LIKE '10.88.%' THEN
    RAISE EXCEPTION 'SAFETY: suspected non-OCI connection from %', v_inet;
  END IF;

  RAISE NOTICE 'seed-herd-signals-oci: starting on database % via %', v_db_name, COALESCE(v_inet, 'socket');
END $$;

\set tenant_id '00000000-0000-4000-8000-000000000001'
\set castro_shed_id '62241795-628e-58ef-9591-aa384fb0f0f7'

BEGIN;

-- ============================================================================
-- FACT 1: the gateway that produced the capture
-- ============================================================================
\echo 'Fact 1: HoneyComm gateway f130d402dcb4 at 192.168.0.9'

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
-- FACT 2: the captured BLE advertisement packets
-- ============================================================================
\echo 'Fact 2: loading capture CSV from $GOATOS_HERD_SIGNALS_CAPTURE_CSV'

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
) ON COMMIT DROP;

\copy herd_signals_csv_import FROM PROGRAM 'cat -- "${GOATOS_HERD_SIGNALS_CAPTURE_CSV:?set GOATOS_HERD_SIGNALS_CAPTURE_CSV to the capture CSV path}"' WITH (FORMAT csv, HEADER);

-- TIMEZONE CONTRACT (do not "simplify" these offsets):
--
--   received_at  is Asia/Kolkata WALL CLOCK, so it is anchored with '+05:30'.
--                Anchoring it with '+00' -- as an earlier load did -- silently
--                moves every packet 5h30m into the future and makes every
--                staleness/movement classification wrong.
--   packet_time  is the SAME instant on the gateway's own (misconfigured) clock,
--                which runs at UTC+08:00. Anchoring it with '+08' reproduces the
--                identical instant as received_at while keeping the capture's
--                sub-second precision. Verified on the capture: received_at
--                2026-08-22T15:52:08+05:30 == packet_time 2026-08-22
--                18:22:08.183+08:00 == 2026-08-22T10:22:08Z.
INSERT INTO herd_signal_packets (
  tenant_id, gateway_id, source, tag_id, tag_mac,
  received_at, gateway_seen_at,
  rssi_dbm, battery_mv, tag_temperature_c, motion_count, sensor_state,
  temperature_sensor_ok, accelerometer_sensor_ok, raw_adv, raw_payload, created_at
)
SELECT
  :'tenant_id'::uuid,
  'honeycomm-gateway-001',
  'gateway',
  csv.printed_id,
  csv.tag_addr,
  (csv.received_at_str || '+05:30')::timestamptz,
  (csv.packet_time_str || '+08:00')::timestamptz,
  NULLIF(csv.rssi, '')::smallint,
  (NULLIF(csv.battery_v, '')::numeric * 1000)::integer,
  NULLIF(csv.temperature_c, '')::numeric(5,2),
  NULLIF(csv.motion_count, '')::bigint,
  NULLIF(csv.sensor_state, '')::smallint,
  csv.temp_sensor_ok = 'True',
  csv.accel_sensor_ok = 'True',
  NULLIF(csv.adv_raw, ''),
  '{}'::jsonb,
  now()
FROM herd_signals_csv_import csv
WHERE NULLIF(csv.printed_id, '') IS NOT NULL
  AND NULLIF(csv.received_at_str, '') IS NOT NULL
-- herd_signal_packets_dedup_uidx (migration 000192):
-- (tenant_id, tag_id, received_at, motion_count) NULLS NOT DISTINCT.
ON CONFLICT DO NOTHING;

-- ============================================================================
-- FACT 3: which ear tag is on which animal
--
-- 19 of the 20 captured tags are mapped to live animals in the Castro shed.
-- A0003B is deliberately left UNMAPPED so the unmapped-tag path stays exercised.
--
-- normalized_value is stored in the capture's printed casing (A0002A), because
-- ResolveTagMapping compares goat_identifiers.normalized_value against the raw
-- tag_id / tag_mac carried on the packet without normalizing either side.
-- ============================================================================
\echo 'Fact 3: mapping 19 captured tags to Castro shed animals (A0003B stays unmapped)'

WITH tags_to_map AS (
  SELECT tag_id, ROW_NUMBER() OVER (ORDER BY tag_id) AS rn
  FROM (VALUES
    ('A0002A'), ('A0002B'), ('A0002C'), ('A0002D'), ('A0002E'),
    ('A0002F'), ('A00030'), ('A00031'), ('A00033'), ('A00034'),
    ('A00035'), ('A00036'), ('A00038'), ('A0003A'), ('A0003C'),
    ('A0003E'), ('A0003F'), ('A00040'), ('A00041')
  ) AS t(tag_id)
),
goats_in_shed AS (
  SELECT goat_id, ROW_NUMBER() OVER (ORDER BY created_at, goat_id) AS rn
  FROM (
    SELECT goat_id, created_at
    FROM goats
    WHERE tenant_id = :'tenant_id'::uuid
      AND current_location_id = :'castro_shed_id'::uuid
      AND lifecycle_status = 'alive'
    ORDER BY created_at, goat_id
    LIMIT 19
  ) g
)
INSERT INTO goat_identifiers (
  tenant_id, goat_id, identifier_type, identifier_value, normalized_value,
  scope_key, is_primary_for_goat, status, valid_from,
  source_system, source_record_id, normalizer_version, smart_tag_capable,
  created_at, updated_at
)
SELECT
  :'tenant_id'::uuid,
  g.goat_id,
  'animal_identifier_1',
  t.tag_id,
  t.tag_id,
  'ble-tag',
  false,
  'active',
  now(),
  'herd-signals-oci-seed',
  'tag-' || t.tag_id,
  'identifier_normalizer_v1',
  true,
  now(),
  now()
FROM tags_to_map t
JOIN goats_in_shed g ON g.rn = t.rn
ON CONFLICT (tenant_id, normalized_value) DO NOTHING;

COMMIT;

-- ============================================================================
-- Verification: FACTS ONLY.
--
-- tag_latest / activity_windows counts are intentionally NOT asserted here --
-- this stage does not produce them. Verify those after stage 2 (the Go
-- replayer); see docs/runbooks/herd-signals-oci-seed.md.
-- ============================================================================
\echo ''
\echo '--- facts loaded ---'

\echo '1. packets:'
SELECT count(*) AS packet_count FROM herd_signal_packets WHERE tenant_id = :'tenant_id'::uuid;

\echo '2. distinct tags in packets:'
SELECT count(DISTINCT tag_id) AS distinct_tags FROM herd_signal_packets WHERE tenant_id = :'tenant_id'::uuid;

\echo '3. packet received_at range (should be Asia/Kolkata wall clock, never in the future):'
SELECT min(received_at) AS first_packet, max(received_at) AS last_packet, now() AS server_now
FROM herd_signal_packets WHERE tenant_id = :'tenant_id'::uuid;

\echo '4. gateways:'
SELECT gateway_id, label, ble_mac, status FROM herd_signal_gateways WHERE tenant_id = :'tenant_id'::uuid;

\echo '5. mapped tags (expect 19; A0003B absent):'
SELECT count(*) AS mapped_tags
FROM goat_identifiers
WHERE tenant_id = :'tenant_id'::uuid
  AND source_system = 'herd-signals-oci-seed'
  AND smart_tag_capable IS TRUE;

\echo ''
\echo 'seed-herd-signals-oci (stage 1, facts): completed'
\echo 'Next: stage 2 replays these packets through the ingest service to compute'
\echo 'herd_signal_tag_latest and herd_signal_activity_windows.'
