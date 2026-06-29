-- +goose Up
-- Feed Direction G2: Counts/Shifting canonical input contract.
-- These tables are Counts-owned; Feed consumes their immutable projection
-- snapshots and readiness state instead of deriving counts inside generation.

CREATE TABLE count_base_anchors (
  base_count_anchor_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  park_id uuid NOT NULL REFERENCES locations(location_id),
  shed_id uuid NOT NULL REFERENCES locations(location_id),
  breed_id uuid NULL REFERENCES breeds(breed_id),
  breed_key text NOT NULL,
  breed_label text NOT NULL,
  counted_at timestamptz NOT NULL,
  head_count integer NOT NULL,
  source_system text NOT NULL,
  source_ref text NOT NULL,
  source_hash text NOT NULL,
  anchor_state text NOT NULL DEFAULT 'adopted',
  discrepancy_state text NOT NULL DEFAULT 'not_checked',
  idempotency_key text NOT NULL,
  request_fingerprint text NOT NULL,
  recorded_by uuid NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  row_version integer NOT NULL DEFAULT 1,
  CONSTRAINT count_base_anchors_tenant_park_fk
    FOREIGN KEY (tenant_id, park_id) REFERENCES locations(tenant_id, location_id),
  CONSTRAINT count_base_anchors_tenant_shed_fk
    FOREIGN KEY (tenant_id, shed_id) REFERENCES locations(tenant_id, location_id),
  CONSTRAINT count_base_anchors_head_count_check CHECK (head_count >= 0),
  CONSTRAINT count_base_anchors_state_check CHECK (anchor_state IN ('adopted', 'superseded', 'rejected')),
  CONSTRAINT count_base_anchors_discrepancy_check CHECK (discrepancy_state IN ('not_checked', 'investigating', 'resolved')),
  CONSTRAINT count_base_anchors_source_check CHECK (source_system IN ('physical_base_count', 'manual_review', 'import', 'goatos_canonical')),
  CONSTRAINT count_base_anchors_breed_key_check CHECK (btrim(breed_key) <> ''),
  CONSTRAINT count_base_anchors_source_ref_check CHECK (btrim(source_ref) <> ''),
  CONSTRAINT count_base_anchors_source_hash_check CHECK (btrim(source_hash) <> ''),
  CONSTRAINT count_base_anchors_idem_check CHECK (btrim(idempotency_key) <> '')
);

CREATE UNIQUE INDEX count_base_anchors_idempotency_unique
  ON count_base_anchors (tenant_id, idempotency_key);

CREATE UNIQUE INDEX count_base_anchors_source_unique
  ON count_base_anchors (tenant_id, park_id, shed_id, lower(breed_key), counted_at, source_hash);

CREATE INDEX count_base_anchors_hot_idx
  ON count_base_anchors (tenant_id, park_id, shed_id, lower(breed_key), counted_at DESC, base_count_anchor_id DESC)
  WHERE anchor_state = 'adopted';

CREATE TABLE shifting_events (
  shifting_event_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  logical_shifting_event_key text NOT NULL,
  priority text NOT NULL,
  category text NOT NULL,
  source_park_id uuid NULL REFERENCES locations(location_id),
  source_shed_id uuid NULL REFERENCES locations(location_id),
  destination_park_id uuid NOT NULL REFERENCES locations(location_id),
  destination_shed_id uuid NOT NULL REFERENCES locations(location_id),
  raised_at timestamptz NOT NULL,
  effective_at timestamptz NOT NULL,
  authorized_at timestamptz NULL,
  authorized_by uuid NULL,
  authorization_state text NOT NULL DEFAULT 'pending',
  verification_state text NOT NULL DEFAULT 'unverified',
  event_status text NOT NULL DEFAULT 'pending',
  source_system text NOT NULL,
  source_ref text NOT NULL,
  proof_ref text NULL,
  payload_hash text NOT NULL,
  idempotency_key text NOT NULL,
  request_fingerprint text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  row_version integer NOT NULL DEFAULT 1,
  CONSTRAINT shifting_events_tenant_source_park_fk
    FOREIGN KEY (tenant_id, source_park_id) REFERENCES locations(tenant_id, location_id),
  CONSTRAINT shifting_events_tenant_source_shed_fk
    FOREIGN KEY (tenant_id, source_shed_id) REFERENCES locations(tenant_id, location_id),
  CONSTRAINT shifting_events_tenant_destination_park_fk
    FOREIGN KEY (tenant_id, destination_park_id) REFERENCES locations(tenant_id, location_id),
  CONSTRAINT shifting_events_tenant_destination_shed_fk
    FOREIGN KEY (tenant_id, destination_shed_id) REFERENCES locations(tenant_id, location_id),
  CONSTRAINT shifting_events_key_check CHECK (btrim(logical_shifting_event_key) <> ''),
  CONSTRAINT shifting_events_priority_check CHECK (priority IN ('normal', 'high', 'emergency')),
  CONSTRAINT shifting_events_category_check CHECK (category IN ('routine', 'high_priority', 'pregnancy', 'warmup', 'medical', 'quarantine', 'other')),
  CONSTRAINT shifting_events_auth_state_check CHECK (authorization_state IN ('pending', 'authorized', 'rejected')),
  CONSTRAINT shifting_events_verification_state_check CHECK (verification_state IN ('unverified', 'verified', 'rejected')),
  CONSTRAINT shifting_events_status_check CHECK (event_status IN ('pending', 'authorized', 'applied', 'rejected', 'canceled', 'unresolved')),
  CONSTRAINT shifting_events_source_check CHECK (source_system IN ('feed_shiftings_docx', 'manual_review', 'legacy_slack', 'import', 'goatos_canonical')),
  CONSTRAINT shifting_events_source_ref_check CHECK (btrim(source_ref) <> ''),
  CONSTRAINT shifting_events_payload_hash_check CHECK (btrim(payload_hash) <> ''),
  CONSTRAINT shifting_events_idem_check CHECK (btrim(idempotency_key) <> '')
);

