-- +goose Up
CREATE TABLE legacy_sync_sources (
  source_id text PRIMARY KEY,
  source_name text NOT NULL,
  domain text NOT NULL,
  source_kind text NOT NULL,
  bq_project text NULL,
  bq_dataset text NULL,
  bq_table_or_config text NULL,
  cadence_seconds int NOT NULL,
  green_within_seconds int NOT NULL,
  yellow_within_seconds int NOT NULL,
  criticality text NOT NULL,
  enabled boolean NOT NULL DEFAULT true,
  known_degraded boolean NOT NULL DEFAULT false,
  notes text NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT legacy_sync_sources_source_id_check CHECK (source_id ~ '^[a-z0-9][a-z0-9_:-]{1,119}$'),
  CONSTRAINT legacy_sync_sources_domain_check CHECK (domain IN ('identity', 'lifecycle', 'current_location', 'active_count', 'feed', 'unknown')),
  CONSTRAINT legacy_sync_sources_kind_check CHECK (source_kind IN ('scheduled_query', 'table', 'view', 'export')),
  CONSTRAINT legacy_sync_sources_cadence_check CHECK (cadence_seconds > 0),
  CONSTRAINT legacy_sync_sources_green_check CHECK (green_within_seconds >= cadence_seconds),
  CONSTRAINT legacy_sync_sources_yellow_check CHECK (yellow_within_seconds >= green_within_seconds),
  CONSTRAINT legacy_sync_sources_criticality_check CHECK (criticality IN ('critical', 'noncritical'))
);

CREATE TABLE legacy_sync_runs (
  sync_run_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  requested_by uuid NOT NULL,
  mode text NOT NULL,
  domain text NOT NULL,
  status text NOT NULL,
  cold_start boolean NOT NULL DEFAULT false,
  source_window_start timestamptz NULL,
  source_window_end timestamptz NULL,
  eta_seconds int NULL,
  rows_read int NOT NULL DEFAULT 0,
  rows_planned int NOT NULL DEFAULT 0,
  rows_applied int NOT NULL DEFAULT 0,
  rows_skipped int NOT NULL DEFAULT 0,
  goats_created int NOT NULL DEFAULT 0,
  goats_updated int NOT NULL DEFAULT 0,
  conflicts_opened int NOT NULL DEFAULT 0,
  conflicts_refreshed int NOT NULL DEFAULT 0,
  counters_rebuilt boolean NOT NULL DEFAULT false,
  counter_check_status text NOT NULL DEFAULT 'pending',
  freshness_status text NOT NULL DEFAULT 'unknown',
  blocked_reason text NULL,
  cancel_requested_at timestamptz NULL,
  started_at timestamptz NOT NULL DEFAULT now(),
  completed_at timestamptz NULL,
  trace_id text NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT legacy_sync_runs_mode_check CHECK (mode IN ('dry_run', 'execute', 'nightly_deep_reconcile')),
  CONSTRAINT legacy_sync_runs_domain_check CHECK (domain IN ('all', 'identity', 'lifecycle', 'current_location', 'active_count')),
  CONSTRAINT legacy_sync_runs_status_check CHECK (status IN ('planning', 'running', 'completed', 'failed', 'canceled', 'blocked')),
  CONSTRAINT legacy_sync_runs_counter_check CHECK (counter_check_status IN ('pending', 'succeeded', 'failed', 'skipped')),
  CONSTRAINT legacy_sync_runs_freshness_check CHECK (freshness_status IN ('green', 'yellow', 'red', 'unknown')),
  CONSTRAINT legacy_sync_runs_counts_check CHECK (
    rows_read >= 0
    AND rows_planned >= 0
    AND rows_applied >= 0
    AND rows_skipped >= 0
    AND goats_created >= 0
    AND goats_updated >= 0
    AND conflicts_opened >= 0
    AND conflicts_refreshed >= 0
  )
);

CREATE INDEX legacy_sync_runs_tenant_started_idx
  ON legacy_sync_runs (tenant_id, started_at DESC, sync_run_id DESC);

CREATE UNIQUE INDEX legacy_sync_runs_one_active_tenant_domain_idx
  ON legacy_sync_runs (tenant_id, domain)
  WHERE status IN ('planning', 'running');

CREATE TABLE legacy_sync_run_steps (
  sync_step_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  sync_run_id uuid NOT NULL REFERENCES legacy_sync_runs(sync_run_id) ON DELETE CASCADE,
  source_id text NULL,
  step_name text NOT NULL,
  status text NOT NULL,
  rows_read int NOT NULL DEFAULT 0,
  rows_planned int NOT NULL DEFAULT 0,
  rows_applied int NOT NULL DEFAULT 0,
  rows_skipped int NOT NULL DEFAULT 0,
  details jsonb NOT NULL DEFAULT '{}'::jsonb,
  started_at timestamptz NOT NULL DEFAULT now(),
  completed_at timestamptz NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT legacy_sync_run_steps_status_check CHECK (status IN ('pending', 'running', 'completed', 'failed', 'canceled', 'blocked')),
  CONSTRAINT legacy_sync_run_steps_counts_check CHECK (
    rows_read >= 0
    AND rows_planned >= 0
    AND rows_applied >= 0
    AND rows_skipped >= 0
  )
);

CREATE INDEX legacy_sync_run_steps_run_idx
  ON legacy_sync_run_steps (sync_run_id, started_at ASC, sync_step_id ASC);

