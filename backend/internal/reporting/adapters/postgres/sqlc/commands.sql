-- name: TenantExists :one
SELECT EXISTS (
  SELECT 1
  FROM tenants
  WHERE tenant_id = @tenant_id
)::bool;

-- name: SourceImportRunBelongsToTenant :one
SELECT EXISTS (
  SELECT 1
  FROM legacy_import_runs
  WHERE tenant_id = @tenant_id
    AND import_run_id = @import_run_id
)::bool;

-- name: MaxGoatIdentityEventRecordedAtForTenant :one
SELECT max(recorded_at)::timestamptz AS as_of_recorded_at
FROM goat_identity_events
WHERE tenant_id = @tenant_id;

-- name: DeleteIdentityCountersByGrain :execrows
DELETE FROM goat_identity_counters
WHERE tenant_id = @tenant_id
  AND counter_grain = @counter_grain;

-- name: InsertTenantLifecycleCounters :execrows
INSERT INTO goat_identity_counters (
  counter_grain,
  tenant_id,
  lifecycle_status,
  count_value,
  as_of_recorded_at,
  source_import_run_id,
  is_rebuilding,
  updated_at
)
SELECT
  'tenant_lifecycle',
  g.tenant_id,
  g.lifecycle_status,
  count(*)::bigint,
  @as_of_recorded_at,
  sqlc.narg('source_import_run_id')::uuid,
  false,
  now()
FROM goats g
WHERE g.tenant_id = @tenant_id
  AND g.identity_state NOT IN ('merged', 'inactive')
GROUP BY g.tenant_id, g.lifecycle_status;

-- name: InsertCustodianLifecycleCounters :execrows
INSERT INTO goat_identity_counters (
  counter_grain,
  tenant_id,
  custodian_party_id,
  lifecycle_status,
  count_value,
  as_of_recorded_at,
  source_import_run_id,
  is_rebuilding,
  updated_at
)
SELECT
  'custodian_lifecycle',
  g.tenant_id,
  g.custodian_party_id,
  g.lifecycle_status,
  count(*)::bigint,
  @as_of_recorded_at,
  sqlc.narg('source_import_run_id')::uuid,
  false,
  now()
FROM goats g
WHERE g.tenant_id = @tenant_id
  AND g.identity_state NOT IN ('merged', 'inactive')
GROUP BY g.tenant_id, g.custodian_party_id, g.lifecycle_status;

-- name: InsertCustodianIdentityCounters :execrows
INSERT INTO goat_identity_counters (
  counter_grain,
  tenant_id,
  custodian_party_id,
  identity_state,
  count_value,
  as_of_recorded_at,
  source_import_run_id,
  is_rebuilding,
  updated_at
)
SELECT
  'custodian_identity',
  g.tenant_id,
  g.custodian_party_id,
  g.identity_state,
  count(*)::bigint,
  @as_of_recorded_at,
  sqlc.narg('source_import_run_id')::uuid,
  false,
  now()
FROM goats g
WHERE g.tenant_id = @tenant_id
  AND g.lifecycle_status = 'alive'
  AND g.identity_state NOT IN ('merged', 'inactive')
GROUP BY g.tenant_id, g.custodian_party_id, g.identity_state;

-- name: InsertParkLifecycleCounters :execrows
INSERT INTO goat_identity_counters (
  counter_grain,
  tenant_id,
  park_id,
  lifecycle_status,
  count_value,
  as_of_recorded_at,
  source_import_run_id,
  is_rebuilding,
  updated_at
)
SELECT
  'park_lifecycle',
  g.tenant_id,
  g.park_id,
  g.lifecycle_status,
  count(*)::bigint,
  @as_of_recorded_at,
  sqlc.narg('source_import_run_id')::uuid,
  false,
  now()
FROM goats g
WHERE g.tenant_id = @tenant_id
  AND g.identity_state NOT IN ('merged', 'inactive')
GROUP BY g.tenant_id, g.park_id, g.lifecycle_status;

-- name: InsertShedLifecycleCounters :execrows
INSERT INTO goat_identity_counters (
  counter_grain,
  tenant_id,
  park_id,
  shed_id,
  lifecycle_status,
  count_value,
  as_of_recorded_at,
  source_import_run_id,
  is_rebuilding,
  updated_at
)
SELECT
  'shed_lifecycle',
  g.tenant_id,
  g.park_id,
  g.shed_id,
  g.lifecycle_status,
  count(*)::bigint,
  @as_of_recorded_at,
  sqlc.narg('source_import_run_id')::uuid,
  false,
  now()
