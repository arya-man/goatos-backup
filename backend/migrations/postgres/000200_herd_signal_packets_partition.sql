-- Herd Signals: convert herd_signal_packets to daily RANGE partitioning.
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
-- WHY THE PARTITION KEY IS A SEPARATE received_date COLUMN, NOT THE RAW received_at TIMESTAMP,
-- AND WHY THAT COLUMN IS APPLICATION-SUPPLIED RATHER THAN COMPUTED BY POSTGRES
--
-- This is a correctness decision, not a style choice, and both halves of it were caught by
-- actually testing the naive versions on a scratch database, not just reasoning about them --
-- worth recording so neither is undone by accident.
--
-- Migration 000198 establishes that received_at is stamped from the SERVER clock ONCE PER
-- INGEST CALL, while device_seen_at is the caller's own claimed timestamp -- observed hours off
-- in the field. So received_at, not device_seen_at, must drive partition routing and retention
-- (partitioning on a field an attacker or a bad gateway clock controls would let one bad clock
-- route packets into the wrong partition, or evade retention).
--
-- Attempt 1 -- partition directly on the raw received_at column, widen
-- herd_signal_packets_dedup_uidx (000196) to
-- (tenant_id, tag_id, device_seen_at, motion_count, received_at). This compiles (Postgres requires
-- the partition key in every unique index on a partitioned table), and it looks like a narrow
-- trade ("a retry that straddles a partition boundary won't dedup"). It is not narrow: received_at
-- is stamped fresh, to microsecond precision, on EVERY ingest call including retries of the exact
-- same physical batch -- 000198's own header says so. A unique index requires EXACT equality on
-- every column, so including full-precision received_at in the key means a retried batch's
-- received_at practically NEVER matches the original attempt's, on ANY day, not just at midnight.
-- Verified by seeding a scratch database, converting, and replaying a packet with a fresh
-- received_at: it inserted as a NEW row every time -- silently reintroducing the exact
-- double-count bug 000196 exists to close, for the ordinary case (any retry), not a rare edge
-- case. Rejected.
--
-- Attempt 2 -- bucket received_at to its UTC calendar day in a GENERATED ALWAYS ... STORED
-- received_date column, partition on that instead. This fails outright: PostgreSQL does not allow
-- a generated column to be a partition key at all ("cannot use generated column in partition
-- key"), verified on a scratch database. Rejected, not a viable option.
--
-- Attempt 3 (also tried and rejected) -- keep received_date a plain column with a default, and
-- populate it via a BEFORE INSERT trigger. This fails too: partition routing for a partitioned
-- table is decided from the tuple's column values BEFORE row-level BEFORE INSERT triggers run, so
-- a trigger that then writes a received_date implying a DIFFERENT partition is rejected with
-- "moving row to another partition during a BEFORE FOR EACH ROW trigger is not supported" --
-- verified on a scratch database. Rejected.
--
-- What actually works, and is used below: received_date is a plain (non-generated, no default)
-- `date NOT NULL` column, and the CALLER supplies its value explicitly on every insert --
-- backend/internal/herdsignals/adapters/postgres/repository.go computes it as
-- `p.ReceivedAt.UTC().Truncate(24*time.Hour)` from the SAME server-stamped p.ReceivedAt already
-- used for received_at itself, so routing and dedup identity can never disagree about which day a
-- packet belongs to. This migration's own data copy (below) computes the equivalent value in SQL
-- for the rows that predate the repository.go change. The dedup unique index becomes
-- (tenant_id, tag_id, device_seen_at, motion_count, received_date) WHERE device_seen_at IS NOT
-- NULL: a retry of the same batch, seconds to low-minutes later, gets a fresh received_at but
-- (except within a few seconds either side of UTC midnight) the SAME received_date, so the index
-- correctly collapses it. The genuinely narrow, honestly-rare residual case: a retry whose
-- received_at lands on the OTHER side of a UTC-midnight boundary from the original attempt is not
-- deduplicated by this index.
--
-- WHY THIS DOES NOT REOPEN THE DOUBLE-COUNT BUG FOR THE PRODUCT (only for raw forensics rows)
--
-- Even the narrow midnight-straddling gap that remains is forensics-only: docs section 3 is
-- explicit that "no read path reads herd_signal_packets" -- every chart, baseline, and pattern in
-- this product reads herd_signal_activity_windows or herd_signal_tag_latest, both rolled up from
-- the deduped newPackets subset PER INGEST CALL (repository.go IngestPackets), not re-derived
-- from this table on read. A duplicate raw row from a rare midnight-straddling retry is one extra
-- forensics-only row nothing downstream reads -- the same class of low-consequence gap the design
-- doc Section 2.2 already accepts for "two gateways forward the same advertisement".
--
-- The PRIMARY KEY changes from (packet_id) to (packet_id, received_date) for the same structural
-- reason (partition key must be in every unique index). packet_id (gen_random_uuid()) remains
-- practically unique -- native declarative partitioning cannot enforce TRUE global uniqueness on
-- a non-partition-key column across partitions, and no code path relies on packet_id as a foreign
-- key target (grep confirms no other table references herd_signal_packets.packet_id).
--
-- CONVERSION STRATEGY AND LOCK PROFILE
--
-- A plain ALTER TABLE cannot turn an existing heap into a partitioned table in place -- PostgreSQL
-- has no such operation. The safe-at-this-size approach is build-alongside-and-swap:
--   1. Build the new partitioned table (empty), with matching columns/indexes and the updated
--      dedup/PK shape described above, under a working name.
--   2. Pre-create daily partitions spanning the existing table's actual [min, max] received_date
--      (so no captured row is homeless) plus a 30-day runway into the future (so ingest never
--      hits a missing-partition error -- see 000201 for the ongoing job that keeps extending
--      this runway), plus a DEFAULT partition as a last-resort safety net.
--   3. LOCK the live table ACCESS EXCLUSIVE, copy every existing row (computing received_date for
--      each in the copy's SELECT list), then rename old -> legacy and new -> live, all inside ONE
--      transaction so no writer can observe a half-migrated state and no row written between
--      "copy" and "rename" is lost.
--
-- Lock profile: ONE ACCESS EXCLUSIVE lock on herd_signal_packets, held only across step 3 (the
-- row copy + two catalog renames) -- NOT across steps 1-2, which run against a table nobody else
-- references yet and take no lock on the live table at all. `lock_timeout` is set locally so if
-- the exclusive lock cannot be acquired promptly (a write is mid-flight), this migration fails
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
-- This migration (and the repository.go change that supplies received_date) was validated on a
-- disposable scratch database (see backend/tests/integration/validate-sqlc-query-plans.sh's
-- GOATOS_SQLC_PLAN_ADMIN_DSN pattern), never against the live database, in the same session that
-- authored it.
--
-- The pre-partition table is intentionally NOT dropped by this migration. It is kept as
-- `herd_signal_packets_pre_partition_000200` as a rollback/audit safety net; drop it manually
-- once the new partitioned table has been running long enough to trust (recommended: after one
-- full retention cycle, i.e. >= 14 days, once 000201's maintenance job has proven itself).

-- +goose NO TRANSACTION
--
-- This migration does NOT run inside goose's default whole-file transaction. It manages its own
-- transaction explicitly (BEGIN/COMMIT around only the LOCK+copy+rename step below) instead,
-- because the risky part needs an explicit transaction it fully controls (SET LOCAL and LOCK
-- TABLE both require an active transaction block), while the table/index/partition creation
-- above it is safe to run as ordinary autocommitted statements -- each builds an object nothing
-- references yet, so a failure there just leaves an orphan to clean up, not a half-migrated live
-- table. This also matches how this repo already treats CREATE INDEX CONCURRENTLY migrations
-- (NO TRANSACTION + the statement's own safety), and is required for compatibility with
-- backend/tests/integration/validate-sqlc-query-plans.sh's apply_goose_up, which pipes a
-- migration's Up section through plain autocommit psql rather than replicating goose's implicit
-- transaction wrapping.

-- +goose Up

CREATE TABLE public.herd_signal_packets_new (
  packet_id uuid NOT NULL DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  gateway_id text,
  source text NOT NULL DEFAULT 'gateway',
  tag_id text NOT NULL,
  tag_mac text,
  received_at timestamptz NOT NULL,
  -- Partition key. UTC calendar day of received_at (server-stamped, never the gateway clock).
  -- Deliberately NOT a generated column and NOT trigger-populated -- Postgres allows neither as
  -- a partition key mechanism, verified while building this migration (see header). The caller
  -- (repository.go IngestPackets, and this migration's own data copy below) supplies it
  -- explicitly, computed from the exact same received_at value.
  received_date date NOT NULL,
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
  PRIMARY KEY (packet_id, received_date)
) PARTITION BY RANGE (received_date);

COMMENT ON TABLE public.herd_signal_packets_new IS
  'Raw BLE advertisement packets, partitioned daily on received_date (UTC calendar day of the server-stamped received_at). Forensics-only -- see docs/modules/herd-signals-system-design.md Section 3. Retention managed by herd_signal_packets_prune_expired_partitions() (000201).';

COMMENT ON COLUMN public.herd_signal_packets_new.received_date IS
  'Application-supplied UTC calendar day of received_at. Partition key AND the dedup unique index''s partition-key column. NOT generated/trigger-populated -- Postgres disallows both for a partition key; the writer must compute and pass this explicitly (see repository.go IngestPackets and 000200 header).';

-- Mirrors the three secondary indexes from 000192, now as partitioned indexes (each partition
-- gets its own local index automatically as it is created below / attached later).
CREATE INDEX herd_signal_packets_new_tenant_tag_received_idx
  ON public.herd_signal_packets_new (tenant_id, tag_id, received_at DESC);
CREATE INDEX herd_signal_packets_new_tenant_gateway_received_idx
  ON public.herd_signal_packets_new (tenant_id, gateway_id, received_at DESC);
CREATE INDEX herd_signal_packets_new_tenant_received_idx
  ON public.herd_signal_packets_new (tenant_id, received_at DESC);

-- Dedup identity: (tenant_id, tag_id, device_seen_at, motion_count) is unchanged from 000196's
-- intent -- received_date is added ONLY because the partition key is mandatory in this index, and
-- bucketing to the UTC calendar day (rather than the raw received_at) means a same-day retry still
-- collides correctly. See the migration header for the full reasoning and the residual
-- midnight-boundary gap.
CREATE UNIQUE INDEX herd_signal_packets_new_dedup_uidx
  ON public.herd_signal_packets_new (tenant_id, tag_id, device_seen_at, motion_count, received_date)
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
  SELECT min((timezone('UTC', received_at))::date), max((timezone('UTC', received_at))::date)
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

-- Lock, copy, swap -- all in ONE explicit transaction so no row is lost and no writer sees a
-- half-migrated table. See "CONVERSION STRATEGY AND LOCK PROFILE" above. Explicit BEGIN/COMMIT
-- (not goose's implicit wrapping, which this file opted out of via NO TRANSACTION above) because
-- SET LOCAL and LOCK TABLE both require an active transaction block, and this is the one section
-- of this migration that must be atomic.
BEGIN;

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '120s';

LOCK TABLE public.herd_signal_packets IN ACCESS EXCLUSIVE MODE;

INSERT INTO public.herd_signal_packets_new (
  packet_id, tenant_id, gateway_id, source, tag_id, tag_mac, received_at, received_date,
  device_seen_at, gateway_seen_at, rssi_dbm, battery_mv, tag_temperature_c, motion_count,
  sensor_state, temperature_sensor_ok, accelerometer_sensor_ok, pkt_sn, raw_adv, raw_payload,
  created_at
)
SELECT
  packet_id, tenant_id, gateway_id, source, tag_id, tag_mac, received_at,
  (timezone('UTC', received_at))::date,
  device_seen_at, gateway_seen_at, rssi_dbm, battery_mv, tag_temperature_c, motion_count,
  sensor_state, temperature_sensor_ok, accelerometer_sensor_ok, pkt_sn, raw_adv, raw_payload,
  created_at
FROM public.herd_signal_packets;

-- Free the canonical names/indexes off the legacy table before the swap.
ALTER INDEX public.herd_signal_packets_pkey RENAME TO herd_signal_packets_legacy_pkey;
ALTER INDEX public.herd_signal_packets_tenant_tag_received_idx RENAME TO herd_signal_packets_legacy_tag_received_idx;
ALTER INDEX public.herd_signal_packets_tenant_gateway_received_idx RENAME TO herd_signal_packets_legacy_gw_received_idx;
ALTER INDEX public.herd_signal_packets_tenant_received_idx RENAME TO herd_signal_packets_legacy_received_idx;
ALTER INDEX public.herd_signal_packets_dedup_uidx RENAME TO herd_signal_packets_legacy_dedup_uidx;

ALTER TABLE public.herd_signal_packets RENAME TO herd_signal_packets_pre_partition_000200;
ALTER TABLE public.herd_signal_packets_new RENAME TO herd_signal_packets;

ALTER INDEX public.herd_signal_packets_new_tenant_tag_received_idx RENAME TO herd_signal_packets_tenant_tag_received_idx;
ALTER INDEX public.herd_signal_packets_new_tenant_gateway_received_idx RENAME TO herd_signal_packets_tenant_gateway_received_idx;
ALTER INDEX public.herd_signal_packets_new_tenant_received_idx RENAME TO herd_signal_packets_tenant_received_idx;
ALTER INDEX public.herd_signal_packets_new_dedup_uidx RENAME TO herd_signal_packets_dedup_uidx;

COMMENT ON TABLE public.herd_signal_packets_pre_partition_000200 IS
  'Pre-partition snapshot kept by migration 000200 as a rollback/audit safety net. Safe to drop manually once the partitioned herd_signal_packets has run through at least one full retention cycle (>= 14 days) and 000201''s maintenance job is confirmed working.';

COMMIT;

-- +goose Down
--
-- Also NO TRANSACTION (see Up section rationale); the rename sequence below is wrapped in its
-- own explicit BEGIN/COMMIT for the same "one atomic swap" reason.

BEGIN;

ALTER TABLE public.herd_signal_packets RENAME TO herd_signal_packets_new;

ALTER INDEX public.herd_signal_packets_tenant_tag_received_idx RENAME TO herd_signal_packets_new_tenant_tag_received_idx;
ALTER INDEX public.herd_signal_packets_tenant_gateway_received_idx RENAME TO herd_signal_packets_new_tenant_gateway_received_idx;
ALTER INDEX public.herd_signal_packets_tenant_received_idx RENAME TO herd_signal_packets_new_tenant_received_idx;
ALTER INDEX public.herd_signal_packets_dedup_uidx RENAME TO herd_signal_packets_new_dedup_uidx;

ALTER TABLE public.herd_signal_packets_pre_partition_000200 RENAME TO herd_signal_packets;

ALTER INDEX public.herd_signal_packets_legacy_pkey RENAME TO herd_signal_packets_pkey;
ALTER INDEX public.herd_signal_packets_legacy_tag_received_idx RENAME TO herd_signal_packets_tenant_tag_received_idx;
ALTER INDEX public.herd_signal_packets_legacy_gw_received_idx RENAME TO herd_signal_packets_tenant_gateway_received_idx;
ALTER INDEX public.herd_signal_packets_legacy_received_idx RENAME TO herd_signal_packets_tenant_received_idx;
ALTER INDEX public.herd_signal_packets_legacy_dedup_uidx RENAME TO herd_signal_packets_dedup_uidx;

COMMIT;

-- Best-effort only: any row inserted into the partitioned table after Up ran, or any partition
-- dropped by the retention job (000201), is NOT recovered by this Down. This rollback is intended
-- for a same-session "the migration was wrong" reversal, not for undoing a live cutover after
-- retention has pruned data.
DROP TABLE IF EXISTS public.herd_signal_packets_new;
