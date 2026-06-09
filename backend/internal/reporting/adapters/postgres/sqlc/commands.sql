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

-- name: HasIdentityCountersForTenant :one
SELECT EXISTS (
  SELECT 1
  FROM goat_identity_counters
  WHERE tenant_id = @tenant_id
)::bool;

-- name: HasGoatIdentityEventsForTenant :one
SELECT EXISTS (
  SELECT 1
  FROM goat_identity_events
  WHERE tenant_id = @tenant_id
)::bool;

-- name: MaxGoatIdentityEventCheckpointForTenant :one
WITH max_recorded AS (
  SELECT max(recorded_at)::timestamptz AS recorded_at
  FROM goat_identity_events
  WHERE tenant_id = @tenant_id
)
SELECT
  max_recorded.recorded_at,
  (
    SELECT gie.identity_event_id
    FROM goat_identity_events gie
    WHERE gie.tenant_id = @tenant_id
      AND gie.recorded_at = max_recorded.recorded_at
    ORDER BY gie.identity_event_id DESC
    LIMIT 1
  ) AS event_id
FROM max_recorded;

-- name: DeleteIdentityCountersByGrain :execrows
DELETE FROM goat_identity_counters
WHERE tenant_id = @tenant_id
  AND counter_grain = @counter_grain;

-- name: InsertIdentityCountersByGrain :execrows
INSERT INTO goat_identity_counters (
  counter_grain,
  tenant_id,
  custodian_party_id,
  farm_id,
  park_id,
  shed_id,
  cohort_id,
  lifecycle_status,
  reproductive_status,
  growth_cohort_tag,
  management_stage,
  health_status,
  identity_state,
  breed_id,
  sex,
  count_value,
  as_of_recorded_at,
  source_import_run_id,
  is_rebuilding,
  updated_at
)
SELECT
  m.counter_grain,
  m.tenant_id,
  m.custodian_party_id,
  m.farm_id,
  m.park_id,
  m.shed_id,
  m.cohort_id,
  m.lifecycle_status,
  m.reproductive_status,
  m.growth_cohort_tag,
  m.management_stage,
  m.health_status,
  m.identity_state,
  m.breed_id,
  m.sex,
  count(*)::bigint,
  @as_of_recorded_at,
  sqlc.narg('source_import_run_id')::uuid,
  false,
  now()
FROM goat_identity_counter_memberships m
WHERE m.tenant_id = @tenant_id
  AND m.counter_grain = @counter_grain
GROUP BY
  m.counter_grain,
  m.tenant_id,
  m.custodian_party_id,
  m.farm_id,
  m.park_id,
  m.shed_id,
  m.cohort_id,
  m.lifecycle_status,
  m.reproductive_status,
  m.growth_cohort_tag,
  m.management_stage,
  m.health_status,
  m.identity_state,
  m.breed_id,
  m.sex;

-- name: UpsertProjectionStateAfterRebuild :exec
INSERT INTO goat_identity_counter_projection_state (
  tenant_id,
  last_processed_recorded_at,
  last_processed_event_id,
  rebuild_required,
  rebuild_reason,
  updated_at
) VALUES (
  @tenant_id,
  sqlc.narg('last_processed_recorded_at')::timestamptz,
  sqlc.narg('last_processed_event_id')::uuid,
  false,
  NULL,
  now()
)
ON CONFLICT (tenant_id) DO UPDATE
SET last_processed_recorded_at = EXCLUDED.last_processed_recorded_at,
    last_processed_event_id = EXCLUDED.last_processed_event_id,
    rebuild_required = false,
    rebuild_reason = NULL,
    updated_at = now();

-- name: InsertEmptyProjectionState :exec
INSERT INTO goat_identity_counter_projection_state (
  tenant_id,
  last_processed_recorded_at,
  last_processed_event_id,
  rebuild_required,
  rebuild_reason,
  updated_at
) VALUES (
  @tenant_id,
  NULL,
  NULL,
  false,
  NULL,
  now()
)
ON CONFLICT (tenant_id) DO NOTHING;

