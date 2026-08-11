-- +goose Up
-- +goose NO TRANSACTION
CREATE OR REPLACE PROCEDURE public.backfill_partition_goat_exact_residence(p_batch_size integer DEFAULT 250)
LANGUAGE plpgsql
AS $$
DECLARE
  moved_count integer;
  remaining_count integer;
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
    IF moved_count = 0 THEN
      SELECT count(*) INTO remaining_count
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
        );
      EXIT WHEN remaining_count = 0;
      PERFORM pg_sleep(0.2);
    END IF;
  END LOOP;
END;
$$;

CALL public.backfill_partition_goat_exact_residence(250);

DROP PROCEDURE public.backfill_partition_goat_exact_residence(integer);

DO $$
DECLARE
  bad_count integer;
BEGIN
  SELECT count(*) INTO bad_count
  FROM public.goats g
  JOIN public.goat_shed_partitions gsp
    ON gsp.tenant_id = g.tenant_id
   AND gsp.goat_id = g.goat_id
  JOIN public.shed_partitions sp
    ON sp.tenant_id = gsp.tenant_id
   AND sp.shed_id = gsp.shed_id
   AND sp.normalized_label = regexp_replace(lower(btrim(gsp.partition_label)), '^part[[:space:]]+', '')
   AND sp.status = 'active'
  JOIN public.locations parent
    ON parent.tenant_id = sp.tenant_id
   AND parent.location_id = sp.shed_id
   AND parent.location_type = 'shed'
   AND parent.status = 'active'
  JOIN public.locations pen
    ON pen.tenant_id = sp.tenant_id
   AND pen.location_id = sp.operational_location_id
   AND pen.location_type = 'shed'
   AND pen.parent_location_id = parent.parent_location_id
   AND pen.location_id <> sp.shed_id
   AND pen.status = 'active'
  WHERE g.lifecycle_status NOT IN ('dead','sold','culled','transferred','lost','merged','inactive')
    AND g.merged_into_goat_id IS NULL
    AND (
      g.shed_id IS DISTINCT FROM sp.operational_location_id
      OR g.shed_group_id IS DISTINCT FROM gsp.shed_id
      OR g.current_location_id IS DISTINCT FROM sp.operational_location_id
    );

  IF bad_count <> 0 THEN
    RAISE EXCEPTION 'live_partitioned_goats_exact_residence_failed: % live goats do not match goat_shed_partitions mapped shed', bad_count;
  END IF;
END $$;

DO $$
DECLARE
  bad_count integer;
BEGIN
  SELECT count(*) INTO bad_count
  FROM public.goats g
  JOIN public.locations cur
    ON cur.tenant_id = g.tenant_id
   AND cur.location_id = g.current_location_id
  JOIN public.locations shed
    ON shed.tenant_id = g.tenant_id
   AND shed.location_id = g.shed_id
  WHERE g.lifecycle_status NOT IN ('dead','sold','culled','transferred','lost','merged','inactive')
    AND g.merged_into_goat_id IS NULL
    AND NOT (
      cur.location_type = 'shed'
      AND cur.location_id = g.shed_id
      AND shed.parent_location_id = g.park_id
    );

  IF bad_count <> 0 THEN
    RAISE EXCEPTION 'goat_current_location_rollup_invariant_failed: % live goats violate current_location/shed/park hierarchy', bad_count;
  END IF;
END $$;

WITH exact_goat_obligations AS (
  SELECT oi.tenant_id, oi.obligation_id, g.shed_id AS exact_shed_id
  FROM public.obligation_instances oi
  JOIN public.goats g
    ON g.tenant_id = oi.tenant_id
   AND g.goat_id = oi.target_id
   AND g.merged_into_goat_id IS NULL
  JOIN public.shed_partitions sp
    ON sp.tenant_id = g.tenant_id
   AND sp.operational_location_id = g.shed_id
  WHERE oi.target_type = 'goat'
    AND oi.scope_type = 'shed'
    AND oi.status NOT IN ('completed','skipped','canceled','waived','superseded')
    AND oi.scope_id IS DISTINCT FROM g.shed_id
)
UPDATE public.obligation_instances oi
SET scope_id = ego.exact_shed_id,
    updated_at = now()
FROM exact_goat_obligations ego
WHERE oi.tenant_id = ego.tenant_id
  AND oi.obligation_id = ego.obligation_id;

WITH single_exact_batch AS (
  SELECT oi.tenant_id, oi.batch_id, (array_agg(DISTINCT oi.scope_id))[1] AS exact_shed_id
  FROM public.obligation_instances oi
  JOIN public.shed_partitions sp
    ON sp.tenant_id = oi.tenant_id
   AND sp.operational_location_id = oi.scope_id
  WHERE oi.target_type = 'goat'
    AND oi.scope_type = 'shed'
    AND oi.batch_id IS NOT NULL
    AND oi.status NOT IN ('completed','skipped','canceled','waived','superseded')
  GROUP BY oi.tenant_id, oi.batch_id
  HAVING count(DISTINCT oi.scope_id) = 1
)
UPDATE public.obligation_batches ob
SET scope_type = 'shed',
    scope_id = seb.exact_shed_id,
    updated_at = now()
FROM single_exact_batch seb
WHERE ob.tenant_id = seb.tenant_id
  AND ob.batch_id = seb.batch_id
  AND ob.scope_id IS DISTINCT FROM seb.exact_shed_id;

WITH single_exact_task AS (
  SELECT ob.tenant_id, ob.sop_task_id AS task_id, (array_agg(DISTINCT ob.scope_id))[1] AS exact_shed_id
  FROM public.obligation_batches ob
  JOIN public.shed_partitions sp
    ON sp.tenant_id = ob.tenant_id
   AND sp.operational_location_id = ob.scope_id
  WHERE ob.sop_task_id IS NOT NULL
    AND ob.scope_type = 'shed'
  GROUP BY ob.tenant_id, ob.sop_task_id
  HAVING count(DISTINCT ob.scope_id) = 1
)
UPDATE public.sop_tasks st
SET scope_type = 'shed',
    scope_id = setask.exact_shed_id,
    updated_at = now()
FROM single_exact_task setask
WHERE st.tenant_id = setask.tenant_id
  AND st.task_id = setask.task_id
  AND st.scope_id IS DISTINCT FROM setask.exact_shed_id;

-- +goose Down
-- +goose NO TRANSACTION
CREATE OR REPLACE PROCEDURE public.rollback_partition_goat_exact_residence(p_batch_size integer DEFAULT 250)
LANGUAGE plpgsql
AS $$
DECLARE
  moved_count integer;
  remaining_count integer;
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
    IF moved_count = 0 THEN
      SELECT count(*) INTO remaining_count
      FROM public.goats g
      JOIN public.shed_partitions sp
        ON sp.tenant_id = g.tenant_id
       AND sp.operational_location_id = g.shed_id
       AND sp.shed_id = g.shed_group_id
      WHERE g.shed_group_id IS NOT NULL
        AND g.current_location_id = sp.operational_location_id
        AND g.shed_id = sp.operational_location_id;
      EXIT WHEN remaining_count = 0;
      PERFORM pg_sleep(0.2);
    END IF;
  END LOOP;
END;
$$;

CALL public.rollback_partition_goat_exact_residence(250);

DROP PROCEDURE public.rollback_partition_goat_exact_residence(integer);