FROM goats g
WHERE g.tenant_id = @tenant_id
  AND g.identity_state NOT IN ('merged', 'inactive')
GROUP BY g.tenant_id, g.park_id, g.shed_id, g.lifecycle_status;

-- name: InsertBreedSexLifecycleCounters :execrows
INSERT INTO goat_identity_counters (
  counter_grain,
  tenant_id,
  breed_id,
  sex,
  lifecycle_status,
  count_value,
  as_of_recorded_at,
  source_import_run_id,
  is_rebuilding,
  updated_at
)
SELECT
  'breed_sex_lifecycle',
  g.tenant_id,
  g.breed_id,
  g.sex,
  g.lifecycle_status,
  count(*)::bigint,
  @as_of_recorded_at,
  sqlc.narg('source_import_run_id')::uuid,
  false,
  now()
FROM goats g
WHERE g.tenant_id = @tenant_id
  AND g.identity_state NOT IN ('merged', 'inactive')
GROUP BY g.tenant_id, g.breed_id, g.sex, g.lifecycle_status;

-- name: InsertHealthStatusCounters :execrows
INSERT INTO goat_identity_counters (
  counter_grain,
  tenant_id,
  health_status,
  count_value,
  as_of_recorded_at,
  source_import_run_id,
  is_rebuilding,
  updated_at
)
SELECT
  'health_status',
  g.tenant_id,
  g.health_status,
  count(*)::bigint,
  @as_of_recorded_at,
  sqlc.narg('source_import_run_id')::uuid,
  false,
  now()
FROM goats g
WHERE g.tenant_id = @tenant_id
  AND g.lifecycle_status = 'alive'
  AND g.identity_state NOT IN ('merged', 'inactive')
GROUP BY g.tenant_id, g.health_status;

-- name: InsertGrowthCohortCounters :execrows
INSERT INTO goat_identity_counters (
  counter_grain,
  tenant_id,
  growth_cohort_tag,
  count_value,
  as_of_recorded_at,
  source_import_run_id,
  is_rebuilding,
  updated_at
)
SELECT
  'growth_cohort',
  g.tenant_id,
  g.growth_cohort_tag,
  count(*)::bigint,
  @as_of_recorded_at,
  sqlc.narg('source_import_run_id')::uuid,
  false,
  now()
FROM goats g
WHERE g.tenant_id = @tenant_id
  AND g.lifecycle_status = 'alive'
  AND g.identity_state NOT IN ('merged', 'inactive')
GROUP BY g.tenant_id, g.growth_cohort_tag;

-- name: InsertManagementStageCounters :execrows
INSERT INTO goat_identity_counters (
  counter_grain,
  tenant_id,
  management_stage,
  count_value,
  as_of_recorded_at,
  source_import_run_id,
  is_rebuilding,
  updated_at
)
SELECT
  'management_stage',
  g.tenant_id,
  g.management_stage,
  count(*)::bigint,
  @as_of_recorded_at,
  sqlc.narg('source_import_run_id')::uuid,
  false,
  now()
FROM goats g
WHERE g.tenant_id = @tenant_id
  AND g.lifecycle_status = 'alive'
  AND g.identity_state NOT IN ('merged', 'inactive')
GROUP BY g.tenant_id, g.management_stage;

-- name: InsertReproductiveStatusCounters :execrows
INSERT INTO goat_identity_counters (
  counter_grain,
  tenant_id,
  reproductive_status,
  count_value,
  as_of_recorded_at,
  source_import_run_id,
  is_rebuilding,
  updated_at
)
SELECT
  'reproductive_status',
  g.tenant_id,
  g.reproductive_status,
  count(*)::bigint,
  @as_of_recorded_at,
  sqlc.narg('source_import_run_id')::uuid,
  false,
  now()
FROM goats g
WHERE g.tenant_id = @tenant_id
  AND g.lifecycle_status = 'alive'
  AND g.identity_state NOT IN ('merged', 'inactive')
GROUP BY g.tenant_id, g.reproductive_status;
