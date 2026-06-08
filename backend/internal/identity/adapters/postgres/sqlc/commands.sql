-- name: InsertIdempotencyStarted :one
INSERT INTO idempotency_keys (
  idempotency_key,
  tenant_id,
  scope,
  request_hash,
  status
) VALUES (
  @idempotency_key,
  @tenant_id,
  @scope,
  @request_hash,
  'started'
)
ON CONFLICT (idempotency_key) DO NOTHING
RETURNING idempotency_key;

-- name: GetIdempotencyKey :one
SELECT
  request_hash,
  status,
  COALESCE(result_type, '')::text AS result_type,
  COALESCE(result_id::text, '')::text AS result_id
FROM idempotency_keys
WHERE idempotency_key = @idempotency_key;

-- name: CompleteIdempotencyKey :exec
UPDATE idempotency_keys
SET
  status = 'completed',
  result_type = @result_type,
  result_id = @result_id,
  completed_at = now()
WHERE idempotency_key = @idempotency_key;

-- name: GoatBelongsToTenant :one
SELECT EXISTS (
  SELECT 1
  FROM goats
  WHERE tenant_id = @tenant_id AND goat_id = @goat_id
)::bool;

-- name: NewUUID :one
SELECT gen_random_uuid()::text AS uuid;

-- name: CreateCorrectionRequest :one
INSERT INTO identity_correction_requests (
  tenant_id,
  request_type,
  state,
  goat_id,
  identifier_type,
  identifier_value,
  farm_id,
  park_id,
  shed_id,
  cohort_id,
  description,
  evidence,
  requested_by
) VALUES (
  @tenant_id,
  @request_type,
  'open',
  sqlc.narg('goat_id')::uuid,
  sqlc.narg('identifier_type')::text,
  sqlc.narg('identifier_value')::text,
  sqlc.narg('farm_id')::uuid,
  sqlc.narg('park_id')::uuid,
  sqlc.narg('shed_id')::uuid,
  sqlc.narg('cohort_id')::uuid,
  @description,
  @evidence,
  @requested_by
)
RETURNING
  correction_request_id::text AS correction_request_id,
  request_type,
  state,
  COALESCE(goat_id::text, '')::text AS goat_id,
  identifier_type,
  identifier_value,
  COALESCE(farm_id::text, '')::text AS farm_id,
  COALESCE(park_id::text, '')::text AS park_id,
  COALESCE(shed_id::text, '')::text AS shed_id,
  COALESCE(cohort_id::text, '')::text AS cohort_id,
  description,
  evidence,
  created_at,
  resolved_at;

-- name: GetCorrectionRequestByID :one
SELECT
  correction_request_id::text AS correction_request_id,
  request_type,
  state,
  COALESCE(goat_id::text, '')::text AS goat_id,
  identifier_type,
  identifier_value,
  COALESCE(farm_id::text, '')::text AS farm_id,
  COALESCE(park_id::text, '')::text AS park_id,
  COALESCE(shed_id::text, '')::text AS shed_id,
  COALESCE(cohort_id::text, '')::text AS cohort_id,
  description,
  evidence,
  created_at,
  resolved_at
FROM identity_correction_requests
WHERE tenant_id = @tenant_id AND correction_request_id = @correction_request_id;

-- name: InsertAuditLog :exec
INSERT INTO audit_log (
  tenant_id,
  actor_id,
  actor_type,
  action,
  resource_type,
  resource_id,
  scope_type,
  scope_id,
  after_state,
  metadata,
  trace_id
) VALUES (
  @tenant_id,
  @actor_id,
  'human',
  @action,
  @resource_type,
  @resource_id,
  sqlc.narg('scope_type')::text,
  sqlc.narg('scope_id')::uuid,
  @after_state,
  @metadata,
  @trace_id
);

-- name: InsertOutboxMessage :exec
INSERT INTO outbox_messages (
  tenant_id,
  event_id,
  event_type,
  schema_version,
  aggregate_type,
  aggregate_id,
  topic,
  payload,
  headers,
  idempotency_key,
  trace_id,
  status
) VALUES (
  @tenant_id,
  @event_id,
  @event_type,
  @schema_version,
  @aggregate_type,
  @aggregate_id,
  @topic,
  @payload,
  @headers,
  @idempotency_key,
  @trace_id,
  'pending'
);

