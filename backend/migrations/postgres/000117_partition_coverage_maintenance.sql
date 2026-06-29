-- +goose Up

CREATE OR REPLACE FUNCTION goatos_partition_parent_specs()
RETURNS TABLE(parent_table regclass, partition_prefix text)
LANGUAGE sql
STABLE
AS $$
  VALUES
    ('goat_identity_events'::regclass, 'goat_identity_events'),
    ('audit_log'::regclass, 'audit_log'),
    ('obligation_status_events'::regclass, 'obligation_status_events')
$$;

CREATE OR REPLACE FUNCTION goatos_month_partition_name(partition_prefix text, month_start date)
RETURNS text
LANGUAGE sql
IMMUTABLE
AS $$
  SELECT format('%s_%s', partition_prefix, to_char(month_start, 'YYYY_MM'))
$$;

CREATE OR REPLACE FUNCTION goatos_ensure_monthly_partitions(
  parent_table regclass,
  partition_prefix text,
  reference_at timestamptz,
  months_ahead integer
)
RETURNS integer
LANGUAGE plpgsql
AS $$
DECLARE
  schema_name text;
  parent_name text;
  cursor_month date;
  target_exclusive date;
  child_name text;
  created_count integer := 0;
BEGIN
  IF months_ahead < 12 OR months_ahead > 60 THEN
    RAISE EXCEPTION 'months_ahead must be between 12 and 60, got %', months_ahead;
  END IF;

  SELECT n.nspname, c.relname
    INTO schema_name, parent_name
    FROM pg_class c
    JOIN pg_namespace n ON n.oid = c.relnamespace
   WHERE c.oid = parent_table;

  IF schema_name IS NULL THEN
    RAISE EXCEPTION 'partition parent % does not exist', parent_table;
  END IF;

  IF NOT EXISTS (SELECT 1 FROM pg_partitioned_table WHERE partrelid = parent_table) THEN
    RAISE EXCEPTION 'partition parent % is not a partitioned table', parent_table;
  END IF;

  cursor_month := date_trunc('month', reference_at AT TIME ZONE 'UTC')::date;
  target_exclusive := (cursor_month + ((months_ahead + 1)::text || ' months')::interval)::date;

  WHILE cursor_month < target_exclusive LOOP
    child_name := goatos_month_partition_name(partition_prefix, cursor_month);
    IF to_regclass(format('%I.%I', schema_name, child_name)) IS NULL THEN
      EXECUTE format(
        'CREATE TABLE %I.%I PARTITION OF %I.%I FOR VALUES FROM (%L) TO (%L)',
        schema_name,
        child_name,
        schema_name,
        parent_name,
        to_char(cursor_month, 'YYYY-MM-DD') || ' 00:00:00+00',
        to_char((cursor_month + interval '1 month')::date, 'YYYY-MM-DD') || ' 00:00:00+00'
      );
      created_count := created_count + 1;
    END IF;
    cursor_month := (cursor_month + interval '1 month')::date;
  END LOOP;

  RETURN created_count;
END;
$$;

CREATE OR REPLACE FUNCTION goatos_partition_coverage_report(
  reference_at timestamptz DEFAULT now(),
  months_ahead integer DEFAULT 12
)
RETURNS TABLE(
  parent_table text,
  coverage_start date,
  coverage_through date,
  expected_months integer,
  present_months integer,
  missing_months text[]
)
LANGUAGE plpgsql
STABLE
AS $$
DECLARE
  spec record;
  schema_name text;
  start_month date;
  through_month date;
