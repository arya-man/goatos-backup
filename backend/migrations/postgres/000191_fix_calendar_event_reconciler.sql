-- +goose Up
/*
CR-004 (calendar-canonical-5k50k review): fix goatos_reconcile_calendar_event_references
(migration 000189).

Bug: the function compared the RAW UUID text of a canonical row's primary key
(oi.obligation_id::text, ob.batch_id::text, st.task_id::text) directly against
notification_requests.calendar_event_id / calendar_snoozes.calendar_event_id --
but every writer of those columns stores a TYPED, prefixed identity
(canonical_read.go's calendarCanonicalEventsCTE / park_drive_events): e.g.
'obligation:<uuid>', 'batch:<uuid>', 'parkdrive:park:<uuid>:date:<day>',
'parkdrive:tenant:<uuid>:date:<day>', 'catchup:...', 'completion:<uuid>', or
'calendar:<uuid>' (sop_task or draft protocol-version review). The raw-uuid
comparison NEVER matched any of these, so the reconciler reported every valid
row as orphaned -- a 100% false-positive rate, making it useless as an
integrity check.

Fix: goatos_calendar_event_reference_valid(tenant, calendar_event_id) parses
the type prefix first, then checks the matching canonical table (or, for the
aggregate park-drive/catch-up identities, whether any canonical work still
resolves to that park/shed + business-date + optional rule via the new
goatos_park_day_has_vaccination_work helper, mirroring the same membership
resolution canonical_read.go's obligation_drive_membership CTE and
targets.go's calendarDriveTargetsSQL use for the live read/roster paths). The
reconciler itself now delegates to that per-row validity check instead of
its old raw-uuid EXISTS predicates, and supports every real canonical event_id
shape currently in use (obligation:, batch:, parkdrive:park:/parkdrive:tenant:,
catchup: variants, completion:, calendar:) -- not just obligation/batch/task.

Scheduling: this remains a callable integrity check, not yet wired into any
recurring worker stage. backend/internal/calendar/app/service.go now exposes
Service.ReconcileEventReferences (backed by
Repository.ReconcileEventReferences in
backend/internal/calendar/adapters/postgres/reconciler.go) as the Go-callable
seam for whichever agent owns cmd/kernel-worker/housekeeping stage wiring --
see that file's doc comment for the exact kernelstages.*ReconcilerStage
pattern (e.g. InventoryBatchReconcilerStage) to mirror when wiring it in.

Lock safety: CREATE OR REPLACE FUNCTION is catalog-only (no table rewrite, no
scan, no lock on any data table) -- safe at any obligation/notification volume.

No seed-path impact: this migration only replaces plpgsql function bodies; it
adds/renames no column any seed command writes.
*/

