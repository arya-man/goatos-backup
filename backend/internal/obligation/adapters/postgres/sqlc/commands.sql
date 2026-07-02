-- name: InsertObligationInstance :one
-- Deterministic idempotency_key makes generation a no-op on replay (returns no row on conflict).
-- The logical duplicate guard also prevents pre-canonical-key rows (for example old next_cycle keys)
-- from being duplicated by a later replay with a corrected idempotency key.
INSERT INTO obligation_instances (
  tenant_id, protocol_version_id, rule_id, batch_id, target_type, target_id,
  scope_type, scope_id, due_at, window_start, window_end, status,
  idempotency_key, generated_by_trigger_id, "sequence"
) SELECT
  @tenant_id, @protocol_version_id, @rule_id, @batch_id, @target_type, @target_id,
  @scope_type, @scope_id, @due_at, @window_start, @window_end, @status,
  @idempotency_key, @generated_by_trigger_id, @sequence
WHERE NOT EXISTS (
  SELECT 1
  FROM obligation_instances existing
  WHERE existing.tenant_id = @tenant_id
    AND existing.protocol_version_id = @protocol_version_id
    AND existing.rule_id = @rule_id
    AND existing.target_type = @target_type
    AND existing.target_id = @target_id
    AND existing."sequence" = @sequence
    AND existing.due_at = @due_at
    AND existing.status IN ('scheduled', 'due', 'in_progress', 'deferred', 'missed')
)
ON CONFLICT (tenant_id, idempotency_key) DO NOTHING
RETURNING obligation_id::text AS obligation_id;

-- name: CreateObligationBatch :one
INSERT INTO obligation_batches (
  tenant_id, protocol_version_id, scope_type, scope_id, session, planned_date,
  window_start, window_end, status, estimated_targets, planned_quantity, quantity_unit,
  primary_inventory_lot_id, sop_task_id, conducted_by
) VALUES (
  @tenant_id, @protocol_version_id, @scope_type, @scope_id, @session, @planned_date,
  @window_start, @window_end, @status, @estimated_targets, @planned_quantity, @quantity_unit,
  @primary_inventory_lot_id, @sop_task_id, @conducted_by
)
RETURNING batch_id::text AS batch_id;

-- name: ReserveIdempotencyKey :one
-- Reserve-before-insert guard for status events (cross-partition dedup). Returns the key on
-- first claim; returns no row if already reserved (retry).
INSERT INTO idempotency_keys (idempotency_key, tenant_id, scope, request_hash, status)
VALUES (@idempotency_key, @tenant_id, @scope, @request_hash, 'started')
ON CONFLICT (idempotency_key) DO NOTHING
RETURNING idempotency_key;

-- name: InsertObligationStatusEvent :one
INSERT INTO obligation_status_events (
  tenant_id, obligation_id, event_type, occurred_at, actor_id, payload, idempotency_key
) VALUES (
  @tenant_id, @obligation_id, @event_type, @occurred_at, @actor_id, @payload, @idempotency_key
)
RETURNING obligation_event_id::text AS obligation_event_id;

-- name: AttachObligationsToBatch :execrows
-- Attach a set of still-unbatched obligations to a batch (idempotent: already-batched are skipped).
UPDATE obligation_instances
SET batch_id = @batch_id, updated_at = now()
WHERE tenant_id = @tenant_id
  AND obligation_id = ANY(@obligation_ids::uuid[])
  AND batch_id IS NULL;

-- name: MarkObligationCompleted :execrows
-- SM-5: mark an obligation completed on accepted verification. Late real-world work may complete
-- a previously-missed obligation; the missed event remains in the ledger for audit/as-of views.
-- Idempotent: rows already completed/waived/canceled/superseded are not matched.
UPDATE obligation_instances
SET status = 'completed', completed_at = now(), row_version = row_version + 1, updated_at = now()
WHERE tenant_id = @tenant_id
  AND obligation_id = @obligation_id
  AND status IN ('scheduled', 'due', 'in_progress', 'missed');

-- name: ReScopeOpenObligationsForGoat :many
-- SM-2: on a goat shift, move the goat's still-open, unbatched obligations to the new scope. 'deferred'
-- (held sick/ICU/quarantine) work is re-scoped too — symmetric with SM-3 CancelOpenObligationsForGoat —
-- so when the goat later recovers, ReopenDeferredObligationForKey surfaces the obligation at the goat's
-- CURRENT shed and the SM-4 sweeper batches it under the right drive ("one shed = one drive"); without
-- this a deferred row would reopen at the stale pre-move shed. The IS DISTINCT FROM guard makes a
-- same-scope replay match nothing (no row_version churn → idempotent). Completed/in-progress/missed/
-- canceled and already-batched obligations are never touched (a batched deferred row is mid-drive and
-- must stay with its batch). Uses obligation_instances_target_idx.
UPDATE obligation_instances
SET scope_type = @scope_type, scope_id = @scope_id, row_version = row_version + 1, updated_at = now()
WHERE tenant_id = @tenant_id
  AND target_type = 'goat'
  AND target_id = @target_id
  AND status IN ('scheduled', 'due', 'deferred')
  AND batch_id IS NULL
  AND (scope_type IS DISTINCT FROM @scope_type OR scope_id IS DISTINCT FROM @scope_id)
