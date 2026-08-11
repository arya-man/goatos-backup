-- +goose Up
-- +goose NO TRANSACTION
-- Move live partitioned goats to their exact partition shed in small committed
-- batches. goats is a hot table; this intentionally stays outside the
-- transactional schema migration so deploy can retry/resume instead of holding
-- one broad write transaction.
CREATE OR REPLACE PROCEDURE public.backfill_partition_goat_exact_residence(p_batch_size integer DEFAULT 250)
LANGUAGE plpgsql
AS $$
DECLARE
  moved_count integer;
BEGIN
  LOOP
    PERFORM set_config('lock_timeout', '2s', true);
    PERFORM set_config('statement_timeout', '30s', true);

    WITH candidates AS (
      SELECT
        g.ctid AS goat_ctid,
        sp.operational_location_id AS exact_shed_id,
        gsp.shed_id AS group_shed_id
      FROM public.goats g
      JOIN public.goat_shed_partitions gsp
        ON gsp.tenant_id = g.tenant_id
       AND gsp.goat_id = g.goat_id
      JOIN public.shed_partitions sp
        ON sp.tenant_id = gsp.tenant_id
       AND sp.shed_id = gsp.shed_id
       AND sp.normalized_label = regexp_replace(lower(btrim(gsp.partition_label)), '^part[[:space:]]+', '')
       AND sp.status = 'active'
       AND sp.operational_location_id IS NOT NULL
      WHERE g.lifecycle_status NOT IN ('dead','sold','culled','transferred','lost','merged','inactive')
        AND g.merged_into_goat_id IS NULL
        AND (
          g.current_location_id IS DISTINCT FROM sp.operational_location_id
          OR g.shed_id IS DISTINCT FROM sp.operational_location_id
          OR g.shed_group_id IS DISTINCT FROM gsp.shed_id
        )
      ORDER BY g.tenant_id, g.goat_id
      LIMIT GREATEST(p_batch_size, 1)
      FOR UPDATE OF g SKIP LOCKED
    )
    UPDATE public.goats g
    SET current_location_id = c.exact_shed_id,
        shed_id = c.exact_shed_id,
        shed_group_id = c.group_shed_id,
        updated_at = now(),
        row_version = g.row_version + 1
    FROM candidates c
    WHERE g.ctid = c.goat_ctid;

    GET DIAGNOSTICS moved_count = ROW_COUNT;
    COMMIT;
    EXIT WHEN moved_count = 0;
  END LOOP;
END;
$$;

CALL public.backfill_partition_goat_exact_residence(250);

DROP PROCEDURE public.backfill_partition_goat_exact_residence(integer);

-- +goose Down
-- +goose NO TRANSACTION
CREATE OR REPLACE PROCEDURE public.rollback_partition_goat_exact_residence(p_batch_size integer DEFAULT 250)
LANGUAGE plpgsql
AS $$
DECLARE
  moved_count integer;
BEGIN
  LOOP
    PERFORM set_config('lock_timeout', '2s', true);
    PERFORM set_config('statement_timeout', '30s', true);

    WITH candidates AS (
      SELECT g.ctid AS goat_ctid, g.shed_group_id AS group_shed_id
      FROM public.goats g
      JOIN public.shed_partitions sp
        ON sp.tenant_id = g.tenant_id
       AND sp.operational_location_id = g.shed_id
       AND sp.shed_id = g.shed_group_id
      WHERE g.shed_group_id IS NOT NULL
        AND g.current_location_id = sp.operational_location_id
        AND g.shed_id = sp.operational_location_id
      ORDER BY g.tenant_id, g.goat_id
      LIMIT GREATEST(p_batch_size, 1)
      FOR UPDATE OF g SKIP LOCKED
    )
    UPDATE public.goats g
    SET current_location_id = c.group_shed_id,
        shed_id = c.group_shed_id,
        updated_at = now(),
        row_version = g.row_version + 1
    FROM candidates c
    WHERE g.ctid = c.goat_ctid;

    GET DIAGNOSTICS moved_count = ROW_COUNT;
    COMMIT;
    EXIT WHEN moved_count = 0;
  END LOOP;
END;
$$;

CALL public.rollback_partition_goat_exact_residence(250);

DROP PROCEDURE public.rollback_partition_goat_exact_residence(integer);
