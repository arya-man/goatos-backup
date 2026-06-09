-- name: CreateLegacyImportRun :one
INSERT INTO legacy_import_runs (
  tenant_id,
  source_name,
  source_system,
  source_dataset,
  source_file_ref,
  source_file_hash,
  policy_version,
  dry_run,
  completed_at,
  status,
  row_count,
  created_goat_count,
  updated_goat_count,
  conflict_count,
  error_count,
  started_by
) VALUES (
  @tenant_id,
  @source_name,
  @source_system,
  @source_dataset,
  sqlc.narg('source_file_ref')::text,
  @source_file_hash,
  @policy_version,
  @dry_run,
  CASE WHEN @status::text = 'completed' THEN now() ELSE NULL END,
  @status,
  @row_count,
  0,
  0,
  0,
  @error_count,
  sqlc.narg('started_by')::uuid
)
RETURNING import_run_id::text AS import_run_id;

-- name: CompleteLegacyImportRun :exec
UPDATE legacy_import_runs
SET
  completed_at = now(),
  status = 'completed',
  row_count = @row_count,
  created_goat_count = 0,
  updated_goat_count = 0,
  conflict_count = 0,
  error_count = @error_count
WHERE tenant_id = @tenant_id
  AND import_run_id = @import_run_id;

-- name: FailLegacyImportRun :exec
UPDATE legacy_import_runs
SET
  completed_at = now(),
  status = 'failed',
  row_count = @row_count,
  created_goat_count = 0,
  updated_goat_count = 0,
  conflict_count = 0,
  error_count = @error_count
WHERE tenant_id = @tenant_id
  AND import_run_id = @import_run_id;

-- name: InsertLegacyImportRow :one
INSERT INTO legacy_import_rows (
  import_run_id,
  tenant_id,
  source_system,
  source_dataset,
  source_record_id,
  source_row_key,
  source_key_recipe_version,
  source_row_version_hash,
  hash_recipe_version,
  row_number,
  raw_payload,
  normalized_payload,
  processing_state,
  error_reason,
  matched_goat_id
) VALUES (
  @import_run_id,
  @tenant_id,
  @source_system,
  @source_dataset,
  sqlc.narg('source_record_id')::text,
  @source_row_key,
  @source_key_recipe_version,
  @source_row_version_hash,
  @hash_recipe_version,
  @row_number,
  @raw_payload,
  @normalized_payload,
  @processing_state,
  sqlc.narg('error_reason')::text,
  NULL
)
ON CONFLICT (tenant_id, source_system, source_dataset, source_row_key, source_row_version_hash) DO NOTHING
RETURNING legacy_row_id::text AS legacy_row_id;

-- name: NewUUID :one
SELECT gen_random_uuid()::text AS uuid;

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

-- name: LockLegacyImportRowForApply :one
SELECT
  legacy_row_id::text AS legacy_row_id,
  row_number,
  source_system,
  source_dataset,
  source_record_id,
  source_row_key,
  source_key_recipe_version,
  source_row_version_hash,
  hash_recipe_version,
  raw_payload,
  normalized_payload,
  processing_state,
  error_reason,
  COALESCE(matched_goat_id::text, '')::text AS matched_goat_id
FROM legacy_import_rows
WHERE tenant_id = @tenant_id
  AND legacy_row_id = @legacy_row_id
FOR UPDATE;

-- name: FindActiveMeshaOrgPartyIDs :many
SELECT p.party_id::text AS party_id
FROM parties p
JOIN orgs o ON o.party_id = p.party_id
WHERE p.party_type = 'org'
  AND p.status = 'active'
  AND o.org_type = 'mesha'
  AND o.status = 'active'
ORDER BY p.party_id;

-- name: GetLegacyStatusMappingForApply :one
SELECT
  lifecycle_status,
  reproductive_status,
  growth_cohort_tag,
  management_stage,
  health_status,
  review_required
FROM legacy_status_mappings
WHERE source_system = @source_system
  AND normalized_raw_label = @normalized_raw_label;

-- name: ResolveBreedAliasForApply :one
SELECT
  b.breed_id::text AS breed_id,
  b.canonical_name
FROM breed_aliases ba
JOIN breeds b ON b.breed_id = ba.breed_id
WHERE ba.normalized_alias = @normalized_alias
  AND b.species = 'goat'
  AND b.status = 'active'
  AND (ba.source_system = @source_system OR ba.source_system = 'phase1_seed' OR ba.source_system IS NULL)
ORDER BY
  CASE
    WHEN ba.source_system = @source_system THEN 0
    WHEN ba.source_system = 'phase1_seed' THEN 1
    ELSE 2
  END,
  b.canonical_name
LIMIT 1;

-- name: GetIdentifierPolicyForApply :one
SELECT normalizer_version, primary_allowed
FROM identifier_policies
WHERE policy_version = @policy_version
  AND identifier_type = @identifier_type;

-- name: ActiveRFIDExistsForApply :one
SELECT EXISTS (
  SELECT 1
  FROM goat_identifiers
  WHERE identifier_type = 'rfid'
    AND normalized_value = @normalized_value
    AND status = 'active'
)::bool;

-- name: ActiveScopedIdentifierExistsForApply :one
SELECT EXISTS (
  SELECT 1
  FROM goat_identifiers
  WHERE tenant_id = @tenant_id
    AND identifier_type = @identifier_type
    AND normalized_value = @normalized_value
    AND scope_key = @scope_key
    AND status = 'active'
)::bool;