-- name: InsertIdentityDecision :one
INSERT INTO identity_decisions (
  decision_id,
  tenant_id,
  decision_type,
  decision_result,
  decision_state,
  decided_by_type,
  decided_by,
  policy_version,
  reviewer_id,
  evidence,
  created_at,
  approved_at,
  decided_at
) VALUES (
  @decision_id,
  @tenant_id,
  @decision_type,
  @decision_result,
  @decision_state,
  'human',
  @decided_by,
  @policy_version,
  @reviewer_id,
  @evidence,
  @created_at,
  sqlc.narg('approved_at')::timestamptz,
  @decided_at
)
RETURNING
  decision_id::text AS decision_id,
  decision_type,
  decision_result,
  decision_state,
  policy_version,
  created_at;

-- name: ResolveCorrectionRequest :one
UPDATE identity_correction_requests
SET
  state = @state,
  assigned_reviewer_id = @reviewer_id,
  decision_id = @decision_id,
  resolved_at = CASE WHEN @terminal::bool THEN @resolved_at::timestamptz ELSE NULL END,
  row_version = row_version + 1
WHERE tenant_id = @tenant_id
  AND correction_request_id = @correction_request_id
  AND state IN ('open', 'assigned', 'needs_field_check')
  AND state <> @state
  AND row_version = @row_version
RETURNING
  correction_request_id::text AS correction_request_id,
  request_type,
  state,
  COALESCE(goat_id::text, '')::text AS goat_id,
  identifier_type,
  identifier_value,
  COALESCE(farm_id::text, '')::text AS farm_id,
  COALESCE(park_id::text, '')::text AS park_id,
  COALESCE(shed_id::text, '')::text AS shed_id,
  COALESCE(cohort_id::text, '')::text AS cohort_id,
  description,
  evidence,
  row_version,
  COALESCE(decision_id::text, '')::text AS decision_id,
  created_at,
  resolved_at;

-- name: GetCorrectionRequestForResolve :one
SELECT
  correction_request_id::text AS correction_request_id,
  request_type,
  state,
  COALESCE(goat_id::text, '')::text AS goat_id,
  identifier_type,
  identifier_value,
  COALESCE(farm_id::text, '')::text AS farm_id,
  COALESCE(park_id::text, '')::text AS park_id,
  COALESCE(shed_id::text, '')::text AS shed_id,
  COALESCE(cohort_id::text, '')::text AS cohort_id,
  description,
  evidence,
  row_version,
  COALESCE(decision_id::text, '')::text AS decision_id,
  created_at,
  resolved_at
FROM identity_correction_requests
WHERE tenant_id = @tenant_id AND correction_request_id = @correction_request_id;

-- name: GetDecisionSummaryByID :one
SELECT
  decision_id::text AS decision_id,
  decision_type,
  decision_result,
  decision_state,
  policy_version,
  created_at
FROM identity_decisions
WHERE tenant_id = @tenant_id AND decision_id = @decision_id;

-- name: GetDecisionEvidenceByID :one
SELECT evidence
FROM identity_decisions
WHERE tenant_id = @tenant_id AND decision_id = @decision_id;

-- name: GuardGoatForIdentifierMutation :one
UPDATE goats
SET
  row_version = row_version + 1,
  updated_at = @updated_at
WHERE goat_id = @goat_id
  AND tenant_id = @tenant_id
  AND row_version = @row_version
  AND identity_state <> 'merged'
RETURNING goat_id::text AS goat_id, row_version;

-- name: TouchGoatForIdentityMutation :one
UPDATE goats
SET
  row_version = row_version + 1,
  updated_at = @updated_at
WHERE tenant_id = @tenant_id
  AND goat_id = @goat_id
  AND identity_state <> 'merged'
RETURNING goat_id::text AS goat_id, row_version;

-- name: GetGoatMutationState :one
SELECT identity_state, row_version
FROM goats
WHERE tenant_id = @tenant_id AND goat_id = @goat_id;

-- name: GetIdentifierPolicy :one
SELECT normalizer_version, primary_allowed
FROM identifier_policies
WHERE policy_version = @policy_version
  AND identifier_type = @identifier_type;

-- name: InsertGoatIdentifier :one
INSERT INTO goat_identifiers (
  tenant_id,
  goat_id,
  identifier_type,
  identifier_value,
  normalized_value,
  scope_key,
  is_primary_for_goat,
  status,
  valid_from,
  normalizer_version,
  approved_by
) VALUES (
  @tenant_id,
  @goat_id,
  @identifier_type,
  @identifier_value,
  @normalized_value,
  @scope_key,
  @is_primary_for_goat,
  'active',
  @valid_from,
  @normalizer_version,
  @approved_by
)
RETURNING
  identifier_id::text AS identifier_id,
  identifier_type,
  identifier_value,
  scope_key,
  status,
  is_primary_for_goat,
  valid_from,
  valid_to,
  source_system,
  source_record_id,
  COALESCE(confidence::float8, 'NaN'::float8)::float8 AS confidence;

