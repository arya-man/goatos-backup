-- +goose Up
CREATE TABLE goat_identity_counter_projection_state (
  tenant_id uuid PRIMARY KEY REFERENCES tenants(tenant_id),
  last_processed_recorded_at timestamptz NULL,
  last_processed_event_id uuid NULL,
  rebuild_required boolean NOT NULL DEFAULT false,
  rebuild_reason text NULL,
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE goat_identity_counter_processed_events (
  tenant_id uuid NOT NULL,
  event_id uuid NOT NULL,
  event_recorded_at timestamptz NOT NULL,
  event_type text NOT NULL,
  outcome text NOT NULL,
  processed_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, event_id, event_recorded_at),
  CONSTRAINT goat_identity_counter_processed_events_outcome_check
    CHECK (outcome IN ('applied', 'noop')),
  CONSTRAINT goat_identity_counter_processed_events_event_fk
    FOREIGN KEY (tenant_id, event_id, event_recorded_at)
    REFERENCES goat_identity_events(tenant_id, identity_event_id, recorded_at)
);

CREATE INDEX goat_identity_events_tenant_recorded_event_idx
  ON goat_identity_events(tenant_id, recorded_at ASC, identity_event_id ASC);

CREATE INDEX goat_identity_counter_processed_events_prune_idx
  ON goat_identity_counter_processed_events(tenant_id, processed_at, event_recorded_at);

CREATE VIEW goat_identity_counter_memberships AS
SELECT
  'tenant_lifecycle'::text AS counter_grain,
  g.tenant_id,
  g.goat_id,
  NULL::uuid AS custodian_party_id,
  NULL::uuid AS farm_id,
  NULL::uuid AS park_id,
  NULL::uuid AS shed_id,
  NULL::uuid AS cohort_id,
  g.lifecycle_status,
  NULL::text AS reproductive_status,
  NULL::text AS growth_cohort_tag,
  NULL::text AS management_stage,
  NULL::text AS health_status,
  NULL::text AS identity_state,
  NULL::uuid AS breed_id,
  NULL::text AS sex
FROM goats g
WHERE g.identity_state NOT IN ('merged', 'inactive')
UNION ALL
SELECT
  'custodian_lifecycle',
  g.tenant_id,
  g.goat_id,
  g.custodian_party_id,
  NULL::uuid,
  NULL::uuid,
  NULL::uuid,
  NULL::uuid,
  g.lifecycle_status,
  NULL::text,
  NULL::text,
  NULL::text,
  NULL::text,
  NULL::text,
  NULL::uuid,
  NULL::text
FROM goats g
WHERE g.identity_state NOT IN ('merged', 'inactive')
UNION ALL
SELECT
  'custodian_identity',
  g.tenant_id,
  g.goat_id,
  g.custodian_party_id,
  NULL::uuid,
  NULL::uuid,
  NULL::uuid,
  NULL::uuid,
  NULL::text,
  NULL::text,
  NULL::text,
  NULL::text,
  NULL::text,
  g.identity_state,
  NULL::uuid,
  NULL::text
FROM goats g
WHERE g.lifecycle_status = 'alive'
  AND g.identity_state NOT IN ('merged', 'inactive')
UNION ALL
SELECT
  'park_lifecycle',
  g.tenant_id,
  g.goat_id,
  NULL::uuid,
  NULL::uuid,
  g.park_id,
  NULL::uuid,
  NULL::uuid,
  g.lifecycle_status,
  NULL::text,
  NULL::text,
  NULL::text,
  NULL::text,
  NULL::text,
  NULL::uuid,
  NULL::text
FROM goats g
WHERE g.identity_state NOT IN ('merged', 'inactive')
UNION ALL
SELECT
  'shed_lifecycle',
  g.tenant_id,
  g.goat_id,
  NULL::uuid,
  NULL::uuid,
  g.park_id,
  g.shed_id,
  NULL::uuid,
  g.lifecycle_status,
  NULL::text,
  NULL::text,
  NULL::text,
  NULL::text,
  NULL::text,
  NULL::uuid,
  NULL::text
FROM goats g
WHERE g.identity_state NOT IN ('merged', 'inactive')
UNION ALL
SELECT
  'breed_sex_lifecycle',
  g.tenant_id,
  g.goat_id,
  NULL::uuid,
  NULL::uuid,
  NULL::uuid,
  NULL::uuid,
  NULL::uuid,
  g.lifecycle_status,
  NULL::text,
  NULL::text,
  NULL::text,
  NULL::text,
  NULL::text,
  g.breed_id,
  g.sex
FROM goats g
WHERE g.identity_state NOT IN ('merged', 'inactive')
UNION ALL
SELECT
  'health_status',
  g.tenant_id,
  g.goat_id,
  NULL::uuid,
  NULL::uuid,
  NULL::uuid,
  NULL::uuid,
  NULL::uuid,
  NULL::text,
  NULL::text,
  NULL::text,
  NULL::text,
  g.health_status,
  NULL::text,
  NULL::uuid,
  NULL::text
FROM goats g
WHERE g.lifecycle_status = 'alive'
  AND g.identity_state NOT IN ('merged', 'inactive')
UNION ALL
SELECT
  'growth_cohort',
  g.tenant_id,
  g.goat_id,
  NULL::uuid,
  NULL::uuid,
  NULL::uuid,
  NULL::uuid,
  NULL::uuid,
  NULL::text,
  NULL::text,
  g.growth_cohort_tag,
  NULL::text,
  NULL::text,
  NULL::text,
  NULL::uuid,
  NULL::text
FROM goats g
WHERE g.lifecycle_status = 'alive'
  AND g.identity_state NOT IN ('merged', 'inactive')
UNION ALL
SELECT
  'management_stage',
  g.tenant_id,
  g.goat_id,
  NULL::uuid,
  NULL::uuid,
  NULL::uuid,
  NULL::uuid,
  NULL::uuid,
  NULL::text,
  NULL::text,
  NULL::text,
  g.management_stage,
  NULL::text,
  NULL::text,
  NULL::uuid,
  NULL::text
FROM goats g
WHERE g.lifecycle_status = 'alive'
  AND g.identity_state NOT IN ('merged', 'inactive')
UNION ALL
SELECT
  'reproductive_status',
  g.tenant_id,
  g.goat_id,
  NULL::uuid,
  NULL::uuid,
  NULL::uuid,
  NULL::uuid,
  NULL::uuid,
  NULL::text,
  g.reproductive_status,
  NULL::text,
  NULL::text,
  NULL::text,
  NULL::text,
  NULL::uuid,
  NULL::text
FROM goats g
WHERE g.lifecycle_status = 'alive'
  AND g.identity_state NOT IN ('merged', 'inactive');

-- +goose Down
DROP VIEW IF EXISTS goat_identity_counter_memberships;
DROP INDEX IF EXISTS goat_identity_counter_processed_events_prune_idx;
DROP INDEX IF EXISTS goat_identity_events_tenant_recorded_event_idx;
DROP TABLE IF EXISTS goat_identity_counter_processed_events;
DROP TABLE IF EXISTS goat_identity_counter_projection_state;
