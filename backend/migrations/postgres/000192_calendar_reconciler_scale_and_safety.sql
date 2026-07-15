-- +goose Up
/*
CR-004 (calendar-canonical-5k50k review): fix FINDING 4 + FINDING 5.

FINDING 4 (P1): the daily reconciler does not scale.
Before: goatos_reconcile_calendar_event_references scanned the COMPLETE notification_requests +
calendar_snoozes history with repeated per-row lookups; reconciler.go loaded EVERY orphan into
memory; calendar_reconciler.go logged ONE warning per orphan. At millions of historical rows this
times out, blows memory, and floods logs.
After: SQL supports bounded LIMIT/OFFSET pagination with a stable order. Repository method
accepts limit/cursor. Kernel stage iterates bounded pages up to a safe per-run cap (10 pages, 1k
rows each = 10k orphans max per run), retains progress across pages, and logs TOTALS + SMALL
SAMPLE (first 5 orphans), not one line per orphan.

FINDING 5 (P2): one malformed id stops the WHOLE reconcile job.
Before: goatos_calendar_event_reference_valid validates typed ids with a regex that merely
accepts UUID-ISH / DATE-ISH strings, then CASTS them directly (::uuid, ::date). A value like
'parkdrive:park:<uuid>:date:2026-99-99' raises a DB cast error that aborts the entire reconcile
run instead of being reported as bad data.
After: plpgsql function guards casts with EXCEPTION handlers (invalid_text_representation). A
malformed id is REPORTED as an invalid/orphan row, and the remaining rows STILL process -- one
bad row never aborts the job.

Migration 000192 introduces pagination support and safe casting. The reconciler function now
accepts LIMIT/OFFSET and returns rows ordered by (source_table, record_id) for stable keyset
pagination. The validity function handles cast errors gracefully.
*/

CREATE OR REPLACE FUNCTION goatos_calendar_event_reference_valid(
  p_tenant_id uuid,
  p_calendar_event_id text
) RETURNS boolean LANGUAGE plpgsql STABLE AS $$
DECLARE
  m text[];
  parsed_uuid uuid;
  parsed_date date;