-- name: RetireGoatIdentifier :one
UPDATE goat_identifiers
SET
  status = 'retired',
  valid_to = @valid_to,
  approved_by = @approved_by,
  updated_at = @updated_at
WHERE tenant_id = @tenant_id
  AND goat_id = @goat_id
  AND identifier_id = @identifier_id
  AND status = 'active'
RETURNING
  identifier_id::text AS identifier_id,
  identifier_type,
  identifier_value,
  normalized_value,
  scope_key,
  status,
  is_primary_for_goat,
  valid_from,
  valid_to,
  source_system,
  source_record_id,
  COALESCE(confidence::float8, 'NaN'::float8)::float8 AS confidence;

-- name: GetIdentifierByID :one
SELECT
  identifier_id::text AS identifier_id,
  goat_id::text AS goat_id,
  identifier_type,
  identifier_value,
  normalized_value,
  scope_key,
  status,
  is_primary_for_goat,
  valid_from,
  valid_to,
  source_system,
  source_record_id,
  COALESCE(confidence::float8, 'NaN'::float8)::float8 AS confidence
FROM goat_identifiers
WHERE tenant_id = @tenant_id AND identifier_id = @identifier_id;

-- name: InsertIdentityDecisionIdentifier :exec
INSERT INTO identity_decision_identifiers (
  decision_id,
  tenant_id,
  identifier_id,
  identifier_type,
  identifier_value,
  action
) VALUES (
  @decision_id,
  @tenant_id,
  @identifier_id,
  @identifier_type,
  @identifier_value,
  @action
);

-- name: InsertGoatIdentityEvent :one
INSERT INTO goat_identity_events (
  identity_event_id,
  tenant_id,
  goat_id,
  event_type,
  event_version,
  occurred_at,
  recorded_at,
  actor_id,
  payload,
  decision_id,
  idempotency_key
) VALUES (
  @identity_event_id,
  @tenant_id,
  @goat_id,
  @event_type,
  1,
  @occurred_at,
  @recorded_at,
  @actor_id,
  @payload,
  @decision_id,
  @idempotency_key
)
RETURNING identity_event_id::text AS event_id, recorded_at;

-- name: InsertIdentityDecisionEvent :exec
INSERT INTO identity_decision_events (
  decision_id,
  tenant_id,
  event_id,
  event_recorded_at
) VALUES (
  @decision_id,
  @tenant_id,
  @event_id,
  @event_recorded_at
);

-- name: GetIdentifierDecisionEventForReplay :one
SELECT
  d.decision_id::text AS decision_id,
  d.decision_type,
  d.decision_result,
  d.decision_state,
  d.policy_version,
  d.created_at,
  gie.identity_event_id::text AS event_id,
  gie.event_type,
  gie.recorded_at,
  gi.goat_id::text AS goat_id
FROM identity_decision_identifiers idi
JOIN identity_decisions d
  ON d.tenant_id = idi.tenant_id
 AND d.decision_id = idi.decision_id
JOIN identity_decision_events ide
  ON ide.tenant_id = idi.tenant_id
 AND ide.decision_id = idi.decision_id
JOIN goat_identity_events gie
  ON gie.tenant_id = idi.tenant_id
 AND gie.identity_event_id = ide.event_id
 AND gie.recorded_at = ide.event_recorded_at
JOIN goat_identifiers gi
  ON gi.tenant_id = idi.tenant_id
 AND gi.identifier_id = idi.identifier_id
WHERE idi.tenant_id = @tenant_id
  AND idi.identifier_id = @identifier_id
  AND idi.action = @action
  AND d.evidence->'decision_record'->>'idempotency_key' = @idempotency_key::text
ORDER BY d.created_at DESC
LIMIT 1;

-- name: GetConflictForResolve :one
SELECT
  conflict_id::text AS conflict_id,
  conflict_type,
  state,
  row_version,
  identifier_type,
  identifier_value,
  COALESCE(decision_id::text, '')::text AS decision_id,
  resolved_at
FROM identity_conflicts
WHERE tenant_id = @tenant_id AND conflict_id = @conflict_id;

-- name: UpdateIdentityConflictState :one
UPDATE identity_conflicts
SET
  state = @target_state,
  resolved_at = @resolved_at,
  resolved_by = @resolved_by,
  decision_id = @decision_id,
  row_version = row_version + 1
