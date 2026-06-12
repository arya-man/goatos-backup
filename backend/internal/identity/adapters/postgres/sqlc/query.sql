-- name: GetGoatByID :one
SELECT
  g.goat_id::text AS goat_id,
  g.display_id,
  old_tag.identifier_value AS primary_old_tag,
  rfid.identifier_value AS rfid,
  g.breed,
  g.sex,
  g.age_band,
  g.lifecycle_status,
  g.reproductive_status,
  g.growth_cohort_tag,
  g.management_stage,
  g.health_status,
  g.identity_state,
  COALESCE(loc.name, 'Unknown location') AS location_display,
  COALESCE(g.farm_id::text, '')::text AS farm_id,
  COALESCE(g.park_id::text, '')::text AS park_id,
  COALESCE(g.shed_id::text, '')::text AS shed_id,
  COALESCE(g.cohort_id::text, '')::text AS cohort_id,
  g.species,
  COALESCE(g.merged_into_goat_id::text, '')::text AS merged_into_goat_id,
  g.row_version
FROM goats g
LEFT JOIN locations loc ON loc.tenant_id = g.tenant_id AND loc.location_id = g.current_location_id
LEFT JOIN goat_identifiers old_tag ON old_tag.tenant_id = g.tenant_id
  AND old_tag.goat_id = g.goat_id
  AND old_tag.identifier_type = 'old_tag'
  AND old_tag.status = 'active'
  AND old_tag.is_primary_for_goat
LEFT JOIN goat_identifiers rfid ON rfid.tenant_id = g.tenant_id
  AND rfid.goat_id = g.goat_id
  AND rfid.identifier_type = 'rfid'
  AND rfid.status = 'active'
WHERE g.tenant_id = @tenant_id AND g.goat_id = @goat_id;

-- name: GetGoatByDisplayID :one
SELECT
  g.goat_id::text AS goat_id,
  g.display_id,
  old_tag.identifier_value AS primary_old_tag,
  rfid.identifier_value AS rfid,
  g.breed,
  g.sex,
  g.age_band,
  g.lifecycle_status,
  g.reproductive_status,
  g.growth_cohort_tag,
  g.management_stage,
  g.health_status,
  g.identity_state,
  COALESCE(loc.name, 'Unknown location') AS location_display,
  COALESCE(g.farm_id::text, '')::text AS farm_id,
  COALESCE(g.park_id::text, '')::text AS park_id,
  COALESCE(g.shed_id::text, '')::text AS shed_id,
  COALESCE(g.cohort_id::text, '')::text AS cohort_id,
  g.species,
  COALESCE(g.merged_into_goat_id::text, '')::text AS merged_into_goat_id,
  g.row_version
FROM goats g
LEFT JOIN locations loc ON loc.tenant_id = g.tenant_id AND loc.location_id = g.current_location_id
LEFT JOIN goat_identifiers old_tag ON old_tag.tenant_id = g.tenant_id
  AND old_tag.goat_id = g.goat_id
  AND old_tag.identifier_type = 'old_tag'
  AND old_tag.status = 'active'
  AND old_tag.is_primary_for_goat
LEFT JOIN goat_identifiers rfid ON rfid.tenant_id = g.tenant_id
  AND rfid.goat_id = g.goat_id
  AND rfid.identifier_type = 'rfid'
  AND rfid.status = 'active'
WHERE g.tenant_id = @tenant_id AND g.display_id = @display_id;

-- name: ListIdentifiersForGoat :many
SELECT
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
  COALESCE(confidence::float8, 'NaN'::float8)::float8 AS confidence
FROM goat_identifiers
WHERE tenant_id = @tenant_id AND goat_id = @goat_id
ORDER BY is_primary_for_goat DESC, status ASC, valid_from DESC;

-- name: FindOpenConflictForIdentifier :one
SELECT conflict_id::text AS conflict_id
FROM identity_conflicts
WHERE tenant_id = @tenant_id
  AND identifier_type = @identifier_type
  AND identifier_value = @identifier_value
  AND evidence->>'scope_key' = @scope_key::text
  AND state IN ('open', 'needs_field_check')
ORDER BY created_at DESC
LIMIT 1;

-- Keep this projection in sync with conflictSummarySelect in repository.go.
-- name: GetConflictSummaryByID :one
SELECT
  c.conflict_id::text AS conflict_id,
  c.conflict_type,
  c.severity,
  c.identifier_type,
  c.identifier_value,
  COALESCE(c.evidence->>'scope_key', 'unknown')::text AS scope_key,
  COALESCE((SELECT count(*)::int FROM identity_conflict_goats cg WHERE cg.tenant_id = c.tenant_id AND cg.conflict_id = c.conflict_id), cardinality(c.goat_ids), 0)::int AS goat_count,
  COALESCE((SELECT count(*)::int FROM identity_conflict_source_records sr WHERE sr.tenant_id = c.tenant_id AND sr.conflict_id = c.conflict_id), cardinality(c.source_record_ids), 0)::int AS source_record_count,
  c.state,
  c.created_at
FROM identity_conflicts c
WHERE c.tenant_id = @tenant_id AND c.conflict_id = @conflict_id;

-- name: ListConflictGoatsByID :many
SELECT goat_id::text AS goat_id
FROM identity_conflict_goats
WHERE tenant_id = @tenant_id AND conflict_id = @conflict_id
ORDER BY created_at ASC;

-- name: ListConflictSourceRecordsByID :many
SELECT source_system, source_record_id
FROM identity_conflict_source_records
WHERE tenant_id = @tenant_id AND conflict_id = @conflict_id
ORDER BY created_at ASC;

