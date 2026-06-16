-- name: ListIdentityCounts :many
SELECT
  c.counter_id::text AS counter_id,
  c.counter_grain,
  c.tenant_id::text AS tenant_id,
  COALESCE(c.custodian_party_id::text, '')::text AS custodian_party_id,
  COALESCE(c.farm_id::text, '')::text AS farm_id,
  farm.name AS farm_name,
  COALESCE(c.park_id::text, '')::text AS park_id,
  park.name AS park_name,
  COALESCE(c.shed_id::text, '')::text AS shed_id,
  shed.name AS shed_name,
  COALESCE(c.cohort_id::text, '')::text AS cohort_id,
  cohort.name AS cohort_name,
  c.lifecycle_status,
  c.reproductive_status,
  c.growth_cohort_tag,
  c.management_stage,
  c.health_status,
  c.identity_state,
  COALESCE(c.breed_id::text, '')::text AS breed_id,
  breed.canonical_name AS breed_name,
  c.sex,
  c.count_value,
  c.as_of_recorded_at,
  COALESCE(c.source_import_run_id::text, '')::text AS source_import_run_id,
  c.is_rebuilding,
  c.updated_at
FROM goat_identity_counters c
LEFT JOIN locations farm ON farm.tenant_id = c.tenant_id AND farm.location_id = c.farm_id
LEFT JOIN locations park ON park.tenant_id = c.tenant_id AND park.location_id = c.park_id
LEFT JOIN locations shed ON shed.tenant_id = c.tenant_id AND shed.location_id = c.shed_id
LEFT JOIN locations cohort ON cohort.tenant_id = c.tenant_id AND cohort.location_id = c.cohort_id
LEFT JOIN breeds breed ON breed.breed_id = c.breed_id
WHERE c.counter_grain = @counter_grain
  AND c.tenant_id = @tenant_id
  AND (sqlc.narg('custodian_party_id')::uuid IS NULL OR c.custodian_party_id = sqlc.narg('custodian_party_id')::uuid)
  AND (sqlc.narg('farm_id')::uuid IS NULL OR c.farm_id = sqlc.narg('farm_id')::uuid)
  AND (sqlc.narg('park_id')::uuid IS NULL OR c.park_id = sqlc.narg('park_id')::uuid)
  AND (sqlc.narg('shed_id')::uuid IS NULL OR c.shed_id = sqlc.narg('shed_id')::uuid)
  AND (sqlc.narg('cohort_id')::uuid IS NULL OR c.cohort_id = sqlc.narg('cohort_id')::uuid)
  AND (sqlc.narg('lifecycle_status')::text IS NULL OR c.lifecycle_status = sqlc.narg('lifecycle_status')::text)
  AND (sqlc.narg('reproductive_status')::text IS NULL OR c.reproductive_status = sqlc.narg('reproductive_status')::text)
  AND (sqlc.narg('growth_cohort_tag')::text IS NULL OR c.growth_cohort_tag = sqlc.narg('growth_cohort_tag')::text)
  AND (sqlc.narg('management_stage')::text IS NULL OR c.management_stage = sqlc.narg('management_stage')::text)
  AND (sqlc.narg('health_status')::text IS NULL OR c.health_status = sqlc.narg('health_status')::text)
  AND (sqlc.narg('identity_state')::text IS NULL OR c.identity_state = sqlc.narg('identity_state')::text)
  AND (sqlc.narg('breed_id')::uuid IS NULL OR c.breed_id = sqlc.narg('breed_id')::uuid)
  AND (sqlc.narg('sex')::text IS NULL OR c.sex = sqlc.narg('sex')::text)
  AND (
    sqlc.narg('cursor_count_value')::bigint IS NULL
    OR c.count_value < sqlc.narg('cursor_count_value')::bigint
    OR (
      c.count_value = sqlc.narg('cursor_count_value')::bigint
      AND c.counter_id > sqlc.narg('cursor_counter_id')::uuid
    )
  )
ORDER BY c.count_value DESC, c.counter_id ASC
LIMIT @limit_count;

-- name: GetIdentityCounterProjectionState :one
SELECT
  tenant_id::text AS tenant_id,
  last_processed_recorded_at,
  COALESCE(last_processed_event_id::text, '')::text AS last_processed_event_id,
  rebuild_required,
  rebuild_reason,
  updated_at
FROM goat_identity_counter_projection_state
WHERE tenant_id = @tenant_id;

-- name: ListIdentityEventsAfterCheckpoint :many
SELECT
  identity_event_id::text AS event_id,
  tenant_id::text AS tenant_id,
  goat_id::text AS goat_id,
  event_type,
  recorded_at
FROM goat_identity_events
WHERE tenant_id = @tenant_id
  AND (
    sqlc.narg('last_processed_recorded_at')::timestamptz IS NULL
    OR recorded_at > sqlc.narg('last_processed_recorded_at')::timestamptz
    OR (
      recorded_at = sqlc.narg('last_processed_recorded_at')::timestamptz
      AND sqlc.narg('last_processed_event_id')::uuid IS NOT NULL
      AND identity_event_id > sqlc.narg('last_processed_event_id')::uuid
    )
  )
ORDER BY recorded_at ASC, identity_event_id ASC
LIMIT @limit_count;
