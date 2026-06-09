-- name: ListIdentityCounts :many
SELECT
  counter_grain,
  tenant_id::text AS tenant_id,
  COALESCE(custodian_party_id::text, '')::text AS custodian_party_id,
  COALESCE(farm_id::text, '')::text AS farm_id,
  COALESCE(park_id::text, '')::text AS park_id,
  COALESCE(shed_id::text, '')::text AS shed_id,
  COALESCE(cohort_id::text, '')::text AS cohort_id,
  lifecycle_status,
  reproductive_status,
  growth_cohort_tag,
  management_stage,
  health_status,
  identity_state,
  COALESCE(breed_id::text, '')::text AS breed_id,
  sex,
  count_value,
  as_of_recorded_at,
  COALESCE(source_import_run_id::text, '')::text AS source_import_run_id,
  is_rebuilding,
  updated_at
FROM goat_identity_counters
WHERE counter_grain = @counter_grain
  AND tenant_id = @tenant_id
  AND (sqlc.narg('custodian_party_id')::uuid IS NULL OR custodian_party_id = sqlc.narg('custodian_party_id')::uuid)
  AND (sqlc.narg('farm_id')::uuid IS NULL OR farm_id = sqlc.narg('farm_id')::uuid)
  AND (sqlc.narg('park_id')::uuid IS NULL OR park_id = sqlc.narg('park_id')::uuid)
  AND (sqlc.narg('shed_id')::uuid IS NULL OR shed_id = sqlc.narg('shed_id')::uuid)
  AND (sqlc.narg('cohort_id')::uuid IS NULL OR cohort_id = sqlc.narg('cohort_id')::uuid)
  AND (sqlc.narg('lifecycle_status')::text IS NULL OR lifecycle_status = sqlc.narg('lifecycle_status')::text)
  AND (sqlc.narg('reproductive_status')::text IS NULL OR reproductive_status = sqlc.narg('reproductive_status')::text)
  AND (sqlc.narg('growth_cohort_tag')::text IS NULL OR growth_cohort_tag = sqlc.narg('growth_cohort_tag')::text)
  AND (sqlc.narg('management_stage')::text IS NULL OR management_stage = sqlc.narg('management_stage')::text)
  AND (sqlc.narg('health_status')::text IS NULL OR health_status = sqlc.narg('health_status')::text)
  AND (sqlc.narg('identity_state')::text IS NULL OR identity_state = sqlc.narg('identity_state')::text)
  AND (sqlc.narg('breed_id')::uuid IS NULL OR breed_id = sqlc.narg('breed_id')::uuid)
  AND (sqlc.narg('sex')::text IS NULL OR sex = sqlc.narg('sex')::text)
ORDER BY updated_at DESC, counter_id
LIMIT 100;
