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

-- name: NewUUID :one
SELECT gen_random_uuid()::text AS uuid;

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

-- name: GuardGoatForIdentifierMutation :one
UPDATE goats
SET
  row_version = row_version + 1,
  updated_at = @updated_at
WHERE goat_id = @goat_id
  AND tenant_id = @tenant_id
  AND row_version = @row_version
  AND merged_into_goat_id IS NULL
RETURNING goat_id::text AS goat_id, row_version;

-- name: GetGoatMutationState :one
SELECT COALESCE(merged_into_goat_id::text, '')::text AS merged_into_goat_id, row_version
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
