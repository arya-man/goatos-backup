-- name: GetObligationByIdempotencyKey :one
SELECT obligation_id::text AS obligation_id, status, due_at, row_version
FROM obligation_instances
WHERE tenant_id = @tenant_id AND idempotency_key = @idempotency_key;

-- name: ListDueObligations :many
-- Due-window scan. Uses obligation_instances_due_window_idx (tenant_id, status, due_at, obligation_id).
SELECT obligation_id::text AS obligation_id, protocol_version_id::text AS protocol_version_id,
       rule_id::text AS rule_id, target_type, target_id::text AS target_id,
       scope_type, COALESCE(scope_id::text, '')::text AS scope_id, due_at, status
FROM obligation_instances
WHERE tenant_id = @tenant_id AND status = @status AND due_at <= @due_before
ORDER BY due_at ASC, obligation_id ASC
LIMIT @row_limit;

-- name: CountObligationsByScope :one
-- Per-scope rollup. Uses obligation_instances_scope_idx (tenant_id, scope_type, scope_id, status, due_at).
SELECT COUNT(*)::bigint AS total
FROM obligation_instances
WHERE tenant_id = @tenant_id AND scope_type = @scope_type
  AND scope_id = @scope_id AND status = @status;

-- name: IdempotencyKeyStatus :one
SELECT status, COALESCE(result_type, '')::text AS result_type, COALESCE(result_id::text, '')::text AS result_id
FROM idempotency_keys
WHERE idempotency_key = @idempotency_key;