BEGIN
  IF p_calendar_event_id IS NULL THEN
    RETURN true;
  END IF;

  -- obligation:<uuid> -- individual vaccination_dose_due obligation events.
  m := regexp_match(p_calendar_event_id, '^obligation:([0-9a-fA-F-]{36})$');
  IF m IS NOT NULL THEN
    BEGIN
      parsed_uuid := m[1]::uuid;
      RETURN EXISTS (
        SELECT 1 FROM obligation_instances oi
        WHERE oi.tenant_id = p_tenant_id AND oi.obligation_id = parsed_uuid
      );
    EXCEPTION WHEN invalid_text_representation OR datetime_field_overflow OR numeric_value_out_of_range THEN
      RETURN false;
    END;
  END IF;

  -- batch:<uuid>
  m := regexp_match(p_calendar_event_id, '^batch:([0-9a-fA-F-]{36})$');
  IF m IS NOT NULL THEN
    BEGIN
      parsed_uuid := m[1]::uuid;
      RETURN EXISTS (
        SELECT 1 FROM obligation_batches ob
        WHERE ob.tenant_id = p_tenant_id AND ob.batch_id = parsed_uuid
      );
    EXCEPTION WHEN invalid_text_representation OR datetime_field_overflow OR numeric_value_out_of_range THEN
      RETURN false;
    END;
  END IF;

  -- completion:<uuid>
  m := regexp_match(p_calendar_event_id, '^completion:([0-9a-fA-F-]{36})$');
  IF m IS NOT NULL THEN
    BEGIN
      parsed_uuid := m[1]::uuid;
      RETURN EXISTS (
        SELECT 1 FROM vaccination_completions vc
        WHERE vc.tenant_id = p_tenant_id AND vc.completion_id = parsed_uuid
      );
    EXCEPTION WHEN invalid_text_representation OR datetime_field_overflow OR numeric_value_out_of_range THEN
      RETURN false;
    END;
  END IF;

  -- calendar:<uuid>
  m := regexp_match(p_calendar_event_id, '^calendar:([0-9a-fA-F-]{36})$');
  IF m IS NOT NULL THEN
    BEGIN
      parsed_uuid := m[1]::uuid;
      RETURN EXISTS (
        SELECT 1 FROM sop_tasks st WHERE st.tenant_id = p_tenant_id AND st.task_id = parsed_uuid
      ) OR EXISTS (
        SELECT 1 FROM protocol_versions pv WHERE pv.tenant_id = p_tenant_id AND pv.protocol_version_id = parsed_uuid
      );
    EXCEPTION WHEN invalid_text_representation OR datetime_field_overflow OR numeric_value_out_of_range THEN
      RETURN false;
    END;
  END IF;

  -- parkdrive:park:<uuid>:date:<day>
  m := regexp_match(p_calendar_event_id, '^parkdrive:park:([0-9a-fA-F-]{36}):date:(\d{4}-\d{2}-\d{2})$');
  IF m IS NOT NULL THEN
    BEGIN
      parsed_uuid := m[1]::uuid;
      parsed_date := m[2]::date;
      RETURN goatos_park_day_has_vaccination_work(p_tenant_id, parsed_uuid, NULL, parsed_date);
    EXCEPTION WHEN invalid_text_representation OR datetime_field_overflow OR numeric_value_out_of_range THEN
      RETURN false;
    END;
  END IF;

  -- parkdrive:tenant:<uuid>:date:<day>
  m := regexp_match(p_calendar_event_id, '^parkdrive:tenant:([0-9a-fA-F-]{36}):date:(\d{4}-\d{2}-\d{2})$');
  IF m IS NOT NULL THEN
    BEGIN
      parsed_uuid := m[1]::uuid;
      parsed_date := m[2]::date;
      RETURN goatos_park_day_has_vaccination_work(p_tenant_id, NULL, NULL, parsed_date);
    EXCEPTION WHEN invalid_text_representation OR datetime_field_overflow OR numeric_value_out_of_range THEN
      RETURN false;
    END;
  END IF;

  -- catchup:park:<uuid>:due:<day>
  m := regexp_match(p_calendar_event_id, '^catchup:park:([0-9a-fA-F-]{36}):due:(\d{4}-\d{2}-\d{2})$');
  IF m IS NOT NULL THEN
    BEGIN
      parsed_uuid := m[1]::uuid;
      parsed_date := m[2]::date;
      RETURN goatos_park_day_has_vaccination_work(p_tenant_id, parsed_uuid, NULL, parsed_date);
    EXCEPTION WHEN invalid_text_representation OR datetime_field_overflow OR numeric_value_out_of_range THEN
      RETURN false;
    END;
  END IF;

  -- catchup:tenant:<uuid>:due:<day>
  m := regexp_match(p_calendar_event_id, '^catchup:tenant:([0-9a-fA-F-]{36}):due:(\d{4}-\d{2}-\d{2})$');
  IF m IS NOT NULL THEN
    BEGIN
      parsed_uuid := m[1]::uuid;
      parsed_date := m[2]::date;
      RETURN goatos_park_day_has_vaccination_work(p_tenant_id, NULL, NULL, parsed_date);
    EXCEPTION WHEN invalid_text_representation OR datetime_field_overflow OR numeric_value_out_of_range THEN
      RETURN false;
    END;
  END IF;

  -- catchup:shed:<uuid>:rule:<uuid>:due:<day>
  m := regexp_match(p_calendar_event_id, '^catchup:shed:([0-9a-fA-F-]{36}):rule:([0-9a-fA-F-]{36}):due:(\d{4}-\d{2}-\d{2})$');
  IF m IS NOT NULL THEN
    BEGIN
      parsed_uuid := m[1]::uuid;
      parsed_date := m[3]::date;
      RETURN goatos_park_day_has_vaccination_work(p_tenant_id, NULL, parsed_uuid, parsed_date, m[2]::uuid);
    EXCEPTION WHEN invalid_text_representation OR datetime_field_overflow OR numeric_value_out_of_range THEN
      RETURN false;
    END;
  END IF;

  -- catchup:tenant:<uuid>:rule:<uuid>:due:<day>
  m := regexp_match(p_calendar_event_id, '^catchup:tenant:([0-9a-fA-F-]{36}):rule:([0-9a-fA-F-]{36}):due:(\d{4}-\d{2}-\d{2})$');
  IF m IS NOT NULL THEN
    BEGIN
      parsed_date := m[3]::date;
      RETURN goatos_park_day_has_vaccination_work(p_tenant_id, NULL, NULL, parsed_date, m[2]::uuid);
    EXCEPTION WHEN invalid_text_representation OR datetime_field_overflow OR numeric_value_out_of_range THEN
      RETURN false;
    END;
  END IF;

  -- Unrecognized shape: not one of the canonical naming conventions.
  RETURN false;