CREATE TABLE legacy_sync_source_watermarks (
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  source_id text NOT NULL,
  last_success_window_start timestamptz NULL,
  last_success_window_end timestamptz NULL,
  last_success_at timestamptz NULL,
  last_success_sync_run_id uuid NULL REFERENCES legacy_sync_runs(sync_run_id) ON DELETE SET NULL,
  checkpoint jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, source_id)
);

CREATE TABLE legacy_sync_source_status (
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  source_id text NOT NULL,
  freshness_status text NOT NULL,
  status_reason text NOT NULL,
  source_watermark_at timestamptz NULL,
  observed_at timestamptz NOT NULL DEFAULT now(),
  last_success_sync_run_id uuid NULL REFERENCES legacy_sync_runs(sync_run_id) ON DELETE SET NULL,
  latest_error text NULL,
  rows_seen int NOT NULL DEFAULT 0,
  is_unknown_source boolean NOT NULL DEFAULT false,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, source_id),
  CONSTRAINT legacy_sync_source_status_freshness_check CHECK (freshness_status IN ('green', 'yellow', 'red', 'unknown')),
  CONSTRAINT legacy_sync_source_status_rows_check CHECK (rows_seen >= 0)
);

CREATE INDEX legacy_sync_source_status_tenant_freshness_idx
  ON legacy_sync_source_status (tenant_id, freshness_status, source_id);

CREATE TABLE legacy_sync_run_conflicts (
  sync_run_conflict_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  sync_run_id uuid NOT NULL REFERENCES legacy_sync_runs(sync_run_id) ON DELETE CASCADE,
  source_id text NOT NULL,
  source_record_id text NOT NULL,
  source_conflict_key text NOT NULL,
  conflict_id uuid NULL,
  goat_id uuid NULL,
  evidence_reason text NOT NULL,
  result text NOT NULL,
  old_goatos_value text NULL,
  new_legacy_value text NULL,
  previous_decision_id uuid NULL,
  previous_decision_at timestamptz NULL,
  audit_id uuid NULL,
  evidence jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT legacy_sync_run_conflicts_reason_check CHECK (evidence_reason IN (
    'bq_gender_self_conflict',
    'bq_breed_self_conflict',
    'legacy_changed_after_human_review',
    'status_mismatch',
    'unregistered_source'
  )),
  CONSTRAINT legacy_sync_run_conflicts_result_check CHECK (result IN (
    'applied',
    'skipped',
    'blocked',
    'conflict_opened',
    'conflict_refreshed',
    'preserved_human_decision',
    'reconciled'
  ))
);

CREATE UNIQUE INDEX legacy_sync_run_conflicts_unique_run_source_key
  ON legacy_sync_run_conflicts (tenant_id, sync_run_id, source_conflict_key);

CREATE INDEX legacy_sync_run_conflicts_run_idx
  ON legacy_sync_run_conflicts (sync_run_id, created_at DESC);

INSERT INTO legacy_sync_sources (
  source_id,
  source_name,
  domain,
  source_kind,
  bq_project,
  bq_dataset,
  bq_table_or_config,
  cadence_seconds,
  green_within_seconds,
  yellow_within_seconds,
  criticality,
  enabled,
  known_degraded,
  notes
) VALUES
  (
    'phase1_identity_attribute_evidence',
    'Phase 1 identity attribute evidence',
    'identity',
    'export',
    'goatos-sheets',
    'goatsDB',
    'bounded_bq_reconcile_attribute_export',
    900,
    1800,
    2700,
    'critical',
    true,
    false,
    'Seeded from the committed BQ reconcile replay path. Live scheduled-query inventory remains pending.'
  ),
  (
    'phase1_lifecycle_event_evidence',
    'Phase 1 lifecycle event evidence',
    'lifecycle',
    'export',
    'goatos-sheets',
    'goatsDB',
    'bounded_bq_reconcile_event_export',
    900,
    1800,
    2700,
    'critical',
    true,
    false,
    'Feeds lifecycle reconciliation through bounded completed source windows.'
  ),
  (
    'phase1_current_location_evidence',
    'Phase 1 current-location evidence',
    'current_location',
    'export',
    'goatos-sheets',
    'goatsDB',
    'bounded_bq_reconcile_latest_location_export',
    3600,
    5400,
    7200,
    'critical',
    true,
    false,
    'Feeds deterministic current-location reconciliation; healthy 60-minute cadence is not globally red.'
  ),
  (
    'phase1_active_count_parity',
    'Phase 1 active-count parity evidence',
    'active_count',
    'export',
    'goatos-sheets',
    'ceo_dashboard',
    'bounded_active_count_parity_export',
    43200,
    46800,
    54000,
    'critical',
    true,
    false,
    '12-hour active-count parity source; used for drift reports, not direct goat creation.'
  )
ON CONFLICT (source_id) DO NOTHING;

-- +goose Down
DROP TABLE IF EXISTS legacy_sync_run_conflicts;
DROP TABLE IF EXISTS legacy_sync_source_status;
DROP TABLE IF EXISTS legacy_sync_source_watermarks;
DROP TABLE IF EXISTS legacy_sync_run_steps;
DROP TABLE IF EXISTS legacy_sync_runs;
DROP TABLE IF EXISTS legacy_sync_sources;