BEGIN
  IF months_ahead < 12 OR months_ahead > 60 THEN
    RAISE EXCEPTION 'months_ahead must be between 12 and 60, got %', months_ahead;
  END IF;

  start_month := date_trunc('month', reference_at AT TIME ZONE 'UTC')::date;
  through_month := (start_month + (months_ahead::text || ' months')::interval)::date;

  FOR spec IN SELECT s.parent_table, s.partition_prefix FROM goatos_partition_parent_specs() s LOOP
    SELECT n.nspname
      INTO schema_name
      FROM pg_class c
      JOIN pg_namespace n ON n.oid = c.relnamespace
     WHERE c.oid = spec.parent_table;

    RETURN QUERY
    WITH months AS (
      SELECT generate_series(start_month, through_month, interval '1 month')::date AS month_start
    ),
    checked AS (
      SELECT
        month_start,
        to_regclass(format('%I.%I', schema_name, goatos_month_partition_name(spec.partition_prefix, month_start))) IS NOT NULL AS present
      FROM months
    )
    SELECT
      spec.parent_table::text,
      start_month,
      through_month,
      count(*)::integer,
      count(*) FILTER (WHERE present)::integer,
      COALESCE(
        array_agg(to_char(month_start, 'YYYY-MM') ORDER BY month_start) FILTER (WHERE NOT present),
        ARRAY[]::text[]
      )
    FROM checked;
  END LOOP;
END;
$$;

CREATE OR REPLACE FUNCTION goatos_assert_partition_coverage(
  reference_at timestamptz DEFAULT now(),
  months_ahead integer DEFAULT 12
)
RETURNS void
LANGUAGE plpgsql
AS $$
DECLARE
  bad record;
BEGIN
  SELECT *
    INTO bad
    FROM goatos_partition_coverage_report(reference_at, months_ahead)
   WHERE array_length(missing_months, 1) IS NOT NULL
   LIMIT 1;

  IF FOUND THEN
    RAISE EXCEPTION 'missing monthly partitions for % through %: %',
      bad.parent_table,
      bad.coverage_through,
      bad.missing_months;
  END IF;
END;
$$;

CREATE OR REPLACE FUNCTION goatos_ensure_partition_coverage(
  reference_at timestamptz DEFAULT now(),
  months_ahead integer DEFAULT 12
)
RETURNS TABLE(parent_table text, created_partitions integer, coverage_through date)
LANGUAGE plpgsql
AS $$
DECLARE
  spec record;
  lock_acquired boolean;
  created_count integer;
  start_month date;
  through_month date;
BEGIN
  lock_acquired := pg_try_advisory_lock(hashtext('goatos:partition-maintenance'));
  IF NOT lock_acquired THEN
    RAISE EXCEPTION 'partition maintenance is already running';
  END IF;

  start_month := date_trunc('month', reference_at AT TIME ZONE 'UTC')::date;
  through_month := (start_month + (months_ahead::text || ' months')::interval)::date;

  BEGIN
    FOR spec IN SELECT s.parent_table, s.partition_prefix FROM goatos_partition_parent_specs() s LOOP
      created_count := goatos_ensure_monthly_partitions(spec.parent_table, spec.partition_prefix, reference_at, months_ahead);
      parent_table := spec.parent_table::text;
      created_partitions := created_count;
      coverage_through := through_month;
      RETURN NEXT;
    END LOOP;

    PERFORM goatos_assert_partition_coverage(reference_at, months_ahead);
  EXCEPTION WHEN OTHERS THEN
    PERFORM pg_advisory_unlock(hashtext('goatos:partition-maintenance'));
    RAISE;
  END;

  PERFORM pg_advisory_unlock(hashtext('goatos:partition-maintenance'));
END;
$$;

-- +goose Down

DROP FUNCTION IF EXISTS goatos_ensure_partition_coverage(timestamptz, integer);
DROP FUNCTION IF EXISTS goatos_assert_partition_coverage(timestamptz, integer);
DROP FUNCTION IF EXISTS goatos_partition_coverage_report(timestamptz, integer);
DROP FUNCTION IF EXISTS goatos_ensure_monthly_partitions(regclass, text, timestamptz, integer);
DROP FUNCTION IF EXISTS goatos_month_partition_name(text, date);
DROP FUNCTION IF EXISTS goatos_partition_parent_specs();