WHERE tenant_id = @tenant_id
  AND conflict_id = @conflict_id
  AND state IN ('open', 'needs_field_check')
  AND row_version = @row_version
RETURNING
  conflict_id::text AS conflict_id,
  state,
  row_version,
  resolved_at;

-- name: ListConflictMemberGoats :many
SELECT
  cg.goat_id::text AS goat_id,
  g.identity_state,
  COALESCE(g.merged_into_goat_id::text, '')::text AS merged_into_goat_id,
  g.row_version,
  COALESCE(g.farm_id::text, '')::text AS farm_id,
  COALESCE(g.park_id::text, '')::text AS park_id,
  COALESCE(g.shed_id::text, '')::text AS shed_id,
  COALESCE(g.cohort_id::text, '')::text AS cohort_id
FROM identity_conflict_goats cg
JOIN goats g
  ON g.tenant_id = cg.tenant_id
 AND g.goat_id = cg.goat_id
WHERE cg.tenant_id = @tenant_id
  AND cg.conflict_id = @conflict_id
ORDER BY cg.goat_id;

-- name: LockGoatsForMerge :many
SELECT
  goat_id::text AS goat_id,
  identity_state,
  COALESCE(merged_into_goat_id::text, '')::text AS merged_into_goat_id,
  row_version,
  COALESCE(farm_id::text, '')::text AS farm_id,
  COALESCE(park_id::text, '')::text AS park_id,
  COALESCE(shed_id::text, '')::text AS shed_id,
  COALESCE(cohort_id::text, '')::text AS cohort_id
FROM goats
WHERE tenant_id = @tenant_id
  AND goat_id = ANY(@goat_ids::uuid[])
ORDER BY goat_id
FOR UPDATE;

-- name: ListGoatsRedirectingTo :many
SELECT
  goat_id::text AS goat_id,
  identity_state,
  COALESCE(merged_into_goat_id::text, '')::text AS merged_into_goat_id,
  row_version,
  COALESCE(farm_id::text, '')::text AS farm_id,
  COALESCE(park_id::text, '')::text AS park_id,
  COALESCE(shed_id::text, '')::text AS shed_id,
  COALESCE(cohort_id::text, '')::text AS cohort_id
FROM goats
WHERE tenant_id = @tenant_id
  AND merged_into_goat_id = ANY(@goat_ids::uuid[])
ORDER BY goat_id
FOR UPDATE;

-- name: MarkGoatMerged :exec
UPDATE goats
SET
  identity_state = 'merged',
  merged_into_goat_id = @survivor_goat_id,
  row_version = row_version + 1,
  updated_at = @updated_at
WHERE tenant_id = @tenant_id
  AND goat_id = @merged_goat_id
  AND goat_id <> @survivor_goat_id;

-- name: RepointMergedGoatRedirect :exec
UPDATE goats
SET
  merged_into_goat_id = @survivor_goat_id,
  row_version = row_version + 1,
  updated_at = @updated_at
WHERE tenant_id = @tenant_id
  AND goat_id = @goat_id
  AND identity_state = 'merged'
  AND merged_into_goat_id IS DISTINCT FROM @survivor_goat_id;

-- name: InsertGoatMergeLink :one
INSERT INTO goat_merge_links (
  tenant_id,
  survivor_goat_id,
  merged_goat_id,
  decision_id,
  reason,
  created_at,
  created_by
) VALUES (
  @tenant_id,
  @survivor_goat_id,
  @merged_goat_id,
  @decision_id,
  @reason,
  @created_at,
  @created_by
)
RETURNING merge_link_id::text AS merge_link_id;

-- name: InsertIdentityDecisionGoat :exec
INSERT INTO identity_decision_goats (
  decision_id,
  tenant_id,
  goat_id,
  role
) VALUES (
  @decision_id,
  @tenant_id,
  @goat_id,
  @role
);

-- name: ListActiveIdentifiersForGoatsForUpdate :many
SELECT
  identifier_id::text AS identifier_id,
  goat_id::text AS goat_id,
  identifier_type,
  identifier_value,
  normalized_value,
  scope_key,
  status,
  is_primary_for_goat,
  valid_from,
  valid_to,
  source_system,
  source_record_id,
  COALESCE(confidence::float8, 'NaN'::float8)::float8 AS confidence
FROM goat_identifiers
WHERE tenant_id = @tenant_id
  AND goat_id = ANY(@goat_ids::uuid[])
  AND status = 'active'
ORDER BY goat_id, identifier_type, normalized_value, scope_key, identifier_id
FOR UPDATE;

