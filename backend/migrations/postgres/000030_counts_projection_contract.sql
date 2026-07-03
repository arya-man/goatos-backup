-- +goose Up
CREATE TABLE feature_coverage_registry (
  coverage_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  feature_module text NOT NULL,
  section text NOT NULL,
  metric_key text NOT NULL,
  grain_key text NOT NULL,
  covered_window text NOT NULL,
  source_mode text NOT NULL,
  coverage_status text NOT NULL,
  canonical_source_version text NULL,
  legacy_source_version text NULL,
  shadow_parity_artifact_path text NULL,
  approving_actor uuid NULL,
  approved_at timestamptz NULL,
  audit_id uuid NULL,
  rollback_policy text NULL,
  expires_at timestamptz NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  row_version integer NOT NULL DEFAULT 1,
  CONSTRAINT feature_coverage_registry_module_check CHECK (feature_module IN ('counts', 'mortality', 'locations')),
  CONSTRAINT feature_coverage_registry_source_mode_check CHECK (source_mode IN ('legacy_bq', 'legacy_sheet', 'goatos_canonical', 'manual_review')),
  CONSTRAINT feature_coverage_registry_status_check CHECK (coverage_status IN ('proposed', 'shadow_passed', 'complete', 'blocked'))
);

CREATE UNIQUE INDEX feature_coverage_registry_unique_grain
  ON feature_coverage_registry (tenant_id, feature_module, section, metric_key, grain_key, covered_window, source_mode);

CREATE INDEX feature_coverage_registry_status_idx
  ON feature_coverage_registry (tenant_id, feature_module, coverage_status, updated_at DESC);

CREATE TABLE counts_source_rows (
  counts_source_row_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  source_system text NOT NULL,
  source_id text NOT NULL,
  source_table text NOT NULL,
  source_row_key text NOT NULL,
  source_observed_at timestamptz NULL,
  source_watermark_date date NULL,
  payload_json jsonb NOT NULL,
  payload_hash text NOT NULL,
  row_status text NOT NULL DEFAULT 'current',
  sync_run_id uuid NULL REFERENCES legacy_sync_runs(sync_run_id) ON DELETE SET NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  superseded_at timestamptz NULL,
  CONSTRAINT counts_source_rows_status_check CHECK (row_status IN ('current', 'superseded', 'invalid', 'ignored')),
  CONSTRAINT counts_source_rows_payload_object_check CHECK (jsonb_typeof(payload_json) = 'object')
);

CREATE UNIQUE INDEX counts_source_rows_current_unique
  ON counts_source_rows (tenant_id, source_system, source_id, source_row_key)
  WHERE row_status = 'current';

CREATE INDEX counts_source_rows_watermark_idx
  ON counts_source_rows (tenant_id, source_id, source_watermark_date, counts_source_row_id);

CREATE INDEX counts_source_rows_sync_run_idx
  ON counts_source_rows (tenant_id, sync_run_id);

CREATE TABLE counts_current_snapshot_rows (
  counts_snapshot_row_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  snapshot_date date NOT NULL,
  source_mode text NOT NULL,
  row_kind text NOT NULL,
  tab_scope text NULL,
  farm_key text NULL,
  farm_label text NULL,
  farm_id uuid NULL REFERENCES locations(location_id),
  park_id uuid NULL REFERENCES locations(location_id),
  shed_key text NULL,
  shed_label text NULL,
  shed_id uuid NULL REFERENCES locations(location_id),
  resolved_location_id uuid NULL REFERENCES locations(location_id),
  resolved_location_type text NULL,
  status_key text NULL,
  status_label text NULL,
  breed_key text NULL,
  breed_label text NULL,
  breed_id uuid NULL REFERENCES breeds(breed_id),
  age_class text NULL,
  source_age_label text NULL,
  sex text NULL,
  metric_name text NOT NULL,
  count_value bigint NULL,
  weight_kg numeric NULL,
  value_inr numeric NULL,
  source_row_id uuid NULL REFERENCES counts_source_rows(counts_source_row_id) ON DELETE SET NULL,
  logical_fact_key text NOT NULL,
  projection_input_hash text NOT NULL,
  sync_run_id uuid NULL REFERENCES legacy_sync_runs(sync_run_id) ON DELETE SET NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT counts_snapshot_source_mode_check CHECK (source_mode IN ('legacy_bq', 'goatos_canonical')),
  CONSTRAINT counts_snapshot_row_kind_check CHECK (row_kind IN ('detail_count', 'summary_kpi', 'age_gender_kpi', 'core_gender_breed')),
  CONSTRAINT counts_snapshot_age_class_check CHECK (age_class IS NULL OR age_class IN ('adult', 'kid', 'unknown')),
  CONSTRAINT counts_snapshot_sex_check CHECK (sex IS NULL OR sex IN ('female', 'male')),
  CONSTRAINT counts_snapshot_nonnegative_count_check CHECK (count_value IS NULL OR count_value >= 0)
);

CREATE UNIQUE INDEX counts_snapshot_logical_fact_unique
  ON counts_current_snapshot_rows (tenant_id, snapshot_date, source_mode, logical_fact_key);

