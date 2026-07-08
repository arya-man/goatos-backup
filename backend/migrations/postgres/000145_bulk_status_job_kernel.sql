-- +goose Up
-- Durable bulk status-update kernel. Commit enqueues a job + one row per goat;
-- workers claim pending/retry rows with FOR UPDATE SKIP LOCKED and apply through
-- per-goat transition semantics. Resume truth is the row_state ledger (NOT a
-- positional cursor): recovery re-scans pending|retry.
CREATE TABLE bulk_status_job (
  bulk_status_job_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  actor_id uuid NULL,
  axis text NOT NULL,
  params jsonb NOT NULL DEFAULT '{}'::jsonb,
  total_rows int NOT NULL DEFAULT 0,
  applied_rows int NOT NULL DEFAULT 0,
  skipped_rows int NOT NULL DEFAULT 0,
  failed_rows int NOT NULL DEFAULT 0,
  state text NOT NULL DEFAULT 'pending',
  idempotency_key text NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT bulk_status_job_axis_check CHECK (axis IN ('reproductive', 'health', 'exit')),
  CONSTRAINT bulk_status_job_state_check CHECK (state IN ('pending', 'running', 'completed', 'failed', 'canceled'))
);
CREATE UNIQUE INDEX bulk_status_job_tenant_idem_idx
  ON bulk_status_job (tenant_id, idempotency_key)
  WHERE idempotency_key IS NOT NULL;

CREATE TABLE bulk_status_job_row (
  bulk_status_job_row_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  job_id uuid NOT NULL REFERENCES bulk_status_job (bulk_status_job_id) ON DELETE CASCADE,
  goat_id uuid NOT NULL,
  axis text NOT NULL,
  target text NOT NULL,
  reason text NULL,
  expected_row_version bigint NULL,
  row_state text NOT NULL DEFAULT 'pending',
  retry_count int NOT NULL DEFAULT 0,
  failure_reason text NULL,
  event_id uuid NULL,
  claimed_at timestamptz NULL,
  applied_at timestamptz NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT bulk_status_job_row_axis_check CHECK (axis IN ('reproductive', 'health', 'exit')),
  CONSTRAINT bulk_status_job_row_state_check CHECK (row_state IN ('pending', 'claimed', 'applied', 'skipped', 'error', 'retry')),
  -- One row per goat per axis per job: stable row-level idempotency anchor.
  CONSTRAINT bulk_status_job_row_unique UNIQUE (job_id, goat_id, axis)
);
-- Worker claim + tenant-wide job discovery path (SKIP LOCKED over pending|retry
-- plus stale-lease claimed, tenant+job scoped). 'claimed' is in the predicate so
-- ListJobIDsWithClaimableRows can rediscover crash-orphaned jobs (last rows stuck
-- 'claimed') on this index. Stays tight at 1M because applied/skipped/error rows
-- drop out of it.
CREATE INDEX bulk_status_job_row_claim_idx
  ON bulk_status_job_row (tenant_id, job_id)
  WHERE row_state IN ('pending', 'retry', 'claimed');
CREATE INDEX bulk_status_job_row_job_idx ON bulk_status_job_row (job_id);

-- +goose Down
DROP TABLE IF EXISTS bulk_status_job_row;
DROP TABLE IF EXISTS bulk_status_job;
