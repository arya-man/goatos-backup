-- name: GetGoatByID :one
SELECT
  g.goat_id::text AS goat_id,
  g.display_id,
  animal_id_1.identifier_value AS animal_identifier_1,
  animal_id_2.identifier_value AS animal_identifier_2,
  g.breed,
  g.sex,
  g.age_band,
  g.lifecycle_status,
  g.reproductive_status,
  g.growth_cohort_tag,
  g.management_stage,
  g.health_status,
  -- location_display resolves from the animal's OWN park/shed, never goats.current_location_id.
  -- current_location_id is vestigial: NULL for ~81% of the live herd and stale where set, which
  -- rendered "Unknown location" on animals whose park/shed resolved fine. Shed first, then park,
  -- matching the bare-shed-name shape already-populated rows return today.
  COALESCE(shed.name, park.name, 'Unknown location') AS location_display,
  COALESCE(g.farm_id::text, '')::text AS farm_id,
  COALESCE(farm.location_code, '')::text AS farm_code,
  COALESCE(farm.name, '')::text AS farm_name,
  COALESCE(g.park_id::text, '')::text AS park_id,
  COALESCE(park.location_code, '')::text AS park_code,
  COALESCE(park.name, '')::text AS park_name,
  COALESCE(g.shed_id::text, '')::text AS shed_id,
  COALESCE(shed.location_code, '')::text AS shed_code,
  COALESCE(shed.name, '')::text AS shed_name,
  COALESCE(g.cohort_id::text, '')::text AS cohort_id,
  COALESCE(cohort.location_code, '')::text AS cohort_code,
  COALESCE(cohort.name, '')::text AS cohort_name,
  CASE WHEN gsp.partition_label IS NULL OR lower(btrim(gsp.partition_label)) = 'whole'
       THEN '' ELSE gsp.partition_label END::text AS partition_label,
  COALESCE(gsp.source_shed_name, '')::text AS source_shed_name,
  g.species,
  COALESCE(g.merged_into_goat_id::text, '')::text AS merged_into_goat_id,
  g.row_version
FROM goats g
LEFT JOIN locations farm ON farm.tenant_id = g.tenant_id AND farm.location_id = g.farm_id
LEFT JOIN locations park ON park.tenant_id = g.tenant_id AND park.location_id = g.park_id
LEFT JOIN locations shed ON shed.tenant_id = g.tenant_id AND shed.location_id = g.shed_id
LEFT JOIN locations cohort ON cohort.tenant_id = g.tenant_id AND cohort.location_id = g.cohort_id
LEFT JOIN goat_shed_partitions gsp ON gsp.tenant_id = g.tenant_id AND gsp.goat_id = g.goat_id
LEFT JOIN goat_identifiers animal_id_1 ON animal_id_1.tenant_id = g.tenant_id
  AND animal_id_1.goat_id = g.goat_id
  AND animal_id_1.identifier_type = 'animal_identifier_1'
  AND animal_id_1.status = 'active'
LEFT JOIN goat_identifiers animal_id_2 ON animal_id_2.tenant_id = g.tenant_id
  AND animal_id_2.goat_id = g.goat_id
  AND animal_id_2.identifier_type = 'animal_identifier_2'
  AND animal_id_2.status = 'active'
WHERE g.tenant_id = @tenant_id AND g.goat_id = @goat_id;

-- name: GetGoatByDisplayID :one
SELECT
  g.goat_id::text AS goat_id,
  g.display_id,
  animal_id_1.identifier_value AS animal_identifier_1,
  animal_id_2.identifier_value AS animal_identifier_2,
  g.breed,
  g.sex,
  g.age_band,
  g.lifecycle_status,
  g.reproductive_status,
  g.growth_cohort_tag,
  g.management_stage,
  g.health_status,
  -- location_display resolves from the animal's OWN park/shed, never goats.current_location_id.
  -- current_location_id is vestigial: NULL for ~81% of the live herd and stale where set, which
  -- rendered "Unknown location" on animals whose park/shed resolved fine. Shed first, then park,
  -- matching the bare-shed-name shape already-populated rows return today.
  COALESCE(shed.name, park.name, 'Unknown location') AS location_display,
  COALESCE(g.farm_id::text, '')::text AS farm_id,
  COALESCE(farm.location_code, '')::text AS farm_code,
  COALESCE(farm.name, '')::text AS farm_name,
  COALESCE(g.park_id::text, '')::text AS park_id,
  COALESCE(park.location_code, '')::text AS park_code,
  COALESCE(park.name, '')::text AS park_name,
  COALESCE(g.shed_id::text, '')::text AS shed_id,
  COALESCE(shed.location_code, '')::text AS shed_code,
  COALESCE(shed.name, '')::text AS shed_name,
  COALESCE(g.cohort_id::text, '')::text AS cohort_id,
  COALESCE(cohort.location_code, '')::text AS cohort_code,
  COALESCE(cohort.name, '')::text AS cohort_name,
  CASE WHEN gsp.partition_label IS NULL OR lower(btrim(gsp.partition_label)) = 'whole'
       THEN '' ELSE gsp.partition_label END::text AS partition_label,
  COALESCE(gsp.source_shed_name, '')::text AS source_shed_name,
  g.species,
  COALESCE(g.merged_into_goat_id::text, '')::text AS merged_into_goat_id,
  g.row_version
FROM goats g
LEFT JOIN locations farm ON farm.tenant_id = g.tenant_id AND farm.location_id = g.farm_id
LEFT JOIN locations park ON park.tenant_id = g.tenant_id AND park.location_id = g.park_id
LEFT JOIN locations shed ON shed.tenant_id = g.tenant_id AND shed.location_id = g.shed_id
LEFT JOIN locations cohort ON cohort.tenant_id = g.tenant_id AND cohort.location_id = g.cohort_id
LEFT JOIN goat_shed_partitions gsp ON gsp.tenant_id = g.tenant_id AND gsp.goat_id = g.goat_id
LEFT JOIN goat_identifiers animal_id_1 ON animal_id_1.tenant_id = g.tenant_id
  AND animal_id_1.goat_id = g.goat_id
  AND animal_id_1.identifier_type = 'animal_identifier_1'
  AND animal_id_1.status = 'active'
LEFT JOIN goat_identifiers animal_id_2 ON animal_id_2.tenant_id = g.tenant_id
  AND animal_id_2.goat_id = g.goat_id
  AND animal_id_2.identifier_type = 'animal_identifier_2'
  AND animal_id_2.status = 'active'
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