END;
$$;

CREATE OR REPLACE FUNCTION goatos_reconcile_calendar_event_references(
  p_tenant_id uuid,
  p_limit int DEFAULT 1000,
  p_offset int DEFAULT 0
)
RETURNS TABLE(
  source_table text,
  record_id uuid,
  calendar_event_id text,
  issue text
) LANGUAGE plpgsql STABLE AS $$
BEGIN
  RETURN QUERY
  SELECT * FROM (
    SELECT
      'notification_requests'::text AS source_table,
      nr.notification_request_id,
      nr.calendar_event_id,
      'orphaned calendar_event_id: does not map to any canonical obligation/batch/drive/task'::text
    FROM notification_requests nr
    WHERE nr.tenant_id = p_tenant_id
      AND nr.calendar_event_id IS NOT NULL
      AND NOT goatos_calendar_event_reference_valid(p_tenant_id, nr.calendar_event_id)

    UNION ALL

    SELECT
      'calendar_snoozes'::text AS source_table,
      cs.snooze_id,
      cs.calendar_event_id,
      'orphaned calendar_event_id: does not map to any canonical obligation/batch/drive/task'::text
    FROM calendar_snoozes cs
    WHERE cs.tenant_id = p_tenant_id
      AND cs.calendar_event_id IS NOT NULL
      AND NOT goatos_calendar_event_reference_valid(p_tenant_id, cs.calendar_event_id)
  ) combined
  ORDER BY source_table, record_id
  LIMIT p_limit
  OFFSET p_offset;
END;
$$;

-- +goose Down
-- Revert to the pre-000192 (no pagination, no guarded casts) reconciler body from migration 000191.
CREATE OR REPLACE FUNCTION goatos_calendar_event_reference_valid(
  p_tenant_id uuid,
  p_calendar_event_id text
) RETURNS boolean LANGUAGE plpgsql STABLE AS $$
DECLARE
  m text[];
