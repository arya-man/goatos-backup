-- Herd Signals: partition maintenance for the daily-partitioned herd_signal_packets (000200).
--
-- Partition key is received_date (a generated, UTC-calendar-day bucket of received_at -- see
-- 000200's header for why the raw timestamp cannot be the partition key here). Both functions
-- below operate purely on partition names and their date bounds, so they are unaffected by that
-- column's exact definition.
--
-- Two hardcoded, single-table functions -- deliberately NOT a generic "partition maintainer"
-- that takes a table name as a parameter. A parameterized version could be pointed at
-- herd_signal_activity_windows or herd_signal_tag_latest by a typo or a future caller who does
-- not know those tables are NOT partitioned and hold long-lived aggregates
-- (docs/modules/herd-signals-system-design.md Section 3: 24-48h / 30d / 13mo retention on
-- aggregates, vs 14 days on raw packets). Hardcoding the target relation to
-- 'public.herd_signal_packets'::regclass inside the function body makes "accidentally delete
-- aggregates with this job" a compile-time impossibility, not a runtime discipline.
--
-- herd_signal_packets_ensure_future_partitions(days_ahead): idempotent. Creates any missing
-- daily partition from today through today + days_ahead. Must run BEFORE the runway created by
-- 000200 (30 days) is exhausted, or ingest hits "no partition of relation herd_signal_packets
-- found for row" -- an ingest OUTAGE, not a soft failure (there is a DEFAULT partition as a
-- last-resort catch, but relying on it defeats partition pruning and mixes days into one
-- unbounded partition, so it must be treated as a bug if it starts collecting rows).
--
-- herd_signal_packets_prune_expired_partitions(retention_days): drops whole daily partitions
-- entirely older than the retention cutoff via DROP TABLE -- a catalog operation, independent of
-- partition row count, no dead tuples, no index bloat, no VACUUM FULL ever needed. This is the
-- entire point of partitioning ahead of scale (000200's header works the arithmetic): the
-- alternative, DELETE ... WHERE received_at < cutoff on an unpartitioned table, would need to
-- delete hundreds of millions of rows to reclaim one day at the stated envelope.
--
-- RETENTION DEFAULT: 14 days, configurable per-call via the retention_days argument.
-- docs/modules/herd-signals-system-design.md Section 3 states the reasoning this migration
-- follows: "no read path reads herd_signal_packets... raw packets exist for forensics and for
-- re-derivation if the advertisement decoding rules change" and recommends 7-14 days. This
-- migration defaults to the TOP of that range (14, not 7) because retention cost is symmetric --
-- keeping a few extra days of an unread table costs storage, not correctness or latency -- while
-- the forensics/re-derivation value of a wider window is asymmetric: once a partition is
-- DROPped, that data is gone, and there is no read path exercising this table today that would
-- surface a "we needed 10 days, we kept 7" regret before it is too late to matter. Aggregates
-- (herd_signal_activity_windows, herd_signal_tag_latest) are NOT touched by either function in
-- this file and keep their own, separately-designed, longer retention.

-- +goose Up

CREATE OR REPLACE FUNCTION public.herd_signal_packets_ensure_future_partitions(days_ahead integer DEFAULT 14)
RETURNS TABLE(partition_name text, range_start date, range_end date, created boolean)
LANGUAGE plpgsql
AS $$
DECLARE
  parent_oid oid := 'public.herd_signal_packets'::regclass;
  d date;
  last_day date := current_date + days_ahead;
  part_name text;
  already_exists boolean;
BEGIN
  IF days_ahead < 0 THEN
    RAISE EXCEPTION 'days_ahead must be >= 0, got %', days_ahead;
  END IF;

  d := current_date;
  WHILE d <= last_day LOOP
    part_name := format('herd_signal_packets_p%s', to_char(d, 'YYYY_MM_DD'));

    SELECT EXISTS (
      SELECT 1
      FROM pg_inherits i
      JOIN pg_class c ON c.oid = i.inhrelid
      WHERE i.inhparent = parent_oid
        AND c.relname = part_name
    ) INTO already_exists;

    IF NOT already_exists THEN
      EXECUTE format(
        'CREATE TABLE public.%I PARTITION OF public.herd_signal_packets FOR VALUES FROM (%L) TO (%L)',
        part_name, d, d + 1
      );
    END IF;

    partition_name := part_name;
    range_start := d;
    range_end := d + 1;
    created := NOT already_exists;
    RETURN NEXT;

    d := d + 1;
  END LOOP;
END;
$$;

COMMENT ON FUNCTION public.herd_signal_packets_ensure_future_partitions(integer) IS
  'Idempotent. Creates missing daily partitions of herd_signal_packets from today through today+days_ahead. Run on a schedule (recommended: daily, days_ahead >= 14) so ingest never hits a missing-partition error. Hardcoded to herd_signal_packets only.';

CREATE OR REPLACE FUNCTION public.herd_signal_packets_prune_expired_partitions(retention_days integer DEFAULT 14)
RETURNS TABLE(partition_name text, range_start date, range_end date)
LANGUAGE plpgsql
AS $$
DECLARE
  parent_oid oid := 'public.herd_signal_packets'::regclass;
  parent_relkind "char";
  cutoff date := current_date - retention_days;
  rec record;
  bound_from text;
  bound_to text;
  upper_bound date;
BEGIN
  IF retention_days < 1 THEN
    RAISE EXCEPTION 'retention_days must be >= 1, got % (raw packets must retain at least one full day)', retention_days;
  END IF;

  -- Belt-and-braces: refuse to run at all unless the target is actually the partitioned parent
  -- table we expect. If a future schema change replaces herd_signal_packets with something that
  -- is no longer a partitioned table, this function must fail loudly rather than silently no-op
  -- or, worse, silently do the wrong thing.
  SELECT relkind INTO parent_relkind FROM pg_class WHERE oid = parent_oid;
  IF parent_relkind IS DISTINCT FROM 'p' THEN
    RAISE EXCEPTION 'herd_signal_packets is not a partitioned table (relkind=%); refusing to run partition pruning', parent_relkind;
  END IF;

  FOR rec IN
    SELECT c.relname,
           pg_get_expr(c.relpartbound, c.oid) AS partbound
    FROM pg_inherits i
    JOIN pg_class c ON c.oid = i.inhrelid
    WHERE i.inhparent = parent_oid
      -- Never touch the DEFAULT partition or anything not named with the expected daily-partition
      -- pattern -- an operator-created oddly-named partition is left alone, not guessed at.
      AND c.relname ~ '^herd_signal_packets_p\d{4}_\d{2}_\d{2}$'
  LOOP
    -- partbound looks like: FOR VALUES FROM ('2026-08-20') TO ('2026-08-21')
    -- Extract the upper bound and only drop partitions ENTIRELY below the cutoff, so a
    -- partition straddling "today" (impossible for daily ranges, but defensive) is never dropped.
    bound_to := substring(rec.partbound FROM 'TO \(''([^'']+)''\)');
    IF bound_to IS NULL THEN
      CONTINUE;
    END IF;
    upper_bound := bound_to::date;

    IF upper_bound <= cutoff THEN
      bound_from := substring(rec.partbound FROM 'FROM \(''([^'']+)''\)');
      EXECUTE format('DROP TABLE public.%I', rec.relname);

      partition_name := rec.relname;
      range_start := bound_from::date;
      range_end := upper_bound;
      RETURN NEXT;
    END IF;
  END LOOP;
END;
$$;

COMMENT ON FUNCTION public.herd_signal_packets_prune_expired_partitions(integer) IS
  'Drops daily herd_signal_packets partitions entirely older than now() - retention_days (default 14) via DROP TABLE. Hardcoded to herd_signal_packets ONLY -- never touches herd_signal_activity_windows or herd_signal_tag_latest, which retain aggregates on their own, longer, separately-designed schedule. Never drops the DEFAULT partition or an unrecognised partition name.';

-- +goose Down

DROP FUNCTION IF EXISTS public.herd_signal_packets_prune_expired_partitions(integer);
DROP FUNCTION IF EXISTS public.herd_signal_packets_ensure_future_partitions(integer);