-- name: GetIdentifierForConflictDispute :one
SELECT
  gi.identifier_id::text AS identifier_id,
  gi.goat_id::text AS goat_id,
  gi.identifier_type,
  gi.identifier_value,
  gi.normalized_value,
  gi.scope_key,
  gi.status,
  gi.is_primary_for_goat,
  gi.valid_from,
  gi.valid_to,
  gi.source_system,
  gi.source_record_id,
  COALESCE(gi.confidence::float8, 'NaN'::float8)::float8 AS confidence,
  COALESCE(g.farm_id::text, '')::text AS farm_id,
  COALESCE(g.park_id::text, '')::text AS park_id,
  COALESCE(g.shed_id::text, '')::text AS shed_id,
  COALESCE(g.cohort_id::text, '')::text AS cohort_id
FROM goat_identifiers gi
JOIN goats g
  ON g.tenant_id = gi.tenant_id
 AND g.goat_id = gi.goat_id
WHERE gi.tenant_id = @tenant_id
  AND gi.identifier_id = @identifier_id;

-- name: MarkIdentifierDisputedForConflict :one
UPDATE goat_identifiers
SET
  status = 'disputed',
  is_primary_for_goat = false,
  approved_by = @approved_by,
  updated_at = @updated_at
WHERE tenant_id = @tenant_id
  AND identifier_id = @identifier_id
  AND goat_id = @goat_id
  AND identifier_type = @identifier_type
  AND normalized_value = @normalized_value
  AND status = 'active'
RETURNING
  identifier_id::text AS identifier_id,
  goat_id::text AS goat_id,
  identifier_type,
  identifier_value,
  normalized_value,
  scope_key,
  status,
  is_primary_for_goat,
  valid_from,
  valid_to,
  source_system,
  source_record_id,
  COALESCE(confidence::float8, 'NaN'::float8)::float8 AS confidence;

-- name: RetireIdentifierForMerge :one
UPDATE goat_identifiers
SET
  status = 'retired',
  is_primary_for_goat = false,
  valid_to = @valid_to,
  approved_by = @approved_by,
  updated_at = @updated_at
WHERE tenant_id = @tenant_id
  AND identifier_id = @identifier_id
  AND status = 'active'
RETURNING
  identifier_id::text AS identifier_id,
  goat_id::text AS goat_id,
  identifier_type,
  identifier_value,
  normalized_value,
  scope_key,
  status,
  is_primary_for_goat,
  valid_from,
  valid_to,
  source_system,
  source_record_id,
  COALESCE(confidence::float8, 'NaN'::float8)::float8 AS confidence;

-- name: TransferIdentifierForMerge :one
UPDATE goat_identifiers
SET
  goat_id = @survivor_goat_id,
  is_primary_for_goat = false,
  approved_by = @approved_by,
  updated_at = @updated_at
WHERE tenant_id = @tenant_id
  AND identifier_id = @identifier_id
  AND status = 'active'
RETURNING
  identifier_id::text AS identifier_id,
  goat_id::text AS goat_id,
  identifier_type,
  identifier_value,
  normalized_value,
  scope_key,
  status,
  is_primary_for_goat,
  valid_from,
  valid_to,
  source_system,
  source_record_id,
  COALESCE(confidence::float8, 'NaN'::float8)::float8 AS confidence;

-- name: ListMergeLinksByDecision :many
SELECT
  merge_link_id::text AS merge_link_id,
  survivor_goat_id::text AS survivor_goat_id,
  merged_goat_id::text AS merged_goat_id
FROM goat_merge_links
WHERE tenant_id = @tenant_id AND decision_id = @decision_id
ORDER BY created_at ASC, merged_goat_id ASC;

-- name: ListDecisionIdentifiers :many
SELECT
  COALESCE(identifier_id::text, '')::text AS identifier_id,
  identifier_type,
  identifier_value,
  action
FROM identity_decision_identifiers
WHERE tenant_id = @tenant_id AND decision_id = @decision_id
ORDER BY created_at ASC, decision_identifier_id ASC;

-- name: ListDecisionEvents :many
SELECT
  gie.identity_event_id::text AS event_id,
  gie.event_type,
  gie.recorded_at
FROM identity_decision_events ide
JOIN goat_identity_events gie
  ON gie.tenant_id = ide.tenant_id
 AND gie.identity_event_id = ide.event_id
 AND gie.recorded_at = ide.event_recorded_at
WHERE ide.tenant_id = @tenant_id AND ide.decision_id = @decision_id
ORDER BY gie.recorded_at ASC, gie.identity_event_id ASC;
