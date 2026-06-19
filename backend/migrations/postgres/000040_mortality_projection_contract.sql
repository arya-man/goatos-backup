-- +goose Up
CREATE TABLE mortality_source_rows (
  mortality_source_row_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  source_system text NOT NULL,
  source_table text NOT NULL,
  source_row_key text NOT NULL,
  source_observed_at timestamptz NULL,
  source_watermark text NULL,
  payload_json jsonb NOT NULL,
  payload_hash text NOT NULL,
  row_status text NOT NULL DEFAULT 'current',
  sync_run_id uuid NULL REFERENCES legacy_sync_runs(sync_run_id) ON DELETE SET NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  superseded_at timestamptz NULL,
  CONSTRAINT mortality_source_rows_status_check CHECK (row_status IN ('current', 'superseded', 'invalid', 'ignored')),
  CONSTRAINT mortality_source_rows_payload_object_check CHECK (jsonb_typeof(payload_json) = 'object')
);

CREATE UNIQUE INDEX mortality_source_rows_current_unique
  ON mortality_source_rows (tenant_id, source_system, source_table, source_row_key)
  WHERE row_status = 'current';

CREATE INDEX mortality_source_rows_observed_idx
  ON mortality_source_rows (tenant_id, source_table, source_observed_at, mortality_source_row_id);

CREATE INDEX mortality_source_rows_hash_idx
  ON mortality_source_rows (tenant_id, payload_hash);

CREATE TABLE mortality_events (
  mortality_event_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  logical_event_key text NULL,
  dedup_candidate_key text NULL,
  unresolved_event_ordinal integer NULL,
  dedup_confidence text NOT NULL,
  event_type text NOT NULL,
  event_date date NOT NULL,
  goat_id uuid NULL REFERENCES goats(goat_id),
  source_goat_identifier text NULL,
  source_identifier_kind text NULL,
  age_class text NOT NULL DEFAULT 'unknown',
  breed_key text NULL,
  breed_label text NULL,
  farm_key text NULL,
  farm_label text NULL,
  canonical_farm_location_id uuid NULL REFERENCES locations(location_id),
  canonical_park_location_id uuid NULL REFERENCES locations(location_id),
  canonical_shed_location_id uuid NULL REFERENCES locations(location_id),
  canonical_housing_location_id uuid NULL REFERENCES locations(location_id),
  load_key text NULL,
  load_label text NULL,
  delivery_key text NULL,
  delivery_label text NULL,
  sex text NULL,
  source_row_id uuid NULL REFERENCES mortality_source_rows(mortality_source_row_id) ON DELETE SET NULL,
  event_hash text NOT NULL,
  review_status text NOT NULL DEFAULT 'needs_review',
  idempotency_key text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT mortality_events_dedup_confidence_check CHECK (dedup_confidence IN ('resolved_identity', 'stable_source_identifier', 'candidate_review')),
  CONSTRAINT mortality_events_event_type_check CHECK (event_type IN ('death', 'abortion')),
  CONSTRAINT mortality_events_age_class_check CHECK (age_class IN ('kid', 'adult', 'unknown')),
  CONSTRAINT mortality_events_sex_check CHECK (sex IS NULL OR sex IN ('female', 'male', 'unknown')),
  CONSTRAINT mortality_events_review_status_check CHECK (review_status IN ('accepted', 'needs_review', 'rejected', 'superseded')),
  CONSTRAINT mortality_events_candidate_key_check CHECK (
    logical_event_key IS NOT NULL OR dedup_candidate_key IS NOT NULL
  )
);

CREATE UNIQUE INDEX mortality_events_idempotency_unique
  ON mortality_events (tenant_id, idempotency_key);

CREATE UNIQUE INDEX mortality_events_logical_unique
  ON mortality_events (tenant_id, logical_event_key)
  WHERE logical_event_key IS NOT NULL
    AND dedup_confidence IN ('resolved_identity', 'stable_source_identifier')
    AND review_status IN ('accepted', 'needs_review');

CREATE INDEX mortality_events_event_date_idx
  ON mortality_events (tenant_id, event_date, mortality_event_id);

CREATE INDEX mortality_events_type_date_idx
  ON mortality_events (tenant_id, event_type, event_date);

CREATE INDEX mortality_events_goat_date_idx
  ON mortality_events (tenant_id, goat_id, event_date)
  WHERE goat_id IS NOT NULL;

CREATE INDEX mortality_events_review_idx
  ON mortality_events (tenant_id, review_status, event_date DESC);

CREATE INDEX mortality_events_candidate_idx
  ON mortality_events (tenant_id, dedup_candidate_key, event_date)
  WHERE dedup_candidate_key IS NOT NULL;

CREATE TABLE mortality_review_items (
  review_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  mortality_event_id uuid NULL REFERENCES mortality_events(mortality_event_id) ON DELETE SET NULL,
  review_type text NOT NULL,
  status text NOT NULL DEFAULT 'open',
  source_context text NULL,
  source_label text NULL,
  evidence_json jsonb NOT NULL DEFAULT '{}'::jsonb,
  evidence_hash text NOT NULL,
  sync_run_id uuid NULL REFERENCES legacy_sync_runs(sync_run_id) ON DELETE SET NULL,
  created_by uuid NULL,
  resolved_by uuid NULL,
  resolution_notes text NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  resolved_at timestamptz NULL,
  row_version integer NOT NULL DEFAULT 1,
  CONSTRAINT mortality_review_items_type_check CHECK (review_type IN ('unresolved_identity', 'dimension_conflict', 'denominator_missing', 'source_changed', 'dedup_conflict')),
  CONSTRAINT mortality_review_items_status_check CHECK (status IN ('open', 'resolved', 'dismissed')),
  CONSTRAINT mortality_review_items_evidence_object_check CHECK (jsonb_typeof(evidence_json) = 'object')
);

