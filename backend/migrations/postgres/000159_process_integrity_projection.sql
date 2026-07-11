-- +goose Up
-- Durable process-integrity read model for Control Tower, Action Center,
-- Protocol Adherence, and Workflow detail. Request paths must read this table
-- when it is fresh; the canonical replay query is reserved for projector
-- rebuild/parity/debug fallback.
CREATE TABLE process_integrity_projection_rows (
  process_integrity_projection_row_id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  category text NOT NULL,
  row_id text NOT NULL,
  sort_priority integer NOT NULL,
  process_key text NOT NULL,
  obligation_id text NOT NULL,
  batch_id text,
  sop_task_id text,
  sop_task_row_version integer,
  sop_submission_id text,
  completion_id text,
  park_id text NOT NULL DEFAULT '',
  park_name text NOT NULL DEFAULT '',
  shed_id text NOT NULL DEFAULT '',
  shed_name text NOT NULL DEFAULT '',
  cohort_id text,
  goat_id text,
  animal_stage text NOT NULL DEFAULT '',
  protocol_id text NOT NULL DEFAULT '',
  protocol_version_id text NOT NULL DEFAULT '',
  rule_id text NOT NULL DEFAULT '',
  protocol_name text NOT NULL DEFAULT '',
  dose_code text NOT NULL DEFAULT '',
  drive_name text,
  sop_version_id text,
  proof_policy text NOT NULL DEFAULT '{}',
  due_at timestamptz NOT NULL,
  window_start timestamptz,
  window_end timestamptz,
  expected_count integer NOT NULL DEFAULT 0,
  obligation_status text NOT NULL DEFAULT '',
  batch_status text,
  sop_state text NOT NULL DEFAULT 'not_started',
  submission_state text,
  proof_state text NOT NULL DEFAULT 'missing',
  verification_state text NOT NULL DEFAULT 'not_ready',
  completion_state text,
  completed_count integer NOT NULL DEFAULT 0,
  proof_count integer NOT NULL DEFAULT 0,
  rejected_count integer NOT NULL DEFAULT 0,
  deferred_count integer NOT NULL DEFAULT 0,
  work_state text NOT NULL,
  gap_type text NOT NULL DEFAULT '',
  severity text NOT NULL,
  blocker_reason text,
  owner_state text NOT NULL,
  next_action text NOT NULL DEFAULT '',
  process_intact boolean NOT NULL DEFAULT false,
  operator_id text,
  operator_name text,
  park_head_id text,
  park_head_name text,
  verifier_id text,
  verifier_name text,
  escalation_owner_id text,
  escalation_owner_name text,
  owner_refs text[] NOT NULL DEFAULT '{}'::text[],
  proof_ids text NOT NULL DEFAULT '',
  evidence_count integer NOT NULL DEFAULT 0,
  latest_evidence_at timestamptz,
  latest_rejection_reason text,
  audit_ref text,
  projection_version bigint NOT NULL,
  projected_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT process_integrity_projection_category_check CHECK (category IN ('vaccination', 'feed_direction')),
  CONSTRAINT process_integrity_projection_work_state_check CHECK (work_state IN (
    'scheduled', 'due', 'overdue', 'in_progress', 'proof_pending',
    'verification_pending', 'rejected', 'deferred', 'missed', 'blocked', 'completed'
  )),
  CONSTRAINT process_integrity_projection_severity_check CHECK (severity IN ('ok', 'watch', 'at_risk', 'broken')),
  CONSTRAINT process_integrity_projection_owner_state_check CHECK (owner_state IN ('assigned', 'missing')),
  CONSTRAINT process_integrity_projection_nonnegative_check CHECK (
    expected_count >= 0 AND completed_count >= 0 AND proof_count >= 0
    AND rejected_count >= 0 AND deferred_count >= 0 AND evidence_count >= 0
  )
);

COMMENT ON TABLE process_integrity_projection_rows IS
  'Read model. Precomputed process-integrity rows for command-lens APIs. Canonical source tables remain truth; projector/recompute is the only writer.';

CREATE UNIQUE INDEX process_integrity_projection_rows_row_uidx
  ON process_integrity_projection_rows (tenant_id, row_id);

CREATE INDEX process_integrity_projection_rows_hot_idx
  ON process_integrity_projection_rows (tenant_id, category, sort_priority, due_at, row_id);

CREATE INDEX process_integrity_projection_rows_scope_idx
  ON process_integrity_projection_rows (tenant_id, category, park_id, shed_id, due_at, row_id);

CREATE INDEX process_integrity_projection_rows_work_idx
  ON process_integrity_projection_rows (tenant_id, category, work_state, sort_priority, due_at, row_id);

CREATE INDEX process_integrity_projection_rows_severity_idx
  ON process_integrity_projection_rows (tenant_id, category, severity, sort_priority, due_at, row_id);

CREATE INDEX process_integrity_projection_rows_protocol_idx
  ON process_integrity_projection_rows (tenant_id, category, protocol_version_id, due_at, row_id);

CREATE INDEX process_integrity_projection_rows_owner_gin_idx
  ON process_integrity_projection_rows USING gin (owner_refs);

CREATE INDEX process_integrity_projection_rows_due_idx
  ON process_integrity_projection_rows (tenant_id, category, due_at, row_id);

CREATE TABLE process_integrity_projection_state (
  tenant_id uuid PRIMARY KEY REFERENCES tenants(tenant_id),
  projection_version bigint NOT NULL,
  projected_at timestamptz NOT NULL,
  as_of timestamptz NOT NULL,
  row_count bigint NOT NULL DEFAULT 0,
  freshness_status text NOT NULL DEFAULT 'unknown',
  serving_state text NOT NULL DEFAULT 'never_synced',
  last_error text,
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT process_integrity_projection_state_freshness_check CHECK (freshness_status IN ('green', 'yellow', 'red', 'unknown')),
  CONSTRAINT process_integrity_projection_state_serving_check CHECK (serving_state IN ('never_synced', 'fresh', 'stale', 'rebuilding', 'failed')),
  CONSTRAINT process_integrity_projection_state_row_count_check CHECK (row_count >= 0)
);

CREATE INDEX process_integrity_projection_state_updated_idx
  ON process_integrity_projection_state (updated_at DESC);

-- +goose Down
DROP TABLE IF EXISTS process_integrity_projection_state;
DROP TABLE IF EXISTS process_integrity_projection_rows;
