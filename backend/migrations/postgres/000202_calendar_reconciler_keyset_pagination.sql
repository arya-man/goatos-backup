-- +goose Up
/*
CAL-MAIN-03 (P1): the daily calendar reconciler can permanently miss records.

Before: goatos_reconcile_calendar_event_references used LIMIT/OFFSET pagination
(migration 000192). The kernel stage restarts at OFFSET 0 on every daily run and
caps itself at a fixed page budget, so records ordered beyond that per-run cap are
rescanned-from-the-top-and-then-dropped every run -- they are never inspected.
OFFSET also rescans and discards all skipped rows, which is O(n^2) across pages at
notification_requests + calendar_snoozes history scale.

After: the function takes a stable keyset cursor -- the last (source_table,
record_id) pair returned -- and returns the next rows strictly greater than that
cursor in (source_table, record_id) order. Passing ('', '') starts from the
beginning. The caller (reconciler.go ReconcileEventReferencesPage) persists the
cursor across runs so every record is eventually inspected with no rescanning and
guaranteed forward progress.

This supersedes the LIMIT/OFFSET overload from 000192. The (uuid, int, int) and
legacy (uuid) overloads are dropped so callers resolve unambiguously to the keyset
signature.
*/

DROP FUNCTION IF EXISTS goatos_reconcile_calendar_event_references(uuid, int, int);
DROP FUNCTION IF EXISTS goatos_reconcile_calendar_event_references(uuid);

CREATE OR REPLACE FUNCTION goatos_reconcile_calendar_event_references(
  p_tenant_id uuid,
  p_cursor_source_table text DEFAULT '',
  p_cursor_record_id text DEFAULT '',
  p_limit int DEFAULT 1000
)
RETURNS TABLE(
  source_table text,
  record_id uuid,
  calendar_event_id text,
  issue text
) LANGUAGE plpgsql STABLE AS $$
DECLARE
  v_cursor_record_id uuid;
BEGIN
  -- Empty/NULL cursor record id means "start of this source_table" (or overall
  -- start when the source_table cursor is empty too).
  IF p_cursor_record_id IS NULL OR p_cursor_record_id = '' THEN
    v_cursor_record_id := NULL;
  ELSE
    v_cursor_record_id := p_cursor_record_id::uuid;
  END IF;

  RETURN QUERY
  SELECT * FROM (
    SELECT
      'notification_requests'::text AS source_table,
      nr.notification_request_id AS record_id,
      nr.calendar_event_id AS calendar_event_id,
      'orphaned calendar_event_id: does not map to any canonical obligation/batch/drive/task'::text AS issue
    FROM notification_requests nr
    WHERE nr.tenant_id = p_tenant_id
      AND nr.calendar_event_id IS NOT NULL
      AND NOT goatos_calendar_event_reference_valid(p_tenant_id, nr.calendar_event_id)

    UNION ALL

    SELECT
      'calendar_snoozes'::text AS source_table,
      cs.snooze_id AS record_id,
      cs.calendar_event_id AS calendar_event_id,
      'orphaned calendar_event_id: does not map to any canonical obligation/batch/drive/task'::text AS issue
    FROM calendar_snoozes cs
    WHERE cs.tenant_id = p_tenant_id
      AND cs.calendar_event_id IS NOT NULL
      AND NOT goatos_calendar_event_reference_valid(p_tenant_id, cs.calendar_event_id)
  ) combined
  WHERE
    -- Keyset: strictly greater than the cursor in (source_table, record_id) order.
    p_cursor_source_table = ''
    OR combined.source_table > p_cursor_source_table
    OR (
      combined.source_table = p_cursor_source_table
      AND v_cursor_record_id IS NOT NULL
      AND combined.record_id > v_cursor_record_id
    )
  ORDER BY combined.source_table, combined.record_id
  LIMIT p_limit;
END;
$$;

-- +goose Down
-- Restore the 000192 LIMIT/OFFSET pagination overload and drop the keyset version.
DROP FUNCTION IF EXISTS goatos_reconcile_calendar_event_references(uuid, text, text, int);

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
