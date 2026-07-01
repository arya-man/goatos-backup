-- name: GetObligationByIdempotencyKey :one
SELECT obligation_id::text AS obligation_id, status, due_at, row_version
FROM obligation_instances
WHERE tenant_id = @tenant_id AND idempotency_key = @idempotency_key;

-- name: ListOpenObligationsByGoat :many
-- Goat Passport next-due: a goat's still-actionable obligations, earliest due first. Uses
-- obligation_instances_target_idx (tenant_id, target_type, target_id, status).
SELECT obligation_id::text AS obligation_id, protocol_version_id::text AS protocol_version_id,
       rule_id::text AS rule_id, scope_type, COALESCE(scope_id::text, '')::text AS scope_id,
       COALESCE(batch_id::text, '')::text AS batch_id, due_at, status, "sequence"
FROM obligation_instances
WHERE tenant_id = @tenant_id AND target_type = 'goat' AND target_id = @target_id
  AND status IN ('scheduled', 'due', 'in_progress', 'deferred', 'missed')
ORDER BY due_at ASC, obligation_id ASC
LIMIT @row_limit;

-- name: GetObligationBoosterContext :one
-- SM-7 basis on the verify path: the obligation's version + scope + sequence, looked up by PK
-- (obligation_instances_tenant_id_unique) so the booster can schedule the next dose without the
-- caller threading protocol context through the verification event.
SELECT protocol_version_id::text AS protocol_version_id,
       scope_type, COALESCE(scope_id::text, '')::text AS scope_id, "sequence"
FROM obligation_instances
WHERE tenant_id = @tenant_id AND obligation_id = @obligation_id;

-- name: ListDueObligations :many
-- Due-window scan. Uses obligation_instances_due_window_idx (tenant_id, status, due_at, obligation_id).
SELECT obligation_id::text AS obligation_id, protocol_version_id::text AS protocol_version_id,
       rule_id::text AS rule_id, target_type, target_id::text AS target_id,
       scope_type, COALESCE(scope_id::text, '')::text AS scope_id, due_at, status
FROM obligation_instances
WHERE tenant_id = @tenant_id AND status = @status AND due_at <= @due_before
ORDER BY due_at ASC, obligation_id ASC
LIMIT @row_limit;

-- name: ListUnbatchedDueForVersion :many
-- SM-4 sweeper: unbatched scheduled/due obligations for a version within the window, grouped by
-- scope + rule + due/window downstream. batch_id IS NULL makes re-sweeps idempotent.
-- Uses obligation due-window index.
SELECT obligation_id::text AS obligation_id,
       rule_id::text AS rule_id,
       scope_type,
       COALESCE(scope_id::text, '')::text AS scope_id,
       due_at,
       window_start,
       window_end
FROM obligation_instances
WHERE tenant_id = @tenant_id
  AND protocol_version_id = @protocol_version_id
  AND status IN ('scheduled', 'due')
  AND batch_id IS NULL
  AND due_at <= @due_before
ORDER BY scope_type, scope_id, rule_id, due_at, obligation_id
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
