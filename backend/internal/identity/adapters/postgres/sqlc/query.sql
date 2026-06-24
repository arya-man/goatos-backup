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