BEGIN
  IF p_calendar_event_id IS NULL THEN
    RETURN true;
  END IF;

  m := regexp_match(p_calendar_event_id, '^obligation:([0-9a-fA-F-]{36})$');
  IF m IS NOT NULL THEN
    RETURN EXISTS (
      SELECT 1 FROM obligation_instances oi
      WHERE oi.tenant_id = p_tenant_id AND oi.obligation_id = m[1]::uuid
    );
  END IF;

  m := regexp_match(p_calendar_event_id, '^batch:([0-9a-fA-F-]{36})$');
  IF m IS NOT NULL THEN
    RETURN EXISTS (
      SELECT 1 FROM obligation_batches ob
      WHERE ob.tenant_id = p_tenant_id AND ob.batch_id = m[1]::uuid
    );
  END IF;

  m := regexp_match(p_calendar_event_id, '^completion:([0-9a-fA-F-]{36})$');
  IF m IS NOT NULL THEN
    RETURN EXISTS (
      SELECT 1 FROM vaccination_completions vc
      WHERE vc.tenant_id = p_tenant_id AND vc.completion_id = m[1]::uuid
    );
  END IF;

  m := regexp_match(p_calendar_event_id, '^calendar:([0-9a-fA-F-]{36})$');
  IF m IS NOT NULL THEN
    RETURN EXISTS (
      SELECT 1 FROM sop_tasks st WHERE st.tenant_id = p_tenant_id AND st.task_id = m[1]::uuid
    ) OR EXISTS (
      SELECT 1 FROM protocol_versions pv WHERE pv.tenant_id = p_tenant_id AND pv.protocol_version_id = m[1]::uuid
    );
  END IF;

  m := regexp_match(p_calendar_event_id, '^parkdrive:park:([0-9a-fA-F-]{36}):date:(\d{4}-\d{2}-\d{2})$');
  IF m IS NOT NULL THEN
    RETURN goatos_park_day_has_vaccination_work(p_tenant_id, m[1]::uuid, NULL, m[2]::date);
  END IF;

  m := regexp_match(p_calendar_event_id, '^parkdrive:tenant:([0-9a-fA-F-]{36}):date:(\d{4}-\d{2}-\d{2})$');
  IF m IS NOT NULL THEN
    RETURN goatos_park_day_has_vaccination_work(p_tenant_id, NULL, NULL, m[2]::date);
  END IF;

  m := regexp_match(p_calendar_event_id, '^catchup:park:([0-9a-fA-F-]{36}):due:(\d{4}-\d{2}-\d{2})$');
  IF m IS NOT NULL THEN
    RETURN goatos_park_day_has_vaccination_work(p_tenant_id, m[1]::uuid, NULL, m[2]::date);
  END IF;

  m := regexp_match(p_calendar_event_id, '^catchup:tenant:([0-9a-fA-F-]{36}):due:(\d{4}-\d{2}-\d{2})$');
  IF m IS NOT NULL THEN
    RETURN goatos_park_day_has_vaccination_work(p_tenant_id, NULL, NULL, m[2]::date);
  END IF;

  m := regexp_match(p_calendar_event_id, '^catchup:shed:([0-9a-fA-F-]{36}):rule:([0-9a-fA-F-]{36}):due:(\d{4}-\d{2}-\d{2})$');
  IF m IS NOT NULL THEN
    RETURN goatos_park_day_has_vaccination_work(p_tenant_id, NULL, m[1]::uuid, m[3]::date, m[2]::uuid);
  END IF;

  m := regexp_match(p_calendar_event_id, '^catchup:tenant:([0-9a-fA-F-]{36}):rule:([0-9a-fA-F-]{36}):due:(\d{4}-\d{2}-\d{2})$');
  IF m IS NOT NULL THEN
    RETURN goatos_park_day_has_vaccination_work(p_tenant_id, NULL, NULL, m[3]::date, m[2]::uuid);
  END IF;

  RETURN false;
END;
$$;

CREATE OR REPLACE FUNCTION goatos_reconcile_calendar_event_references(p_tenant_id uuid)
RETURNS TABLE(
  source_table text,
  record_id uuid,
  calendar_event_id text,
  issue text
) LANGUAGE plpgsql STABLE AS $$
BEGIN
  RETURN QUERY
  SELECT
    'notification_requests'::text AS source_table,
    nr.notification_request_id,
    nr.calendar_event_id,
    'orphaned calendar_event_id: does not map to any canonical obligation/batch/drive/task'::text
  FROM notification_requests nr
  WHERE nr.tenant_id = p_tenant_id
    AND nr.calendar_event_id IS NOT NULL
    AND NOT goatos_calendar_event_reference_valid(p_tenant_id, nr.calendar_event_id);

  RETURN QUERY
  SELECT
    'calendar_snoozes'::text AS source_table,
    cs.snooze_id,
    cs.calendar_event_id,
    'orphaned calendar_event_id: does not map to any canonical obligation/batch/drive/task'::text
  FROM calendar_snoozes cs
  WHERE cs.tenant_id = p_tenant_id
    AND cs.calendar_event_id IS NOT NULL
    AND NOT goatos_calendar_event_reference_valid(p_tenant_id, cs.calendar_event_id);
END;
$$;