CREATE UNIQUE INDEX shifting_events_logical_key_unique
  ON shifting_events (tenant_id, logical_shifting_event_key);

CREATE UNIQUE INDEX shifting_events_tenant_id_unique
  ON shifting_events (tenant_id, shifting_event_id);

CREATE UNIQUE INDEX shifting_events_idempotency_unique
  ON shifting_events (tenant_id, idempotency_key);

CREATE INDEX shifting_events_projection_window_idx
  ON shifting_events (tenant_id, event_status, effective_at, destination_park_id, destination_shed_id, shifting_event_id);

CREATE TABLE shifting_event_impacts (
  shifting_event_impact_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  shifting_event_id uuid NOT NULL REFERENCES shifting_events(shifting_event_id) ON DELETE CASCADE,
  grain_key text NOT NULL,
  breed_id uuid NULL REFERENCES breeds(breed_id),
  breed_key text NOT NULL,
  breed_label text NOT NULL,
  stage_tag text NULL,
  age_class text NULL,
  sex text NULL,
  head_count integer NOT NULL,
  pregnant_count integer NOT NULL DEFAULT 0,
  lactating_count integer NOT NULL DEFAULT 0,
  warmup_count integer NOT NULL DEFAULT 0,
  risk_flags jsonb NOT NULL DEFAULT '{}'::jsonb,
  ration_context_resolution_state text NOT NULL DEFAULT 'unresolved',
  ration_context_ref text NULL,
  blocker_reason text NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT shifting_event_impacts_tenant_event_fk
    FOREIGN KEY (tenant_id, shifting_event_id) REFERENCES shifting_events(tenant_id, shifting_event_id) ON DELETE CASCADE,
  CONSTRAINT shifting_event_impacts_grain_key_check CHECK (btrim(grain_key) <> ''),
  CONSTRAINT shifting_event_impacts_breed_key_check CHECK (btrim(breed_key) <> ''),
  CONSTRAINT shifting_event_impacts_head_count_check CHECK (head_count > 0),
  CONSTRAINT shifting_event_impacts_risk_counts_check CHECK (
    pregnant_count >= 0 AND lactating_count >= 0 AND warmup_count >= 0
    AND pregnant_count <= head_count AND lactating_count <= head_count AND warmup_count <= head_count
  ),
  CONSTRAINT shifting_event_impacts_risk_flags_object_check CHECK (jsonb_typeof(risk_flags) = 'object'),
  CONSTRAINT shifting_event_impacts_resolution_check CHECK (ration_context_resolution_state IN ('resolved', 'unresolved', 'blocked', 'not_required'))
);

CREATE UNIQUE INDEX shifting_event_impacts_grain_unique
  ON shifting_event_impacts (tenant_id, shifting_event_id, grain_key);

CREATE INDEX shifting_event_impacts_projection_idx
  ON shifting_event_impacts (tenant_id, lower(breed_key), stage_tag, ration_context_resolution_state);