-- name: MarkProjectionRebuildRequired :exec
INSERT INTO goat_identity_counter_projection_state (
  tenant_id,
  last_processed_recorded_at,
  last_processed_event_id,
  rebuild_required,
  rebuild_reason,
  updated_at
) VALUES (
  @tenant_id,
  sqlc.narg('last_processed_recorded_at')::timestamptz,
  sqlc.narg('last_processed_event_id')::uuid,
  true,
  @rebuild_reason,
  now()
)
ON CONFLICT (tenant_id) DO UPDATE
SET rebuild_required = true,
    rebuild_reason = EXCLUDED.rebuild_reason,
    updated_at = now();

-- name: AdvanceProjectionState :exec
UPDATE goat_identity_counter_projection_state
SET last_processed_recorded_at = @last_processed_recorded_at,
    last_processed_event_id = @last_processed_event_id,
    rebuild_required = false,
    rebuild_reason = NULL,
    updated_at = now()
WHERE tenant_id = @tenant_id
  AND rebuild_required = false;

-- name: InsertProcessedCounterEvent :execrows
INSERT INTO goat_identity_counter_processed_events (
  tenant_id,
  event_id,
  event_recorded_at,
  event_type,
  outcome,
  processed_at
) VALUES (
  @tenant_id,
  @event_id,
  @event_recorded_at,
  @event_type,
  @outcome,
  now()
)
ON CONFLICT DO NOTHING;

-- name: ApplyCreatedGoatCounterEvent :one
WITH membership AS (
  SELECT
    m.counter_grain,
    m.tenant_id,
    m.custodian_party_id,
    m.farm_id,
    m.park_id,
    m.shed_id,
    m.cohort_id,
    m.lifecycle_status,
    m.reproductive_status,
    m.growth_cohort_tag,
    m.management_stage,
    m.health_status,
    m.identity_state,
    m.breed_id,
    m.sex
  FROM goat_identity_counter_memberships m
  WHERE m.tenant_id = @tenant_id
    AND m.goat_id = @goat_id
),
inserted_event AS (
  INSERT INTO goat_identity_counter_processed_events (
    tenant_id,
    event_id,
    event_recorded_at,
    event_type,
    outcome,
    processed_at
  )
  SELECT
    @tenant_id,
    @event_id,
    @event_recorded_at,
    @event_type,
    'applied',
    now()
  WHERE EXISTS (SELECT 1 FROM membership)
  ON CONFLICT DO NOTHING
  RETURNING 1
),
upserted_counters AS (
  INSERT INTO goat_identity_counters (
    counter_grain,
    tenant_id,
    custodian_party_id,
    farm_id,
    park_id,
    shed_id,
    cohort_id,
    lifecycle_status,
    reproductive_status,
    growth_cohort_tag,
    management_stage,
    health_status,
    identity_state,
    breed_id,
    sex,
    count_value,
    as_of_recorded_at,
    source_import_run_id,
    is_rebuilding,
    updated_at
  )
  SELECT
    m.counter_grain,
    m.tenant_id,
    m.custodian_party_id,
    m.farm_id,
    m.park_id,
    m.shed_id,
    m.cohort_id,
    m.lifecycle_status,
    m.reproductive_status,
    m.growth_cohort_tag,
    m.management_stage,
    m.health_status,
    m.identity_state,
    m.breed_id,
    m.sex,
    1,
    @event_recorded_at,
    NULL,
    false,
    now()
  FROM membership m
  WHERE EXISTS (SELECT 1 FROM inserted_event)
  ON CONFLICT (
    counter_grain,
    tenant_id,
    custodian_party_id,
    farm_id,
    park_id,
    shed_id,
    cohort_id,
    lifecycle_status,
    reproductive_status,
    growth_cohort_tag,
    management_stage,
    health_status,
    identity_state,
    breed_id,
    sex
  ) DO UPDATE
  SET count_value = goat_identity_counters.count_value + EXCLUDED.count_value,
      as_of_recorded_at = EXCLUDED.as_of_recorded_at,
      source_import_run_id = NULL,
      is_rebuilding = false,
      updated_at = now()
  RETURNING 1
)
SELECT
  (SELECT count(*) FROM membership)::bigint AS membership_rows,
  (SELECT count(*) FROM inserted_event)::bigint AS processed_rows,
  (SELECT count(*) FROM upserted_counters)::bigint AS counter_rows;

-- name: PruneProcessedCounterEvents :execrows
DELETE FROM goat_identity_counter_processed_events
WHERE tenant_id = @tenant_id
  AND processed_at < @processed_before
  AND event_recorded_at < @checkpoint_recorded_at;
