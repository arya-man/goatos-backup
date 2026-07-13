-- +goose Up
-- Calendar completed-history + date-marker PROJECTION. Replaces the request-time
-- vaccination_completions/obligation_instances/protocol_* canonical joins
-- (calendarListSQL's completed_history CTE, calendarDateMarkersSQL's branch-2
-- aggregate, and calendarCompletedHistoryDetailSQL) with projector-owned,
-- tenant-scoped, indexed read tables. See docs/decisions/high-scale-dashboard-
-- projections.md (Serving-Read Freshness Contract) and
-- backend/internal/calendar/adapters/postgres/history_projection.go.

CREATE TABLE calendar_history_projection_rows (
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  event_id text NOT NULL,
  business_date date NOT NULL,
  park_id uuid NULL,
  park_code text NULL,
  shed_id uuid NULL,
  shed_name text NULL,
  protocol_id uuid NULL,
  protocol_version_id uuid NULL,
  rule_id uuid NULL,
  vaccine_name text NULL,
  dose_code text NULL,
  title text NOT NULL,
  subtitle text NOT NULL DEFAULT '',
  target_count int NOT NULL,
  window_start timestamptz NULL,
  window_end timestamptz NULL,
  detail jsonb NOT NULL DEFAULT '{}'::jsonb,
  projection_version bigint NOT NULL,
  projected_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, projection_version, event_id),
  CONSTRAINT calendar_history_projection_rows_target_count_check CHECK (target_count >= 0),
  CONSTRAINT calendar_history_projection_rows_detail_object_check CHECK (jsonb_typeof(detail) = 'object'),
  CONSTRAINT calendar_history_projection_rows_window_check CHECK (window_end IS NULL OR window_start IS NULL OR window_end >= window_start)
);

-- Hot list read: tenant + serving version + business-date window, ordered/keyed by event_id.
CREATE INDEX calendar_history_projection_rows_hot_list_idx
  ON calendar_history_projection_rows (tenant_id, projection_version, business_date, event_id);

-- Scoped read: tenant + serving version + park/shed + business-date window.
CREATE INDEX calendar_history_projection_rows_scope_idx
  ON calendar_history_projection_rows (tenant_id, projection_version, park_id, shed_id, business_date, event_id);

CREATE TABLE calendar_history_date_markers (
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  projection_version bigint NOT NULL,
  business_date date NOT NULL,
  park_key text NOT NULL,
  shed_key text NOT NULL,
  park_id uuid NULL,
  shed_id uuid NULL,
  completion_count bigint NOT NULL,
  projected_at timestamptz NOT NULL,
  PRIMARY KEY (tenant_id, projection_version, business_date, park_key, shed_key),
  CONSTRAINT calendar_history_date_markers_completion_count_check CHECK (completion_count >= 0)
);

CREATE INDEX calendar_history_date_markers_hot_idx
  ON calendar_history_date_markers (tenant_id, projection_version, business_date);

-- Mirrors vaccination_shed_projection_state's serving-pointer shape (migration
-- 000167) so this projection follows the same last-known-good / version-swap
-- contract as the sibling projections.
CREATE TABLE calendar_history_projection_state (
  tenant_id uuid PRIMARY KEY REFERENCES tenants(tenant_id),
  projection_version bigint NOT NULL,
  serving_projection_version bigint NULL,
  projected_at timestamptz NOT NULL,
  date_from timestamptz NOT NULL,
  date_to timestamptz NOT NULL,
  freshness_status text NOT NULL DEFAULT 'unknown',
  serving_state text NOT NULL DEFAULT 'never_synced',
  last_error text,
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT calendar_history_projection_state_freshness_check CHECK (freshness_status IN ('green', 'yellow', 'red', 'unknown')),
  CONSTRAINT calendar_history_projection_state_serving_check CHECK (serving_state IN ('never_synced', 'fresh', 'stale', 'rebuilding', 'failed')),
  CONSTRAINT calendar_history_projection_state_window_check CHECK (date_to >= date_from)
);

-- +goose Down
DROP TABLE IF EXISTS calendar_history_projection_state;
DROP TABLE IF EXISTS calendar_history_date_markers;
DROP TABLE IF EXISTS calendar_history_projection_rows;