CREATE UNIQUE INDEX mortality_review_items_unique_open_evidence
  ON mortality_review_items (tenant_id, review_type, evidence_hash)
  WHERE status = 'open';

CREATE INDEX mortality_review_items_queue_idx
  ON mortality_review_items (tenant_id, status, review_type, updated_at DESC);

CREATE TABLE mortality_projection_rows (
  mortality_projection_row_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  period text NOT NULL,
  period_start date NULL,
  period_end date NULL,
  section text NOT NULL,
  grain text NOT NULL,
  dimension_key text NOT NULL,
  dimension_label text NOT NULL,
  metric_key text NOT NULL,
  numerator numeric NULL,
  denominator numeric NULL,
  denominator_source_module text NULL,
  denominator_projection_version bigint NULL,
  denominator_source_watermark text NULL,
  numerator_source_composition text NULL,
  denominator_source_composition text NULL,
  mixed_composition_exception_id uuid NULL,
  value numeric NOT NULL,
  unit text NOT NULL,
  sort_order integer NOT NULL DEFAULT 0,
  projection_version bigint NOT NULL,
  sync_run_id uuid NULL REFERENCES legacy_sync_runs(sync_run_id) ON DELETE SET NULL,
  source_hash text NOT NULL,
  source_composition text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT mortality_projection_period_check CHECK (period IN ('overall', 'this-month', 'month-wise')),
  CONSTRAINT mortality_projection_section_check CHECK (section IN ('summary', 'breed', 'farm', 'load', 'delivery', 'trends', 'gender', 'status', 'housing')),
  CONSTRAINT mortality_projection_grain_check CHECK (grain IN ('total', 'breed', 'farm', 'load', 'delivery', 'month', 'age_class', 'sex', 'status', 'housing')),
  CONSTRAINT mortality_projection_unit_check CHECK (unit IN ('count', 'percent', 'ratio')),
  CONSTRAINT mortality_projection_source_composition_check CHECK (source_composition IN ('legacy_only', 'canonical_only', 'blended')),
  CONSTRAINT mortality_projection_numerator_composition_check CHECK (numerator_source_composition IS NULL OR numerator_source_composition IN ('legacy_only', 'canonical_only', 'blended')),
  CONSTRAINT mortality_projection_denominator_composition_check CHECK (denominator_source_composition IS NULL OR denominator_source_composition IN ('legacy_only', 'canonical_only', 'blended'))
);

CREATE INDEX mortality_projection_hot_read_idx
  ON mortality_projection_rows (tenant_id, period, section, grain, sort_order, mortality_projection_row_id);

CREATE INDEX mortality_projection_metric_idx
  ON mortality_projection_rows (tenant_id, period, section, metric_key);

CREATE INDEX mortality_projection_version_idx
  ON mortality_projection_rows (tenant_id, projection_version);

CREATE TABLE mortality_projection_state (
  mortality_projection_state_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  period text NULL,
  last_successful_run_id uuid NULL REFERENCES legacy_sync_runs(sync_run_id) ON DELETE SET NULL,
  last_success_at timestamptz NULL,
  source_watermark text NULL,
  projection_version bigint NOT NULL DEFAULT 0,
  freshness_status text NOT NULL DEFAULT 'unknown',
  serving_state text NOT NULL DEFAULT 'never_synced',
  source_composition text NOT NULL DEFAULT 'legacy_only',
  row_count integer NOT NULL DEFAULT 0,
  conflict_count integer NOT NULL DEFAULT 0,
  unavailable_sources jsonb NOT NULL DEFAULT '[]'::jsonb,
  rebuild_required boolean NOT NULL DEFAULT false,
  last_error text NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT mortality_projection_state_period_check CHECK (period IS NULL OR period IN ('overall', 'this-month', 'month-wise')),
  CONSTRAINT mortality_projection_state_freshness_check CHECK (freshness_status IN ('green', 'yellow', 'red', 'unknown')),
  CONSTRAINT mortality_projection_state_serving_check CHECK (serving_state IN ('never_synced', 'fresh', 'stale', 'rebuilding', 'failed', 'source_unavailable')),
  CONSTRAINT mortality_projection_state_source_composition_check CHECK (source_composition IN ('legacy_only', 'canonical_only', 'blended')),
  CONSTRAINT mortality_projection_state_unavailable_array_check CHECK (jsonb_typeof(unavailable_sources) = 'array')
);

CREATE UNIQUE INDEX mortality_projection_state_unique_period
  ON mortality_projection_state (tenant_id, COALESCE(period, '__all__'));

CREATE INDEX mortality_projection_state_updated_idx
  ON mortality_projection_state (tenant_id, updated_at DESC);

-- +goose Down
DROP TABLE IF EXISTS mortality_projection_state;
DROP TABLE IF EXISTS mortality_projection_rows;
DROP TABLE IF EXISTS mortality_review_items;
DROP TABLE IF EXISTS mortality_events;
DROP TABLE IF EXISTS mortality_source_rows;
