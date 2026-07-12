-- +goose Up
-- Versioned, projector-owned summary grain for Action Center navigation counts,
-- Protocol Adherence KPIs, and Control Tower summary cards. Request paths read
-- these rows instead of grouping the high-cardinality serving row projection.
CREATE TABLE process_integrity_projection_summaries (
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  projection_version bigint NOT NULL,
  category text NOT NULL,
  park_id text NOT NULL DEFAULT '',
  shed_id text NOT NULL DEFAULT '',
  owner_id text NOT NULL DEFAULT '',
  protocol_version_id text NOT NULL DEFAULT '',
  due_business_date date NOT NULL,
  work_state text NOT NULL,
  severity text NOT NULL,
  process_intact boolean NOT NULL,
  row_count bigint NOT NULL,
  expected_count bigint NOT NULL,
  completed_count bigint NOT NULL,
  deferred_count bigint NOT NULL,
  projected_at timestamptz NOT NULL,
  PRIMARY KEY (
    tenant_id, projection_version, category, park_id, shed_id, owner_id,
    protocol_version_id, due_business_date, work_state, severity, process_intact
  ),
  CONSTRAINT process_integrity_projection_summary_category_check
    CHECK (category IN ('vaccination', 'feed_direction')),
  CONSTRAINT process_integrity_projection_summary_nonnegative_check
    CHECK (row_count >= 0 AND expected_count >= 0 AND completed_count >= 0 AND deferred_count >= 0)
);

COMMENT ON TABLE process_integrity_projection_summaries IS
  'Off-request preaggregation for process-integrity counts and KPI summaries. owner_id empty rows are the non-owner-filtered grain; owner-specific rows are emitted separately.';

CREATE INDEX process_integrity_projection_summaries_hot_idx
  ON process_integrity_projection_summaries (
    tenant_id, projection_version, category, due_business_date, work_state
  ) INCLUDE (row_count, expected_count, completed_count, deferred_count, severity, process_intact);

CREATE INDEX process_integrity_projection_summaries_scope_idx
  ON process_integrity_projection_summaries (
    tenant_id, projection_version, category, park_id, shed_id, due_business_date, work_state
  ) INCLUDE (row_count, expected_count, completed_count, deferred_count, severity, process_intact);

CREATE INDEX process_integrity_projection_summaries_owner_idx
  ON process_integrity_projection_summaries (
    tenant_id, projection_version, category, owner_id, due_business_date, work_state
  ) INCLUDE (row_count, expected_count, completed_count, deferred_count, severity, process_intact)
  WHERE owner_id <> '';

CREATE INDEX process_integrity_projection_summaries_protocol_idx
  ON process_integrity_projection_summaries (
    tenant_id, projection_version, category, protocol_version_id, due_business_date, work_state
  ) INCLUDE (row_count, expected_count, completed_count, deferred_count, severity, process_intact);

-- +goose Down
DROP TABLE IF EXISTS process_integrity_projection_summaries;
