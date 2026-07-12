-- +goose Up
-- Version-swapped CQRS read model for GET /vaccination/execution. The canonical replay remains an
-- off-request projector input only; request handlers never fall back to it.
ALTER TABLE vaccination_shed_projection_state
  ADD COLUMN due_before timestamptz;
UPDATE vaccination_shed_projection_state
SET due_before = as_of + interval '30 days'
WHERE due_before IS NULL;
ALTER TABLE vaccination_shed_projection_state
  ALTER COLUMN due_before SET NOT NULL;

CREATE TABLE vaccination_execution_projection_rows (
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  projection_version bigint NOT NULL,
  park_id text NOT NULL,
  park_name text NOT NULL,
  shed_id text NOT NULL,
  shed_name text NOT NULL,
  animal_stage text NOT NULL,
  batch_id text,
  protocol_name text NOT NULL,
  dose_code text NOT NULL,
  due_at timestamptz,
  obligation_count bigint NOT NULL,
  scheduled_count bigint NOT NULL,
  due_count bigint NOT NULL,
  in_progress_count bigint NOT NULL,
  completed_count bigint NOT NULL,
  missed_count bigint NOT NULL,
  deferred_count bigint NOT NULL,
  canceled_count bigint NOT NULL,
  completion_recorded bigint NOT NULL,
  completion_accepted bigint NOT NULL,
  completion_rejected bigint NOT NULL,
  completion_reversed bigint NOT NULL,
  batch_status text,
  task_state text,
  operator_name text,
  park_head_name text,
  verifier_name text,
  usable_for_vaccination boolean NOT NULL,
  is_quarantine boolean NOT NULL,
  is_icu boolean NOT NULL,
  health_deferred_count bigint NOT NULL,
  obligation_id text,
  sop_task_id text,
  sop_version_id text,
  sop_task_row_version integer,
  completion_id text,
  work_state text NOT NULL,
  severity text NOT NULL,
  sort_rank integer NOT NULL,
  sort_due_micros bigint NOT NULL,
  sort_row_key text NOT NULL,
  projected_at timestamptz NOT NULL,
  CONSTRAINT vaccination_execution_projection_rows_pk PRIMARY KEY (tenant_id, projection_version, sort_row_key),
  CONSTRAINT vaccination_execution_projection_work_state_check CHECK (work_state IN (
    'due','overdue','scheduled','in_progress','proof_pending','verification_pending','rejected',
    'deferred','missed','blocked','completed'
  )),
  CONSTRAINT vaccination_execution_projection_severity_check CHECK (severity IN ('ok','watch','at_risk','broken'))
);

CREATE INDEX vaccination_execution_projection_page_idx
  ON vaccination_execution_projection_rows (tenant_id, projection_version, sort_rank, sort_due_micros, sort_row_key);
CREATE INDEX vaccination_execution_projection_park_idx
  ON vaccination_execution_projection_rows (tenant_id, projection_version, park_id, sort_rank, sort_due_micros, sort_row_key);
CREATE INDEX vaccination_execution_projection_shed_idx
  ON vaccination_execution_projection_rows (tenant_id, projection_version, shed_id, sort_rank, sort_due_micros, sort_row_key);
CREATE INDEX vaccination_execution_projection_state_idx
  ON vaccination_execution_projection_rows (tenant_id, projection_version, work_state, severity, sort_rank, sort_due_micros, sort_row_key);

CREATE TABLE vaccination_execution_projection_state (
  tenant_id uuid PRIMARY KEY REFERENCES tenants(tenant_id),
  projection_version bigint NOT NULL,
  serving_projection_version bigint,
  projected_at timestamptz NOT NULL,
  as_of timestamptz NOT NULL,
  due_before timestamptz NOT NULL,
  closed_after timestamptz NOT NULL,
  row_count bigint NOT NULL DEFAULT 0 CHECK (row_count >= 0),
  freshness_status text NOT NULL DEFAULT 'unknown' CHECK (freshness_status IN ('green','yellow','red','unknown')),
  serving_state text NOT NULL DEFAULT 'never_synced' CHECK (serving_state IN ('never_synced','fresh','stale','rebuilding','failed')),
  last_error text,
  updated_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS vaccination_execution_projection_state;
DROP TABLE IF EXISTS vaccination_execution_projection_rows;
ALTER TABLE vaccination_shed_projection_state DROP COLUMN IF EXISTS due_before;
