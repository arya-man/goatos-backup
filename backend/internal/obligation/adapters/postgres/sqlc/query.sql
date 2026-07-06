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
-- Uses obligation_instances_unbatched_due_version_idx.
-- Clinical blocks and location quarantine/ICU goats are excluded at sweep time.
SELECT oi.obligation_id::text AS obligation_id,
       oi.rule_id::text AS rule_id,
       oi.scope_type,
       COALESCE(oi.scope_id::text, '')::text AS scope_id,
       CASE
         WHEN oi.target_type = 'goat' THEN COALESCE(g.species, 'goat')::text
         ELSE ''
       END AS target_species,
       COALESCE(g.stage, '')::text AS target_animal_stage,
       oi.due_at,
       oi.window_start,
       oi.window_end,
       oi.batching_hold_count,
       oi.first_batching_hold_until
FROM obligation_instances oi
LEFT JOIN goats g
  ON g.tenant_id = oi.tenant_id
 AND g.goat_id = oi.target_id
 AND oi.target_type = 'goat'
LEFT JOIN location_operational_attributes loa
  ON loa.tenant_id = g.tenant_id
 AND loa.location_id = COALESCE(g.current_location_id, g.shed_id)
WHERE oi.tenant_id = @tenant_id
  AND oi.protocol_version_id = @protocol_version_id
  AND oi.status IN ('scheduled', 'due', 'missed')
  AND oi.batch_id IS NULL
  AND oi.due_at <= @due_before
  AND (
    oi.target_type <> 'goat'
    OR (
      COALESCE(g.health_status, '') NOT IN ('sick', 'under_treatment', 'quarantine', 'icu')
      AND NOT COALESCE(loa.is_quarantine, false)
      AND NOT COALESCE(loa.is_icu, false)
      AND COALESCE(loa.usable_for_vaccination, true)
    )
  )
ORDER BY oi.scope_type, oi.scope_id, oi.rule_id, target_species, oi.due_at, oi.obligation_id
LIMIT @row_limit;

-- name: CountObligationsByScope :one
-- Per-scope rollup. Uses obligation_instances_scope_idx (tenant_id, scope_type, scope_id, status, due_at).
SELECT COUNT(*)::bigint AS total
FROM obligation_instances
WHERE tenant_id = @tenant_id AND scope_type = @scope_type
  AND scope_id = @scope_id AND status = @status;

-- name: FindNearestPlannedBatchDate :one
-- Sick-recovery align: earliest planned drive for this rule in shed or park within the align window.
SELECT b.planned_date
FROM obligation_batches b
WHERE b.tenant_id = @tenant_id
  AND b.protocol_version_id = @protocol_version_id
  AND b.session = @session
  AND b.status = 'planned'
  AND b.planned_date >= @from_date::date
  AND b.planned_date <= @to_date::date
  AND (
    (@shed_id::uuid IS NOT NULL AND b.scope_type = 'shed' AND b.scope_id = @shed_id)
    OR (@park_id::uuid IS NOT NULL AND b.scope_type = 'park' AND b.scope_id = @park_id)
  )
ORDER BY b.planned_date ASC
LIMIT 1;

-- name: IdempotencyKeyStatus :one
SELECT status, COALESCE(result_type, '')::text AS result_type, COALESCE(result_id::text, '')::text AS result_id
FROM idempotency_keys
WHERE idempotency_key = @idempotency_key;
