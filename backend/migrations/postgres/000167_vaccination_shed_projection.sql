-- +goose Up
-- Durable shed-wise vaccination read model (C35-002, vaccinationexecution half). The
-- request path (ShedSummary / GET /vaccination/sheds) still reads the live compute-on-read
-- CTE in this migration (see repository.go's shedSummarySQL, baselined in
-- tools/scale-guard/baseline.txt as owner=vaccination-platform issue=C35-002) — this table is
-- landed alongside it, off the request path, so the projector can start keeping it fresh
-- before the read is flipped over in a later change. Table shape mirrors
-- process_integrity_projection_rows / process_integrity_projection_state (migrations 000159 +
-- 000160), including the version-swap columns from day one since this table starts empty (no
-- CONCURRENTLY / NO TRANSACTION needed -- there is no pre-existing data or query traffic to
-- lock against).
CREATE TABLE vaccination_shed_projection_rows (
  vaccination_shed_projection_row_id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  park_id text NOT NULL,
  park_name text NOT NULL,
  shed_id text NOT NULL,
  shed_name text NOT NULL,
  animals integer NOT NULL DEFAULT 0,
  due_animals integer NOT NULL DEFAULT 0,
  open_cells integer NOT NULL DEFAULT 0,
  sessions integer NOT NULL DEFAULT 0,
  capacity_status text NOT NULL,
  shed_status text NOT NULL,
  last_done timestamptz,
  next_due timestamptz,
  projection_version bigint NOT NULL,
  projected_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT vaccination_shed_projection_capacity_status_check CHECK (capacity_status IN (
    'within_cap', 'over_cap', 'capacity_breach'
  )),
  CONSTRAINT vaccination_shed_projection_shed_status_check CHECK (shed_status IN (
    'overdue', 'needs_review', 'split', 'due', 'scheduled', 'on_track'
  )),
  CONSTRAINT vaccination_shed_projection_nonnegative_check CHECK (
    animals >= 0 AND due_animals >= 0 AND due_animals <= animals
    AND open_cells >= 0 AND sessions >= 0
  )
);

COMMENT ON TABLE vaccination_shed_projection_rows IS
  'Read model. Precomputed shed-wise vaccination rollup rows for GET /vaccination/sheds. Canonical source tables (obligation_instances, goats, vaccination_completions, vaccination_capacity_config) remain truth; RecomputeShedProjection is the only writer. Not yet the serving path for ShedSummary -- see C35-002 follow-up.';

-- Serving identity: one row per (tenant, projection_version, shed). A recompute inserts a whole
-- new projection_version; the OLD version's rows keep serving reads until the projector flips
-- vaccination_shed_projection_state.serving_projection_version, then the old version is pruned in
-- batches (see RecomputeShedProjection / pruneOldShedProjectionRows) -- never a live
-- DELETE-then-reinsert of the serving version.
CREATE UNIQUE INDEX vaccination_shed_projection_rows_version_row_uidx
  ON vaccination_shed_projection_rows (tenant_id, projection_version, shed_id);

CREATE INDEX vaccination_shed_projection_rows_hot_idx
  ON vaccination_shed_projection_rows (tenant_id, projection_version, park_name, shed_name);

CREATE INDEX vaccination_shed_projection_rows_status_idx
  ON vaccination_shed_projection_rows (tenant_id, projection_version, shed_status, park_name, shed_name);

CREATE INDEX vaccination_shed_projection_rows_capacity_idx
  ON vaccination_shed_projection_rows (tenant_id, projection_version, capacity_status, park_name, shed_name);

CREATE INDEX vaccination_shed_projection_rows_due_idx
  ON vaccination_shed_projection_rows (tenant_id, projection_version, due_animals DESC, park_name, shed_name);

CREATE INDEX vaccination_shed_projection_rows_next_due_idx
  ON vaccination_shed_projection_rows (tenant_id, projection_version, next_due, park_name, shed_name);

CREATE INDEX vaccination_shed_projection_rows_park_shed_idx
  ON vaccination_shed_projection_rows (tenant_id, projection_version, park_id, shed_id);

CREATE TABLE vaccination_shed_projection_state (
  tenant_id uuid PRIMARY KEY REFERENCES tenants(tenant_id),
  projection_version bigint NOT NULL,
  serving_projection_version bigint,
  projected_at timestamptz NOT NULL,
  as_of timestamptz NOT NULL,
  row_count bigint NOT NULL DEFAULT 0,
  freshness_status text NOT NULL DEFAULT 'unknown',
  serving_state text NOT NULL DEFAULT 'never_synced',
  last_error text,
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT vaccination_shed_projection_state_freshness_check CHECK (freshness_status IN ('green', 'yellow', 'red', 'unknown')),
  CONSTRAINT vaccination_shed_projection_state_serving_check CHECK (serving_state IN ('never_synced', 'fresh', 'stale', 'rebuilding', 'failed')),
  CONSTRAINT vaccination_shed_projection_state_row_count_check CHECK (row_count >= 0)
);

COMMENT ON COLUMN vaccination_shed_projection_state.serving_projection_version IS
  'Projection version that would be served once ShedSummary is flipped to read this table. RecomputeShedProjection builds a new projection_version, then atomically flips this pointer in the same transaction as the row insert.';

CREATE INDEX vaccination_shed_projection_state_updated_idx
  ON vaccination_shed_projection_state (updated_at DESC);

-- +goose Down
DROP TABLE IF EXISTS vaccination_shed_projection_state;
DROP TABLE IF EXISTS vaccination_shed_projection_rows;