CREATE OR REPLACE FUNCTION goatos_park_day_has_vaccination_work(
  p_tenant_id uuid,
  p_park_id uuid,
  p_shed_id uuid,
  p_due_day date,
  p_rule_id uuid DEFAULT NULL
) RETURNS boolean LANGUAGE sql STABLE AS $$
  -- Unbatched obligations due that business-date, resolved to the requested park/shed/tenant scope
  -- (mirrors targets.go's calendarDriveTargetsSQL unbatched branch and
  -- canonical_read.go's obligation_drive_membership unbatched branches).
  SELECT EXISTS (
    SELECT 1
    FROM obligation_instances oi
    LEFT JOIN locations scope_loc
      ON scope_loc.tenant_id = oi.tenant_id
     AND scope_loc.location_id = oi.scope_id
     AND oi.scope_type IN ('park', 'shed', 'cohort')
    LEFT JOIN locations scope_parent
      ON scope_parent.tenant_id = oi.tenant_id
     AND scope_parent.location_id = scope_loc.parent_location_id
    WHERE oi.tenant_id = p_tenant_id
      AND oi.batch_id IS NULL
      AND oi.status NOT IN ('waived', 'canceled', 'superseded')
      AND (oi.due_at AT TIME ZONE 'Asia/Kolkata')::date = p_due_day
      AND (p_rule_id IS NULL OR oi.rule_id = p_rule_id)
      AND (
        (p_park_id IS NOT NULL AND (
          (oi.scope_type = 'park' AND scope_loc.location_id = p_park_id)
          OR (oi.scope_type IN ('shed', 'cohort') AND scope_parent.location_id = p_park_id)
        ))
        OR (p_shed_id IS NOT NULL AND (
          (oi.scope_type = 'shed' AND scope_loc.location_id = p_shed_id)
          OR (oi.scope_type = 'cohort' AND scope_parent.location_id = p_shed_id)
        ))
        OR (p_park_id IS NULL AND p_shed_id IS NULL AND scope_loc.location_id IS NULL)
      )
  )
  -- Batched drives whose batch resolves to that park+business-date (mirrors targets.go's
  -- matched_batches CTE: batch scope is resolved from obligation_batches/locations, NOT the member
  -- obligation's own oi.scope_type/scope_id, since AttachObligationsToBatch never syncs it).
  OR EXISTS (
    SELECT 1
    FROM obligation_batches ob
    LEFT JOIN locations shed_loc
      ON shed_loc.tenant_id = ob.tenant_id AND shed_loc.location_id = ob.scope_id
    WHERE ob.tenant_id = p_tenant_id
      AND ob.status NOT IN ('superseded', 'canceled')
      AND (COALESCE(ob.window_start, ob.planned_date::timestamptz, ob.window_end) AT TIME ZONE 'Asia/Kolkata')::date = p_due_day
      AND (
        (p_park_id IS NOT NULL AND shed_loc.parent_location_id = p_park_id)
        OR (p_shed_id IS NOT NULL AND ob.scope_id = p_shed_id)
        OR (p_park_id IS NULL AND p_shed_id IS NULL AND shed_loc.parent_location_id IS NULL)
      )
  );
$$;

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

  -- obligation:<uuid> -- individual vaccination_dose_due obligation events (reminder-cadence fires,
  -- the obligation-scoped escalation lookup).
  m := regexp_match(p_calendar_event_id, '^obligation:([0-9a-fA-F-]{36})$');
  IF m IS NOT NULL THEN
    RETURN EXISTS (
      SELECT 1 FROM obligation_instances oi
      WHERE oi.tenant_id = p_tenant_id AND oi.obligation_id = m[1]::uuid
    );
  END IF;

  -- batch:<uuid> -- a resolvable MEMBERSHIP identity (canonical_read.go's park_drive_events no
  -- longer assigns this as a NEW drive's own identity post CR-002/CR-003, but rows written before
  -- that fix, or any other legacy writer, may still carry it).
  m := regexp_match(p_calendar_event_id, '^batch:([0-9a-fA-F-]{36})$');
  IF m IS NOT NULL THEN
    RETURN EXISTS (
      SELECT 1 FROM obligation_batches ob
      WHERE ob.tenant_id = p_tenant_id AND ob.batch_id = m[1]::uuid
    );
  END IF;

  -- completion:<uuid> -- accepted vaccination_history_events completion rows.
  m := regexp_match(p_calendar_event_id, '^completion:([0-9a-fA-F-]{36})$');
  IF m IS NOT NULL THEN
    RETURN EXISTS (
      SELECT 1 FROM vaccination_completions vc
      WHERE vc.tenant_id = p_tenant_id AND vc.completion_id = m[1]::uuid
    );
  END IF;

  -- calendar:<uuid> -- overloaded prefix shared by sop_events (sop_task_id) and config_events
  -- (protocol_version_id); either canonical table backing it makes the reference valid.
  m := regexp_match(p_calendar_event_id, '^calendar:([0-9a-fA-F-]{36})$');
  IF m IS NOT NULL THEN
    RETURN EXISTS (
      SELECT 1 FROM sop_tasks st WHERE st.tenant_id = p_tenant_id AND st.task_id = m[1]::uuid
    ) OR EXISTS (
      SELECT 1 FROM protocol_versions pv WHERE pv.tenant_id = p_tenant_id AND pv.protocol_version_id = m[1]::uuid
    );
  END IF;

  -- parkdrive:park:<uuid>:date:<day> -- CR-002/CR-003 stable park/day drive identity.
  m := regexp_match(p_calendar_event_id, '^parkdrive:park:([0-9a-fA-F-]{36}):date:(\d{4}-\d{2}-\d{2})$');
  IF m IS NOT NULL THEN
    RETURN goatos_park_day_has_vaccination_work(p_tenant_id, m[1]::uuid, NULL, m[2]::date);
  END IF;

  -- parkdrive:tenant:<uuid>:date:<day> -- tenant-wide park-drive aggregate (no resolvable park).
  m := regexp_match(p_calendar_event_id, '^parkdrive:tenant:([0-9a-fA-F-]{36}):date:(\d{4}-\d{2}-\d{2})$');
  IF m IS NOT NULL THEN
    RETURN goatos_park_day_has_vaccination_work(p_tenant_id, NULL, NULL, m[2]::date);
  END IF;

  -- catchup:park:<uuid>:due:<day> -- unbatched catch-up, park-scoped.
  m := regexp_match(p_calendar_event_id, '^catchup:park:([0-9a-fA-F-]{36}):due:(\d{4}-\d{2}-\d{2})$');
  IF m IS NOT NULL THEN
    RETURN goatos_park_day_has_vaccination_work(p_tenant_id, m[1]::uuid, NULL, m[2]::date);
  END IF;

  -- catchup:tenant:<uuid>:due:<day> -- unbatched catch-up, tenant-wide.
  m := regexp_match(p_calendar_event_id, '^catchup:tenant:([0-9a-fA-F-]{36}):due:(\d{4}-\d{2}-\d{2})$');
  IF m IS NOT NULL THEN
    RETURN goatos_park_day_has_vaccination_work(p_tenant_id, NULL, NULL, m[2]::date);
  END IF;

  -- catchup:shed:<uuid>:rule:<uuid>:due:<day> -- unbatched catch-up, shed+rule-scoped.
  m := regexp_match(p_calendar_event_id, '^catchup:shed:([0-9a-fA-F-]{36}):rule:([0-9a-fA-F-]{36}):due:(\d{4}-\d{2}-\d{2})$');
  IF m IS NOT NULL THEN
    RETURN goatos_park_day_has_vaccination_work(p_tenant_id, NULL, m[1]::uuid, m[3]::date, m[2]::uuid);
  END IF;

  -- catchup:tenant:<uuid>:rule:<uuid>:due:<day> -- unbatched catch-up, tenant+rule-scoped.
  m := regexp_match(p_calendar_event_id, '^catchup:tenant:([0-9a-fA-F-]{36}):rule:([0-9a-fA-F-]{36}):due:(\d{4}-\d{2}-\d{2})$');
  IF m IS NOT NULL THEN
    RETURN goatos_park_day_has_vaccination_work(p_tenant_id, NULL, NULL, m[3]::date, m[2]::uuid);
  END IF;

  -- Unrecognized shape: not one of the canonical naming conventions above -- flag it rather than
  -- silently accept it (a genuine typo/legacy/foreign id should surface as an orphan for review).
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

-- +goose Down
-- Catalog-only revert: restore the pre-000191 (raw-uuid-comparison) reconciler body from
-- migration 000189, and drop the two new helper functions this migration introduced.
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
    'orphaned calendar_event_id: does not map to any canonical obligation/batch/task'::text
  FROM notification_requests nr
  WHERE nr.tenant_id = p_tenant_id
    AND nr.calendar_event_id IS NOT NULL
    AND NOT EXISTS (
      SELECT 1 FROM obligation_instances oi
      WHERE oi.tenant_id = nr.tenant_id AND oi.obligation_id::text = nr.calendar_event_id
    )
    AND NOT EXISTS (
      SELECT 1 FROM obligation_batches ob
      WHERE ob.tenant_id = nr.tenant_id AND ob.batch_id::text = nr.calendar_event_id
    )
    AND NOT EXISTS (
      SELECT 1 FROM sop_tasks st
      WHERE st.tenant_id = nr.tenant_id AND st.task_id::text = nr.calendar_event_id
    );

  RETURN QUERY
  SELECT
    'calendar_snoozes'::text AS source_table,
    cs.snooze_id,
    cs.calendar_event_id,
    'orphaned calendar_event_id: does not map to any canonical obligation/batch/task'::text
  FROM calendar_snoozes cs
  WHERE cs.tenant_id = p_tenant_id
    AND cs.calendar_event_id IS NOT NULL
    AND NOT EXISTS (
      SELECT 1 FROM obligation_instances oi
      WHERE oi.tenant_id = cs.tenant_id AND oi.obligation_id::text = cs.calendar_event_id
    )
    AND NOT EXISTS (
      SELECT 1 FROM obligation_batches ob
      WHERE ob.tenant_id = cs.tenant_id AND ob.batch_id::text = cs.calendar_event_id
    )
    AND NOT EXISTS (
      SELECT 1 FROM sop_tasks st
      WHERE st.tenant_id = cs.tenant_id AND st.task_id::text = cs.calendar_event_id
    );
END;
$$;

DROP FUNCTION IF EXISTS goatos_calendar_event_reference_valid(uuid, text);

DROP FUNCTION IF EXISTS goatos_park_day_has_vaccination_work(uuid, uuid, uuid, date, uuid);