CREATE TABLE count_projection_snapshots (
  count_projection_snapshot_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  horizon text NOT NULL,
  park_id uuid NOT NULL REFERENCES locations(location_id),
  target_date date NOT NULL,
  as_of timestamptz NOT NULL,
  projection_status text NOT NULL DEFAULT 'blocked',
  source_contract_version text NOT NULL,
  source_hash text NOT NULL,
  base_anchor_ids_hash text NOT NULL,
  shifting_event_ids_hash text NOT NULL,
  row_count integer NOT NULL DEFAULT 0,
  exception_count integer NOT NULL DEFAULT 0,
  generated_by text NOT NULL,
  trace_id text NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  row_version integer NOT NULL DEFAULT 1,
  CONSTRAINT count_projection_snapshots_tenant_park_fk
    FOREIGN KEY (tenant_id, park_id) REFERENCES locations(tenant_id, location_id),
  CONSTRAINT count_projection_snapshots_horizon_check CHECK (horizon IN ('count_as_of', 'feed_target_date')),
  CONSTRAINT count_projection_snapshots_status_check CHECK (projection_status IN ('ready', 'blocked', 'stale', 'failed')),
  CONSTRAINT count_projection_snapshots_row_counts_check CHECK (row_count >= 0 AND exception_count >= 0),
  CONSTRAINT count_projection_snapshots_contract_check CHECK (btrim(source_contract_version) <> ''),
  CONSTRAINT count_projection_snapshots_hash_check CHECK (btrim(source_hash) <> '' AND btrim(base_anchor_ids_hash) <> '' AND btrim(shifting_event_ids_hash) <> '')
);

CREATE UNIQUE INDEX count_projection_snapshots_source_unique
  ON count_projection_snapshots (tenant_id, horizon, park_id, target_date, source_hash);

CREATE UNIQUE INDEX count_projection_snapshots_tenant_id_unique
  ON count_projection_snapshots (tenant_id, count_projection_snapshot_id);

CREATE INDEX count_projection_snapshots_hot_idx
  ON count_projection_snapshots (tenant_id, horizon, park_id, target_date DESC, created_at DESC);

CREATE TABLE count_projection_snapshot_rows (
  count_projection_snapshot_row_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  count_projection_snapshot_id uuid NOT NULL REFERENCES count_projection_snapshots(count_projection_snapshot_id) ON DELETE CASCADE,
  park_id uuid NOT NULL REFERENCES locations(location_id),
  shed_id uuid NOT NULL REFERENCES locations(location_id),
  target_date date NOT NULL,
  grain_key text NOT NULL,
  breed_id uuid NULL REFERENCES breeds(breed_id),
  breed_key text NOT NULL,
  breed_label text NOT NULL,
  stage_tag text NULL,
  age_class text NULL,
  sex text NULL,
  head_count integer NOT NULL,
  pregnant_count integer NOT NULL DEFAULT 0,
  lactating_count integer NOT NULL DEFAULT 0,
  warmup_count integer NOT NULL DEFAULT 0,
  ration_context_resolution_state text NOT NULL DEFAULT 'unresolved',
  ration_context_ref text NULL,
  blocker_reason text NULL,
  source_row_hash text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT count_projection_snapshot_rows_tenant_snapshot_fk
    FOREIGN KEY (tenant_id, count_projection_snapshot_id) REFERENCES count_projection_snapshots(tenant_id, count_projection_snapshot_id) ON DELETE CASCADE,
  CONSTRAINT count_projection_snapshot_rows_tenant_park_fk
    FOREIGN KEY (tenant_id, park_id) REFERENCES locations(tenant_id, location_id),
  CONSTRAINT count_projection_snapshot_rows_tenant_shed_fk
    FOREIGN KEY (tenant_id, shed_id) REFERENCES locations(tenant_id, location_id),
  CONSTRAINT count_projection_snapshot_rows_grain_key_check CHECK (btrim(grain_key) <> ''),
  CONSTRAINT count_projection_snapshot_rows_breed_key_check CHECK (btrim(breed_key) <> ''),
  CONSTRAINT count_projection_snapshot_rows_count_check CHECK (head_count >= 0),
  CONSTRAINT count_projection_snapshot_rows_risk_counts_check CHECK (
    pregnant_count >= 0 AND lactating_count >= 0 AND warmup_count >= 0
    AND pregnant_count <= head_count AND lactating_count <= head_count AND warmup_count <= head_count
  ),
  CONSTRAINT count_projection_snapshot_rows_resolution_check CHECK (ration_context_resolution_state IN ('resolved', 'unresolved', 'blocked', 'not_required')),
  CONSTRAINT count_projection_snapshot_rows_hash_check CHECK (btrim(source_row_hash) <> '')
);

CREATE UNIQUE INDEX count_projection_snapshot_rows_grain_unique
  ON count_projection_snapshot_rows (tenant_id, count_projection_snapshot_id, grain_key);

CREATE INDEX count_projection_snapshot_rows_feed_hot_idx
  ON count_projection_snapshot_rows (tenant_id, target_date, park_id, shed_id, lower(breed_key), count_projection_snapshot_row_id);

