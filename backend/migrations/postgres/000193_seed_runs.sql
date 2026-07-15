-- +goose Up
-- Persisted seed-run state machine (VACC-REV-02). A source-backed seed run carries an explicit
-- durable lifecycle state so a half-seeded database can NEVER appear healthy or promotable:
--   loading    -> source rows being inserted (pre-commit)
--   generating -> source committed, kernel generation running
--   verified   -> post-generation invariant verification passed (this is the READY/promotable state)
--   failed     -> any pre- or post-commit failure (this is the RESET_REQUIRED state; seed-closeout,
--                 CI, and deployment promotion MUST reject a database whose latest run is `failed`
--                 or that has no `verified` run at all)
-- The row is written on a connection OUTSIDE the seed data transaction on purpose: a data-tx
-- rollback (e.g. an in-transaction source-fact drop detected before commit) still leaves a durable
-- `failed` marker, and a clean-rollback database simply has no `verified` run — itself unpromotable.
CREATE TABLE seed_runs (
  seed_run_id uuid PRIMARY KEY,
  tenant_id   uuid NOT NULL,
  command     text NOT NULL,
  state       text NOT NULL,
  detail      jsonb NOT NULL DEFAULT '{}'::jsonb,
  error       text NOT NULL DEFAULT '',
  started_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  finished_at timestamptz,
  CONSTRAINT seed_runs_state_check CHECK (state IN ('loading', 'generating', 'verified', 'failed'))
);

CREATE INDEX seed_runs_tenant_state_idx ON seed_runs (tenant_id, updated_at DESC);

-- +goose Down
DROP TABLE seed_runs;
