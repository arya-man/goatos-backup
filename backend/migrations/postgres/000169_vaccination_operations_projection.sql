-- +goose Up
CREATE TABLE vaccination_operations_projection_rows (
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  projection_version bigint NOT NULL,
  park_id text NOT NULL, park_name text NOT NULL,
  shed_id text NOT NULL, shed_name text NOT NULL,
  stage text NOT NULL, age_band text,
  protocol_id text NOT NULL, protocol_name text NOT NULL,
  animals bigint NOT NULL, next_due timestamptz, last_dose timestamptz,
  overdue_count bigint NOT NULL, due_count bigint NOT NULL, in_progress_count bigint NOT NULL,
  scheduled_count bigint NOT NULL, missed_count bigint NOT NULL, deferred_count bigint NOT NULL,
  accepted_count bigint NOT NULL, proof_pending_count bigint NOT NULL, rejected_count bigint NOT NULL,
  total_count bigint NOT NULL, projected_at timestamptz NOT NULL,
  PRIMARY KEY (tenant_id,projection_version,park_id,shed_id,stage,protocol_id)
);
CREATE INDEX vaccination_operations_projection_page_idx
  ON vaccination_operations_projection_rows
  (tenant_id,projection_version,lower(park_name) COLLATE "C",park_name COLLATE "C",park_id,
   lower(shed_name) COLLATE "C",shed_name COLLATE "C",shed_id,stage COLLATE "C",protocol_name COLLATE "C",protocol_id);
CREATE INDEX vaccination_operations_projection_shed_idx
  ON vaccination_operations_projection_rows (tenant_id,projection_version,shed_id,stage,protocol_id);

CREATE TABLE vaccination_operations_projection_state (
  tenant_id uuid PRIMARY KEY REFERENCES tenants(tenant_id),
  projection_version bigint NOT NULL, serving_projection_version bigint,
  projected_at timestamptz NOT NULL, as_of timestamptz NOT NULL, due_before timestamptz NOT NULL,
  row_count bigint NOT NULL DEFAULT 0 CHECK (row_count>=0),
  freshness_status text NOT NULL DEFAULT 'unknown' CHECK (freshness_status IN ('green','yellow','red','unknown')),
  serving_state text NOT NULL DEFAULT 'never_synced' CHECK (serving_state IN ('never_synced','fresh','stale','rebuilding','failed')),
  last_error text, updated_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS vaccination_operations_projection_state;
DROP TABLE IF EXISTS vaccination_operations_projection_rows;