RETURNING obligation_id::text AS obligation_id;

-- name: CancelOpenObligationsForGoat :many
-- SM-3: cancel a goat's still-open obligations on death/sale/exit. 'deferred' (held sick/ICU/
-- quarantine) work is included so a goat that exits while on hold does not strand open obligations.
-- Idempotent — completed/accepted/missed/already-canceled rows are not matched. Uses
-- obligation_instances_target_idx.
UPDATE obligation_instances
SET status = 'canceled', row_version = row_version + 1, updated_at = now()
WHERE tenant_id = @tenant_id
  AND target_type = 'goat'
  AND target_id = @target_id
  AND status IN ('scheduled', 'due', 'in_progress', 'deferred')
RETURNING obligation_id::text AS obligation_id;

-- name: ReopenDeferredObligationForKey :one
-- Recovery recheck: a previously-deferred (held) obligation becomes schedulable again once the goat
-- is no longer in a defer state (recovered from sick/ICU/quarantine). Clear batch_id defensively so
-- recovery always returns the obligation to the unbatched sweeper path, even if a future execution
-- path deferred a row after it had been attached to a non-planned batch. Idempotent: only rows still
-- 'deferred' match, so a replay after the goat is already schedulable is a no-op.
UPDATE obligation_instances oi
SET status = 'scheduled', batch_id = NULL, row_version = row_version + 1, updated_at = now()
WHERE oi.tenant_id = @tenant_id
  AND oi.idempotency_key = @idempotency_key
  AND oi.status = 'deferred'
  AND NOT EXISTS (
    SELECT 1
    FROM goats g
    JOIN protocol_versions pv
      ON pv.tenant_id = oi.tenant_id
     AND pv.protocol_version_id = oi.protocol_version_id
    JOIN protocol_definitions pd
      ON pd.tenant_id = pv.tenant_id
     AND pd.protocol_id = pv.protocol_id
    WHERE oi.target_type = 'goat'
      AND g.tenant_id = oi.tenant_id
      AND g.goat_id = oi.target_id
      AND pd.category = 'vaccination'
      AND (
        g.lifecycle_status IN ('dead', 'sold', 'lost', 'culled', 'transferred', 'merged', 'inactive')
        OR g.identity_state IN ('disputed', 'merged', 'inactive')
      )
  )
RETURNING oi.obligation_id::text AS obligation_id;

-- name: ReopenDeferredObligationForKeyWithDue :one
-- Health recovery replan: reopen a held obligation and slide due_at to a nearby planned drive date
-- or to recovery time for an immediate micro-drive. Clears batch_id so the sweeper re-attaches.
UPDATE obligation_instances oi
SET status = 'scheduled',
    batch_id = NULL,
    due_at = @rescheduled_due_at,
    window_start = @rescheduled_window_start,
    window_end = @rescheduled_window_end,
    row_version = row_version + 1,
    updated_at = now()
WHERE oi.tenant_id = @tenant_id
  AND oi.idempotency_key = @idempotency_key
  AND oi.status = 'deferred'
  AND NOT EXISTS (
    SELECT 1
    FROM goats g
    JOIN protocol_versions pv
      ON pv.tenant_id = oi.tenant_id
     AND pv.protocol_version_id = oi.protocol_version_id
    JOIN protocol_definitions pd
      ON pd.tenant_id = pv.tenant_id
     AND pd.protocol_id = pv.protocol_id
    WHERE oi.target_type = 'goat'
      AND g.tenant_id = oi.tenant_id
      AND g.goat_id = oi.target_id
      AND pd.category = 'vaccination'
      AND (
        g.lifecycle_status IN ('dead', 'sold', 'lost', 'culled', 'transferred', 'merged', 'inactive')
        OR g.identity_state IN ('disputed', 'merged', 'inactive')
      )
  )
RETURNING oi.obligation_id::text AS obligation_id;

-- name: CompleteIdempotencyKey :exec
UPDATE idempotency_keys
SET status = 'completed', result_type = @result_type, result_id = @result_id, completed_at = now()
WHERE idempotency_key = @idempotency_key;
