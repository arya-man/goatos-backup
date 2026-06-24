-- +goose Up
-- Rework must keep rejection history without blocking a corrected resubmission. Keep at most one
-- active completion per obligation/goat while allowing rejected/reversed attempts to remain as
-- immutable history.
ALTER TABLE vaccination_completions
  DROP CONSTRAINT IF EXISTS vaccination_completions_obligation_goat_unique;

CREATE UNIQUE INDEX vaccination_completions_obligation_goat_active_unique_idx
  ON vaccination_completions (tenant_id, obligation_id, goat_id)
  WHERE status IN ('recorded', 'accepted');

-- Durable retry point for task-level review fanout. The SOP task review transaction writes one row
-- keyed by the resulting task row_version; the application marks it completed or failed after the
-- idempotent vaccination fanout runs. A failed row is safe to retry because the downstream handlers
-- only act on still-recorded completions.
CREATE TABLE sop_task_review_fanouts (
  review_fanout_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  task_id uuid NOT NULL REFERENCES sop_tasks(task_id),
  task_row_version int NOT NULL,
  outcome text NOT NULL,
  status text NOT NULL DEFAULT 'pending',
  requested_by uuid NULL,
  reason text NOT NULL DEFAULT '',
  attempt_count int NOT NULL DEFAULT 0,
  last_error text NOT NULL DEFAULT '',
  completed_at timestamptz NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT sop_task_review_fanouts_outcome_check CHECK (outcome IN ('accepted', 'rework_requested')),
  CONSTRAINT sop_task_review_fanouts_status_check CHECK (status IN ('pending', 'completed', 'failed', 'superseded')),
  CONSTRAINT sop_task_review_fanouts_row_version_check CHECK (task_row_version >= 1),
  CONSTRAINT sop_task_review_fanouts_attempt_check CHECK (attempt_count >= 0),
  CONSTRAINT sop_task_review_fanouts_unique UNIQUE (tenant_id, task_id, task_row_version, outcome)
);

CREATE INDEX sop_task_review_fanouts_retry_idx
  ON sop_task_review_fanouts (tenant_id, status, updated_at, review_fanout_id)
  WHERE status IN ('pending', 'failed');

CREATE TABLE sop_task_submission_fanouts (
  submission_fanout_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  task_id uuid NOT NULL REFERENCES sop_tasks(task_id),
  submission_id uuid NOT NULL REFERENCES sop_submissions(submission_id),
  status text NOT NULL DEFAULT 'pending',
  requested_by uuid NULL,
  attempt_count int NOT NULL DEFAULT 0,
  last_error text NOT NULL DEFAULT '',
  completed_at timestamptz NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT sop_task_submission_fanouts_status_check CHECK (status IN ('pending', 'completed', 'failed', 'skipped')),
  CONSTRAINT sop_task_submission_fanouts_attempt_check CHECK (attempt_count >= 0),
  CONSTRAINT sop_task_submission_fanouts_unique UNIQUE (tenant_id, submission_id)
);

CREATE INDEX sop_task_submission_fanouts_retry_idx
  ON sop_task_submission_fanouts (tenant_id, status, updated_at, submission_fanout_id)
  WHERE status IN ('pending', 'failed');

-- +goose Down
DROP TABLE IF EXISTS sop_task_review_fanouts;
DROP TABLE IF EXISTS sop_task_submission_fanouts;

DROP INDEX IF EXISTS vaccination_completions_obligation_goat_active_unique_idx;

ALTER TABLE vaccination_completions
  ADD CONSTRAINT vaccination_completions_obligation_goat_unique
  UNIQUE (tenant_id, obligation_id, goat_id);