-- name: ListIdentityCandidates :many
SELECT
  candidate_id::text AS candidate_id,
  COALESCE(proposed_goat_id::text, '')::text AS proposed_goat_id,
  COALESCE(candidate_goat_id::text, '')::text AS candidate_goat_id,
  match_score::float8 AS match_score,
  match_reasons,
  state,
  created_by,
  row_version,
  created_at
FROM identity_match_candidates
WHERE tenant_id = @tenant_id
  AND state IN ('proposed', 'needs_review')
  AND (
    sqlc.narg('cursor_created_at')::timestamptz IS NULL
    OR (created_at, candidate_id) < (sqlc.narg('cursor_created_at')::timestamptz, sqlc.narg('cursor_candidate_id')::uuid)
  )
ORDER BY created_at DESC, candidate_id DESC
LIMIT @limit_count;

-- name: GetImportRunByID :one
SELECT
  r.import_run_id::text AS import_run_id,
  r.source_system,
  r.source_dataset,
  r.policy_version,
  r.status,
  r.dry_run,
  r.row_count,
  r.created_goat_count,
  r.updated_goat_count,
  r.conflict_count,
  r.error_count,
  COALESCE((
    SELECT count(*)::int
    FROM legacy_import_rows row
    WHERE row.tenant_id = r.tenant_id
      AND row.import_run_id = r.import_run_id
      AND row.processing_state = 'needs_review'
  ), 0)::int AS rows_needing_review,
  r.started_at,
  r.completed_at
FROM legacy_import_runs r
WHERE r.tenant_id = @tenant_id
  AND r.import_run_id = @import_run_id;

-- name: ListImportRunRows :many
SELECT
  legacy_row_id::text AS import_row_id,
  COALESCE(source_record_id, '')::text AS source_record_id,
  row_number,
  processing_state AS row_state,
  source_row_key,
  COALESCE(matched_goat_id::text, '')::text AS matched_goat_id,
  COALESCE(error_reason, '')::text AS error_reason,
  COALESCE(normalized_payload->>'rfid', '')::text AS rfid,
  COALESCE(normalized_payload->>'normalized_old_tag', '')::text AS old_tag,
  COALESCE(normalized_payload->>'breed', '')::text AS breed,
  COALESCE(normalized_payload->>'gender', '')::text AS gender,
  COALESCE(normalized_payload->>'farm', '')::text AS farm,
  COALESCE(normalized_payload->>'shed', '')::text AS shed,
  COALESCE(normalized_payload->>'partition', '')::text AS partition,
  COALESCE(ARRAY(
    SELECT reason
    FROM (
      SELECT error_reason AS reason
      WHERE error_reason IS NOT NULL AND error_reason <> ''
      UNION
      SELECT jsonb_array_elements_text(
        CASE
          WHEN jsonb_typeof(normalized_payload->'processing_reasons') = 'array'
          THEN normalized_payload->'processing_reasons'
          ELSE '[]'::jsonb
        END
      ) AS reason
    ) reasons
    WHERE reason IS NOT NULL AND reason <> ''
  ), ARRAY[]::text[])::text[] AS review_reasons
FROM legacy_import_rows
WHERE tenant_id = @tenant_id
  AND import_run_id = @import_run_id
  AND (
    sqlc.narg('processing_state')::text IS NULL
    OR processing_state = sqlc.narg('processing_state')::text
  )
  AND (
    sqlc.narg('reason_code')::text IS NULL
    OR error_reason = sqlc.narg('reason_code')::text
    OR (normalized_payload -> 'processing_reasons') ? sqlc.narg('reason_code')::text
  )
  AND (
    sqlc.narg('cursor_row_number')::int IS NULL
    OR (row_number, legacy_row_id) > (sqlc.narg('cursor_row_number')::int, sqlc.narg('cursor_legacy_row_id')::uuid)
  )
ORDER BY row_number ASC, legacy_row_id ASC
LIMIT @limit_count;

-- name: ListGoatTimeline :many
SELECT
  e.identity_event_id::text AS event_id,
  e.event_type,
  e.occurred_at,
  e.recorded_at,
  CASE WHEN e.actor_id IS NULL THEN 'system' ELSE 'human' END::text AS actor_type,
  COALESCE(e.payload->'evidence_refs', '[]'::jsonb)::text AS evidence_refs,
  COALESCE(e.decision_id::text, '')::text AS decision_id
FROM goat_identity_events e
WHERE e.tenant_id = @tenant_id
  AND e.goat_id = @goat_id
  AND (
    sqlc.narg('cursor_occurred_at')::timestamptz IS NULL
    OR (e.occurred_at, e.identity_event_id) < (sqlc.narg('cursor_occurred_at')::timestamptz, sqlc.narg('cursor_event_id')::uuid)
  )
ORDER BY e.occurred_at DESC, e.identity_event_id DESC
LIMIT @limit_count;

-- name: ListCorrectionRequests :many
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
  created_at,
  resolved_at
FROM identity_correction_requests
WHERE tenant_id = @tenant_id
  AND (
    sqlc.narg('created_by')::uuid IS NULL
    OR requested_by = sqlc.narg('created_by')::uuid
  )
  AND (
    sqlc.narg('state')::text IS NULL
    OR state = sqlc.narg('state')::text
  )
  AND (
    sqlc.narg('cursor_created_at')::timestamptz IS NULL
    OR (created_at, identity_correction_requests.correction_request_id) < (sqlc.narg('cursor_created_at')::timestamptz, sqlc.narg('cursor_correction_request_id')::uuid)
  )
ORDER BY created_at DESC, identity_correction_requests.correction_request_id DESC
LIMIT @limit_count;
