-- name: InsertObligationInstance :one
-- Deterministic idempotency_key makes generation a no-op on replay (returns no row on conflict).
INSERT INTO obligation_instances (
  tenant_id, protocol_version_id, rule_id, batch_id, target_type, target_id,
  scope_type, scope_id, due_at, window_start, window_end, status,
  idempotency_key, generated_by_trigger_id, "sequence"
) VALUES (
  @tenant_id, @protocol_version_id, @rule_id, @batch_id, @target_type, @target_id,
  @scope_type, @scope_id, @due_at, @window_start, @window_end, @status,
  @idempotency_key, @generated_by_trigger_id, @sequence
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
-- SM-5: mark an obligation completed on accepted verification. Idempotent: a row already terminal
-- (completed/missed/waived/canceled/superseded) is not matched, so a re-run completes nothing.
UPDATE obligation_instances
SET status = 'completed', completed_at = now(), row_version = row_version + 1, updated_at = now()
WHERE tenant_id = @tenant_id
  AND obligation_id = @obligation_id
  AND status IN ('scheduled', 'due', 'in_progress');

-- name: CancelOpenObligationsForGoat :many
-- SM-3: cancel a goat's still-open obligations on death/sale. Idempotent — completed/accepted/
-- missed/already-canceled rows are not matched. Uses obligation_instances_target_idx.
UPDATE obligation_instances
SET status = 'canceled', row_version = row_version + 1, updated_at = now()
WHERE tenant_id = @tenant_id
  AND target_type = 'goat'
  AND target_id = @target_id
  AND status IN ('scheduled', 'due')
RETURNING obligation_id::text AS obligation_id;

-- name: CompleteIdempotencyKey :exec
UPDATE idempotency_keys
SET status = 'completed', result_type = @result_type, result_id = @result_id, completed_at = now()
WHERE idempotency_key = @idempotency_key;
