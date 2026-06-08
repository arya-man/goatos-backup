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