CREATE INDEX counts_snapshot_kind_idx
  ON counts_current_snapshot_rows (tenant_id, snapshot_date, row_kind, metric_name);

CREATE INDEX counts_snapshot_farm_idx
  ON counts_current_snapshot_rows (tenant_id, snapshot_date, farm_key);

CREATE INDEX counts_snapshot_location_idx
  ON counts_current_snapshot_rows (tenant_id, snapshot_date, resolved_location_id);

CREATE INDEX counts_snapshot_sync_run_idx
  ON counts_current_snapshot_rows (tenant_id, sync_run_id);

CREATE TABLE counts_projection_rows (
  counts_projection_row_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  view_id text NOT NULL,
  snapshot_date date NOT NULL,
  summary_source_date date NULL,
  section text NOT NULL,
  grain text NOT NULL,
  dimension_key text NOT NULL,
  dimension_label text NOT NULL,
  secondary_dimension_key text NULL,
  secondary_dimension_label text NULL,
  metric_key text NOT NULL,
  count_value bigint NULL,
  numeric_value numeric NULL,
  unit text NOT NULL,
  denominator numeric NULL,
  sort_order integer NOT NULL DEFAULT 0,
  projection_version bigint NOT NULL,
  sync_run_id uuid NULL REFERENCES legacy_sync_runs(sync_run_id) ON DELETE SET NULL,
  source_hash text NOT NULL,
  source_composition text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT counts_projection_view_check CHECK (view_id IN ('overall', 'core-farms', 'cbe', 'cpt', 'holdings')),
  CONSTRAINT counts_projection_section_check CHECK (section IN ('summary', 'status', 'breed', 'status_breed', 'farm', 'farm_distribution', 'age', 'adults_gender', 'kids_gender', 'kids_stage_gender', 'fattening_gender', 'core_farm_gender_breed')),
  CONSTRAINT counts_projection_grain_check CHECK (grain IN ('metric', 'status', 'breed', 'status_breed', 'farm', 'age_class', 'sex', 'breed_sex')),
  CONSTRAINT counts_projection_unit_check CHECK (unit IN ('count', 'kg', 'inr', 'kg_per_goat', 'percent')),
  CONSTRAINT counts_projection_source_composition_check CHECK (source_composition IN ('legacy_only', 'canonical_only', 'blended')),
  CONSTRAINT counts_projection_nonnegative_count_check CHECK (count_value IS NULL OR count_value >= 0)
);

CREATE INDEX counts_projection_hot_read_idx
  ON counts_projection_rows (tenant_id, view_id, snapshot_date, section, sort_order, counts_projection_row_id);

CREATE INDEX counts_projection_metric_idx
  ON counts_projection_rows (tenant_id, view_id, snapshot_date, metric_key);

CREATE INDEX counts_projection_version_idx
  ON counts_projection_rows (tenant_id, projection_version);

CREATE INDEX counts_projection_sync_run_idx
  ON counts_projection_rows (tenant_id, sync_run_id);

CREATE TABLE counts_projection_state (
  counts_projection_state_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  view_id text NULL,
  last_successful_run_id uuid NULL REFERENCES legacy_sync_runs(sync_run_id) ON DELETE SET NULL,
  last_success_at timestamptz NULL,
  snapshot_date date NULL,
  summary_source_date date NULL,
  source_watermark text NULL,
  projection_version bigint NOT NULL DEFAULT 0,
  freshness_status text NOT NULL DEFAULT 'unknown',
  serving_state text NOT NULL DEFAULT 'never_synced',
  row_count integer NOT NULL DEFAULT 0,
  conflict_count integer NOT NULL DEFAULT 0,
  unavailable_sources jsonb NOT NULL DEFAULT '[]'::jsonb,
  source_composition text NOT NULL DEFAULT 'legacy_only',
  rebuild_required boolean NOT NULL DEFAULT false,
  last_error text NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT counts_projection_state_view_check CHECK (view_id IS NULL OR view_id IN ('overall', 'core-farms', 'cbe', 'cpt', 'holdings')),
  CONSTRAINT counts_projection_state_freshness_check CHECK (freshness_status IN ('green', 'yellow', 'red', 'unknown')),
  CONSTRAINT counts_projection_state_serving_check CHECK (serving_state IN ('never_synced', 'fresh', 'stale', 'rebuilding', 'failed', 'source_unavailable')),
  CONSTRAINT counts_projection_state_source_composition_check CHECK (source_composition IN ('legacy_only', 'canonical_only', 'blended')),
  CONSTRAINT counts_projection_state_unavailable_array_check CHECK (jsonb_typeof(unavailable_sources) = 'array')
);

CREATE UNIQUE INDEX counts_projection_state_unique_view
  ON counts_projection_state (tenant_id, COALESCE(view_id, '__all__'));

CREATE INDEX counts_projection_state_updated_idx
  ON counts_projection_state (tenant_id, updated_at DESC);

-- +goose Down
DROP TABLE IF EXISTS counts_projection_state;
DROP TABLE IF EXISTS counts_projection_rows;
DROP TABLE IF EXISTS counts_current_snapshot_rows;
DROP TABLE IF EXISTS counts_source_rows;
DROP TABLE IF EXISTS feature_coverage_registry;
