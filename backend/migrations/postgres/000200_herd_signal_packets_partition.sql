-- Herd Signals: convert herd_signal_packets to daily RANGE partitioning on received_at.
--
-- WHY NOW, AT ~50K ROWS
--
-- docs/modules/herd-signals-system-design.md Section 1.5/3.1 works the arithmetic: at the
-- stated envelope (50,000 tags, one packet per tag per 10s) this table grows at
-- 50,000/10*86,400 = 432,000,000 rows/day, ~190 GB/day, and multiple billion rows within a
-- week. Converting an unpartitioned table of that size to partitioned requires copying every
-- row under a lock that scales with the row count -- effectively undoable once the table is
-- large. At today's ~50k captured rows the same conversion copies a few MB and completes in a
-- lock window bounded at a small fraction of a second. This migration exists to take that
-- window while it is cheap, not because today's volume needs it.
--
-- WHY DAILY, NOT WEEKLY
--
-- Retention (000201) defaults to 14 days. Daily partitions divide that window into 14 equal,
-- independently droppable units; weekly would leave only ~2 live partitions, each holding up to
-- ~1.33 TB at the 50k-tag envelope (Section 1.5) -- too coarse a unit for autovacuum and
-- DROP-based retention to act on independently, and it under-shoots the granularity the 60s/
-- 300s/3600s activity-window tiers already use to bound their own read ranges (Section 3). Daily
-- also means "yesterday's partition" and "today's retention cutoff" are the same concept, which
-- keeps the maintenance job in 000201 simple to reason about.
--
-- WHY received_at, NEVER THE GATEWAY CLOCK
--
-- Migration 000198's header (and the 000198 security fix it documents) establishes that
-- received_at is stamped from the SERVER clock once per ingest call, while device_seen_at is the
-- gateway's own claimed timestamp -- observed hours off in the field and controllable by
-- whatever is on the other end of the ingest endpoint. Partitioning on device_seen_at would let a
-- single bad gateway clock route packets into a partition far outside the ingest-time window,
-- defeating both partition pruning and the retention job's "drop what's old" logic. received_at
-- is monotonic-per-ingest-call and attacker/clock independent, so it is the only correct
-- partition key.
--
-- THE DEDUP-IDENTITY CORRECTNESS DECISION
--
-- PostgreSQL requires every unique index (and therefore the PRIMARY KEY) on a partitioned table
-- to include the partition key column. herd_signal_packets_dedup_uidx (000196) is
-- (tenant_id, tag_id, device_seen_at, motion_count) WHERE device_seen_at IS NOT NULL --
-- deliberately NOT including received_at, because 000196 exists precisely to dedup a retried
-- ingest batch, which gets a NEW received_at on every attempt. Adding received_at to that index
-- is therefore not a no-op: it weakens dedup for the case where a retry's received_at lands in a
-- DIFFERENT daily partition than the original attempt's.
--
-- This migration accepts that weakening, deliberately, for one reason: docs section 3 is explicit
-- that "no read path reads herd_signal_packets" -- every chart, baseline, and pattern in this
-- product reads herd_signal_activity_windows or herd_signal_tag_latest, both of which are rolled
-- up from the DEDUPED newPackets subset at ingest time (repository.go IngestPackets), not
-- re-derived from this table on every read. A duplicate raw packet row that slips through because
-- a retry straddled midnight UTC:
--   (a) requires a network-timeout retry (already the rare path, not the common one) to ALSO land
--       within seconds of a partition boundary -- a rare intersection of two already-rare events;
--   (b) produces one extra forensics-only row that nothing downstream reads;
--   (c) does NOT double-count anything in the product: the activity-window rollup and tag_latest
--       advance-only guard operate on newPackets from THAT ingest call's RowsAffected>0 subset,
--       so a duplicate raw row from a boundary-straddling retry would still be counted once per
--       ingest call it was inserted in -- the existing "two gateways both forward the same
--       advertisement" limitation documented in the design doc Section 2.2 already accepts this
--       class of double-count as a known, low-consequence gap, not a new one this migration opens.
-- The alternative -- partitioning on device_seen_at instead of received_at to keep the dedup key
-- partition-key-free -- was rejected above because it hands an adversarial/inaccurate clock
-- control over ingest routing and retention. Keeping received_at as partition key and accepting
-- the narrowed dedup guarantee is the smaller, better-understood risk.
--
-- The PRIMARY KEY changes from (packet_id) to (packet_id, received_at) for the same structural
-- reason. packet_id (gen_random_uuid()) remains practically unique -- native declarative
-- partitioning cannot enforce TRUE global uniqueness on a non-partition-key column across
-- partitions, and no code path relies on packet_id as a foreign key target (grep confirms no
-- other table references herd_signal_packets.packet_id), so this is a safe, honest trade.
--
-- CONVERSION STRATEGY AND LOCK PROFILE
--
-- A plain ALTER TABLE cannot turn an existing heap into a partitioned table in place -- PostgreSQL
-- has no such operation. The safe-at-this-size approach is build-alongside-and-swap:
--   1. Build the new partitioned table (empty), with matching columns/indexes and the updated
--      dedup/PK shape described above, under a working name.
--   2. Pre-create daily partitions spanning the existing table's actual [min, max] received_at
--      (so no captured row is homeless) plus a 30-day runway into the future (so ingest never
--      hits a missing-partition error -- see 000201 for the ongoing job that keeps extending
--      this runway), plus a DEFAULT partition as a last-resort safety net.
--   3. LOCK the live table ACCESS EXCLUSIVE, copy every existing row, then rename old -> legacy
--      and new -> live, all inside ONE transaction so no writer can observe a half-migrated state
--      and no row written between "copy" and "rename" is lost.
--
-- Lock profile: ONE ACCESS EXCLUSIVE lock on herd_signal_packets, held only across steps 3a-3c
-- (the row copy + two catalog renames) -- NOT across step 1-2, which run against a table nobody
-- else references yet and take no lock on the live table at all. `lock_timeout` is set locally so
-- if the exclusive lock cannot be acquired promptly (a write is mid-flight), this migration fails
-- fast and can be retried, rather than queuing behind live traffic indefinitely. At today's ~50k
-- rows the copy itself is expected to complete in well under a second, so total live-table lock
-- time is expected to be sub-second; this bound only holds because the conversion is happening
-- now, at this row count -- see the arithmetic at the top of this file for why waiting is not free.
--
-- OPERATIONAL WARNING -- DO NOT APPLY TO A LIVE goatos DATABASE WITHOUT A BINARY RESTART
--
-- backend/internal/platform/postgres/postgres.go opens pgxpool with pgx's default
-- QueryExecMode (statement caching, prepared once per SQL text PER CONNECTION and bound to the
-- relation OID at prepare time). Renaming the live table under an already-connected pool does
-- NOT retroactively repoint a connection's already-cached prepared plan -- a connection that
-- prepared its INSERT before this migration ran would keep executing against the OLD table (now
-- renamed to herd_signal_packets_pre_partition_000200) until that connection is closed and
-- reopened. This pool sets no MaxConnLifetime, so that could persist indefinitely without an
-- explicit restart. Applying this migration must be paired with an API process restart
-- immediately after -- exactly the same discipline the existing migrate-then-restart deploy
-- practice already uses -- and must NOT be run against the live `goatos` database while the
-- current API process / UDP bridge connections are expected to keep serving traffic unattended.
-- This migration was validated on a disposable scratch database (see
-- backend/tests/integration/validate-sqlc-query-plans.sh's GOATOS_SQLC_PLAN_ADMIN_DSN pattern),
-- never against the live database, in the same session that authored it.
--
-- The pre-partition table is intentionally NOT dropped by this migration. It is kept as
-- `herd_signal_packets_pre_partition_000200` as a rollback/audit safety net; drop it manually
-- once the new partitioned table has been running long enough to trust (recommended: after one
-- full retention cycle, i.e. >= 14 days, once 000201's maintenance job has proven itself).

-- +goose Up

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '120s';

CREATE TABLE public.herd_signal_packets_new (
  packet_id uuid NOT NULL DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  gateway_id text,
  source text NOT NULL DEFAULT 'gateway',
  tag_id text NOT NULL,
  tag_mac text,
  received_at timestamptz NOT NULL,
  device_seen_at timestamptz,
  gateway_seen_at timestamptz,
  rssi_dbm smallint,
  battery_mv integer,
  tag_temperature_c numeric(5,2),
  motion_count bigint,
  sensor_state smallint,
  temperature_sensor_ok boolean,
  accelerometer_sensor_ok boolean,
  pkt_sn bigint,
  raw_adv text,
  raw_payload jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (packet_id, received_at)
) PARTITION BY RANGE (received_at);

COMMENT ON TABLE public.herd_signal_packets_new IS
  'Raw BLE advertisement packets, partitioned daily on received_at (server-stamped). Forensics-only -- see docs/modules/herd-signals-system-design.md Section 3. Retention managed by herd_signal_packets_prune_expired_partitions() (000201).';

-- Mirrors the three secondary indexes from 000192, now as partitioned indexes (each partition
-- gets its own local index automatically as it is created below / attached later).
CREATE INDEX herd_signal_packets_new_tenant_tag_received_idx
  ON public.herd_signal_packets_new (tenant_id, tag_id, received_at DESC);
CREATE INDEX herd_signal_packets_new_tenant_gateway_received_idx
  ON public.herd_signal_packets_new (tenant_id, gateway_id, received_at DESC);
CREATE INDEX herd_signal_packets_new_tenant_received_idx
  ON public.herd_signal_packets_new (tenant_id, received_at DESC);

-- Dedup identity: see the correctness-decision note above. received_at is added only because
-- the partition key is mandatory in this index; it is not part of the dedup semantics.
CREATE UNIQUE INDEX herd_signal_packets_new_dedup_uidx
  ON public.herd_signal_packets_new (tenant_id, tag_id, device_seen_at, motion_count, received_at)
  WHERE device_seen_at IS NOT NULL;

-- Pre-create daily partitions spanning existing data (if any) plus a 30-day forward runway, so
-- (a) every already-captured row has a home and (b) ingest cannot hit a missing-partition error
-- for the next month even if the maintenance job (000201) is not yet scheduled. A DEFAULT
-- partition catches anything outside that range as a last-resort safety net -- ingest must never
-- fail with "no partition of relation found for row".
DO $$
DECLARE
  min_day date;
  max_day date;
  d date;
  part_name text;
BEGIN
  SELECT date_trunc('day', min(received_at))::date, date_trunc('day', max(received_at))::date
    INTO min_day, max_day
    FROM public.herd_signal_packets;

  IF min_day IS NULL THEN
    min_day := current_date;
    max_day := current_date;
  END IF;

  -- Always cover today plus a 30-day forward runway, regardless of historical data range.
  IF max_day < current_date + 30 THEN
    max_day := current_date + 30;
  END IF;

  d := min_day;
  WHILE d <= max_day LOOP
    part_name := format('herd_signal_packets_p%s', to_char(d, 'YYYY_MM_DD'));
    EXECUTE format(
      'CREATE TABLE IF NOT EXISTS public.%I PARTITION OF public.herd_signal_packets_new FOR VALUES FROM (%L) TO (%L)',
      part_name, d, d + 1
    );
    d := d + 1;
  END LOOP;

  EXECUTE
    'CREATE TABLE IF NOT EXISTS public.herd_signal_packets_default '
    || 'PARTITION OF public.herd_signal_packets_new DEFAULT';
END $$;

-- Lock, copy, swap -- all in this transaction so no row is lost and no writer sees a
-- half-migrated table. See "CONVERSION STRATEGY AND LOCK PROFILE" above.
LOCK TABLE public.herd_signal_packets IN ACCESS EXCLUSIVE MODE;

INSERT INTO public.herd_signal_packets_new (
  packet_id, tenant_id, gateway_id, source, tag_id, tag_mac, received_at, device_seen_at,
  gateway_seen_at, rssi_dbm, battery_mv, tag_temperature_c, motion_count, sensor_state,
  temperature_sensor_ok, accelerometer_sensor_ok, pkt_sn, raw_adv, raw_payload, created_at
)
SELECT
  packet_id, tenant_id, gateway_id, source, tag_id, tag_mac, received_at, device_seen_at,
  gateway_seen_at, rssi_dbm, battery_mv, tag_temperature_c, motion_count, sensor_state,
  temperature_sensor_ok, accelerometer_sensor_ok, pkt_sn, raw_adv, raw_payload, created_at
FROM public.herd_signal_packets;

-- Free the canonical names/indexes off the legacy table before the swap.
ALTER INDEX public.herd_signal_packets_pkey RENAME TO herd_signal_packets_pre_partition_000200_pkey;
ALTER INDEX public.herd_signal_packets_tenant_tag_received_idx RENAME TO herd_signal_packets_pre_partition_000200_tenant_tag_received_idx;
ALTER INDEX public.herd_signal_packets_tenant_gateway_received_idx RENAME TO herd_signal_packets_pre_partition_000200_tenant_gateway_received_idx;
ALTER INDEX public.herd_signal_packets_tenant_received_idx RENAME TO herd_signal_packets_pre_partition_000200_tenant_received_idx;
ALTER INDEX public.herd_signal_packets_dedup_uidx RENAME TO herd_signal_packets_pre_partition_000200_dedup_uidx;

ALTER TABLE public.herd_signal_packets RENAME TO herd_signal_packets_pre_partition_000200;
ALTER TABLE public.herd_signal_packets_new RENAME TO herd_signal_packets;

ALTER INDEX public.herd_signal_packets_new_tenant_tag_received_idx RENAME TO herd_signal_packets_tenant_tag_received_idx;
ALTER INDEX public.herd_signal_packets_new_tenant_gateway_received_idx RENAME TO herd_signal_packets_tenant_gateway_received_idx;
ALTER INDEX public.herd_signal_packets_new_tenant_received_idx RENAME TO herd_signal_packets_tenant_received_idx;
ALTER INDEX public.herd_signal_packets_new_dedup_uidx RENAME TO herd_signal_packets_dedup_uidx;

COMMENT ON TABLE public.herd_signal_packets_pre_partition_000200 IS
  'Pre-partition snapshot kept by migration 000200 as a rollback/audit safety net. Safe to drop manually once the partitioned herd_signal_packets has run through at least one full retention cycle (>= 14 days) and 000201''s maintenance job is confirmed working.';

-- +goose Down

ALTER TABLE public.herd_signal_packets RENAME TO herd_signal_packets_new;

ALTER INDEX public.herd_signal_packets_tenant_tag_received_idx RENAME TO herd_signal_packets_new_tenant_tag_received_idx;
ALTER INDEX public.herd_signal_packets_tenant_gateway_received_idx RENAME TO herd_signal_packets_new_tenant_gateway_received_idx;
ALTER INDEX public.herd_signal_packets_tenant_received_idx RENAME TO herd_signal_packets_new_tenant_received_idx;
ALTER INDEX public.herd_signal_packets_dedup_uidx RENAME TO herd_signal_packets_new_dedup_uidx;

ALTER TABLE public.herd_signal_packets_pre_partition_000200 RENAME TO herd_signal_packets;

ALTER INDEX public.herd_signal_packets_pre_partition_000200_pkey RENAME TO herd_signal_packets_pkey;
ALTER INDEX public.herd_signal_packets_pre_partition_000200_tenant_tag_received_idx RENAME TO herd_signal_packets_tenant_tag_received_idx;
ALTER INDEX public.herd_signal_packets_pre_partition_000200_tenant_gateway_received_idx RENAME TO herd_signal_packets_tenant_gateway_received_idx;
ALTER INDEX public.herd_signal_packets_pre_partition_000200_tenant_received_idx RENAME TO herd_signal_packets_tenant_received_idx;
ALTER INDEX public.herd_signal_packets_pre_partition_000200_dedup_uidx RENAME TO herd_signal_packets_dedup_uidx;

-- Best-effort only: any row inserted into the partitioned table after Up ran, or any partition
-- dropped by the retention job (000201), is NOT recovered by this Down. This rollback is intended
-- for a same-session "the migration was wrong" reversal, not for undoing a live cutover after
-- retention has pruned data.
DROP TABLE IF EXISTS public.herd_signal_packets_new;