-- name: InsertImportIdentityDecision :one
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
  'create_goat',
  'imported_from_rfid_source',
  'approved',
  'import_policy',
  sqlc.narg('decided_by')::uuid,
  @policy_version,
  sqlc.narg('reviewer_id')::uuid,
  @evidence,
  @created_at,
  @created_at,
  @created_at
)
RETURNING
  decision_id::text AS decision_id,
  decision_type,
  decision_result,
  decision_state,
  policy_version,
  created_at;

-- name: InsertGoatFromRFIDApply :one
INSERT INTO goats (
  tenant_id,
  species,
  breed,
  breed_id,
  sex,
  age_band,
  lifecycle_status,
  reproductive_status,
  growth_cohort_tag,
  management_stage,
  health_status,
  identity_state,
  custodian_party_id,
  created_at,
  updated_at,
  created_by
) VALUES (
  @tenant_id,
  'goat',
  sqlc.narg('breed')::text,
  sqlc.narg('breed_id')::uuid,
  @sex,
  sqlc.narg('age_band')::text,
  @lifecycle_status,
  sqlc.narg('reproductive_status')::text,
  sqlc.narg('growth_cohort_tag')::text,
  sqlc.narg('management_stage')::text,
  sqlc.narg('health_status')::text,
  'clean',
  @custodian_party_id,
  @created_at,
  @created_at,
  sqlc.narg('created_by')::uuid
)
RETURNING goat_id::text AS goat_id, display_id, row_version, created_at;

-- name: InsertGoatIdentifierFromRFIDApply :one
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
  source_system,
  source_record_id,
  normalizer_version,
  confidence,
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
  @source_system,
  @source_record_id,
  @normalizer_version,
  1,
  sqlc.narg('approved_by')::uuid
)
RETURNING identifier_id::text AS identifier_id;

-- name: InsertGoatOwnershipFromRFIDApply :exec
INSERT INTO goat_ownership (
  tenant_id,
  goat_id,
  owner_party_id,
  share_bps,
  valid_from,
  status,
  decision_id,
  created_by
) VALUES (
  @tenant_id,
  @goat_id,
  @owner_party_id,
  10000,
  @valid_from,
  'active',
  @decision_id,
  sqlc.narg('created_by')::uuid
);

-- name: InsertGoatCustodyHistoryFromRFIDApply :exec
INSERT INTO goat_custody_history (
  tenant_id,
  goat_id,
  custodian_party_id,
  valid_from,
  decision_id,
  reason,
  created_by
) VALUES (
  @tenant_id,
  @goat_id,
  @custodian_party_id,
  @valid_from,
  @decision_id,
  'first_rfid_import',
  sqlc.narg('created_by')::uuid
);

-- name: InsertGoatIdentityEventFromRFIDApply :one
INSERT INTO goat_identity_events (
  identity_event_id,
  tenant_id,
  goat_id,
  event_type,
  event_version,
  occurred_at,
  recorded_at,
  actor_id,
  source_system,
  source_record_id,
  payload,
  decision_id,
  idempotency_key
) VALUES (
  @identity_event_id,
  @tenant_id,
  @goat_id,
  'goat.created',
  1,
  @occurred_at,
  @occurred_at,
  sqlc.narg('actor_id')::uuid,
  @source_system,
  @source_record_id,
  @payload,
  @decision_id,
  @idempotency_key
)
RETURNING identity_event_id::text AS event_id, recorded_at;

-- name: InsertIdentityDecisionGoatForApply :exec
INSERT INTO identity_decision_goats (
  decision_id,
  tenant_id,
  goat_id,
  role
) VALUES (
  @decision_id,
  @tenant_id,
  @goat_id,
  'affected'
);

-- name: InsertIdentityDecisionIdentifierForApply :exec
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
  'attach'
);

-- name: InsertIdentityDecisionEventForApply :exec
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

-- name: InsertAuditLogForApply :exec
INSERT INTO audit_log (
  tenant_id,
  actor_id,
  actor_type,
  action,
  resource_type,
  resource_id,
  decision_id,
  after_state,
  metadata,
  trace_id
) VALUES (
  @tenant_id,
  sqlc.narg('actor_id')::uuid,
  'import_job',
  'goat.created',
  'goat',
  @resource_id,
  @decision_id,
  @after_state,
  @metadata,
  @trace_id
);

-- name: InsertOutboxMessageForApply :exec
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
  'goat.created',
  '1.0.0',
  'goat',
  @aggregate_id,
  'identity.events',
  @payload,
  @headers,
  @idempotency_key,
  @trace_id,
  'pending'
);

-- name: MarkLegacyImportRowCreatedGoat :exec
UPDATE legacy_import_rows
SET
  processing_state = 'created_goat',
  matched_goat_id = @matched_goat_id,
  error_reason = NULL
WHERE tenant_id = @tenant_id
  AND legacy_row_id = @legacy_row_id;

-- name: MarkLegacyImportRowNeedsReview :exec
UPDATE legacy_import_rows
SET
  processing_state = 'needs_review',
  normalized_payload = @normalized_payload,
  error_reason = @error_reason
WHERE tenant_id = @tenant_id
  AND legacy_row_id = @legacy_row_id;

-- name: IncrementLegacyImportRunCreatedGoatCount :exec
UPDATE legacy_import_runs
SET created_goat_count = created_goat_count + 1
WHERE tenant_id = @tenant_id
  AND import_run_id = @import_run_id;