CREATE INDEX count_projection_snapshot_rows_blocker_idx
  ON count_projection_snapshot_rows (tenant_id, target_date, ration_context_resolution_state, count_projection_snapshot_row_id)
  WHERE ration_context_resolution_state IN ('unresolved', 'blocked');

CREATE TABLE count_projection_exceptions (
  count_projection_exception_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  count_projection_snapshot_id uuid NULL REFERENCES count_projection_snapshots(count_projection_snapshot_id) ON DELETE SET NULL,
  exception_type text NOT NULL,
  source_key text NOT NULL,
  grain_key text NOT NULL,
  park_id uuid NULL REFERENCES locations(location_id),
  shed_id uuid NULL REFERENCES locations(location_id),
  breed_key text NULL,
  stage_tag text NULL,
  severity text NOT NULL DEFAULT 'blocking',
  status text NOT NULL DEFAULT 'open',
  owner_ref text NULL,
  blocker_reason text NOT NULL,
  evidence_json jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  resolved_at timestamptz NULL,
  CONSTRAINT count_projection_exceptions_tenant_snapshot_fk
    FOREIGN KEY (tenant_id, count_projection_snapshot_id) REFERENCES count_projection_snapshots(tenant_id, count_projection_snapshot_id) ON DELETE SET NULL,
  CONSTRAINT count_projection_exceptions_tenant_park_fk
    FOREIGN KEY (tenant_id, park_id) REFERENCES locations(tenant_id, location_id),
  CONSTRAINT count_projection_exceptions_tenant_shed_fk
    FOREIGN KEY (tenant_id, shed_id) REFERENCES locations(tenant_id, location_id),
  CONSTRAINT count_projection_exceptions_type_check CHECK (exception_type IN ('missing_base_count', 'missing_structured_impact', 'unreported_shifting', 'count_mismatch', 'alias_conflict', 'ration_context_unresolved', 'destination_shortage', 'unsafe_surplus', 'query_plan_unproven')),
  CONSTRAINT count_projection_exceptions_severity_check CHECK (severity IN ('warning', 'blocking', 'critical')),
  CONSTRAINT count_projection_exceptions_status_check CHECK (status IN ('open', 'resolved', 'dismissed')),
  CONSTRAINT count_projection_exceptions_source_key_check CHECK (btrim(source_key) <> ''),
  CONSTRAINT count_projection_exceptions_grain_key_check CHECK (btrim(grain_key) <> ''),
  CONSTRAINT count_projection_exceptions_reason_check CHECK (btrim(blocker_reason) <> ''),
  CONSTRAINT count_projection_exceptions_evidence_object_check CHECK (jsonb_typeof(evidence_json) = 'object')
);

CREATE UNIQUE INDEX count_projection_exceptions_open_unique
  ON count_projection_exceptions (tenant_id, exception_type, source_key, grain_key)
  WHERE status = 'open';

CREATE INDEX count_projection_exceptions_queue_idx
  ON count_projection_exceptions (tenant_id, status, severity, updated_at DESC, count_projection_exception_id DESC);

CREATE TABLE counts_shifting_readiness_subgates (
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  subgate_id text NOT NULL,
  status text NOT NULL DEFAULT 'blocked',
  owner text NOT NULL,
  evidence_ref text NOT NULL,
  blocker_reason text NOT NULL,
  implementation_ref text NULL,
  last_checked_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, subgate_id),
  CONSTRAINT counts_shifting_readiness_subgate_id_check CHECK (subgate_id IN ('CSG1','CSG2','CSG3','CSG4','CSG5','CSG6','CSG7','CSG8','CSG9','CSG10')),
  CONSTRAINT counts_shifting_readiness_status_check CHECK (status IN ('ready', 'blocked', 'pending')),
  CONSTRAINT counts_shifting_readiness_owner_check CHECK (btrim(owner) <> ''),
  CONSTRAINT counts_shifting_readiness_evidence_check CHECK (btrim(evidence_ref) <> ''),
  CONSTRAINT counts_shifting_readiness_blocker_check CHECK (btrim(blocker_reason) <> '')
);

CREATE INDEX counts_shifting_readiness_status_idx
  ON counts_shifting_readiness_subgates (tenant_id, status, updated_at DESC);

-- +goose Down
DROP TABLE IF EXISTS counts_shifting_readiness_subgates;
DROP TABLE IF EXISTS count_projection_exceptions;
DROP TABLE IF EXISTS count_projection_snapshot_rows;
DROP TABLE IF EXISTS count_projection_snapshots;
DROP TABLE IF EXISTS shifting_event_impacts;
DROP TABLE IF EXISTS shifting_events;
DROP TABLE IF EXISTS count_base_anchors;
