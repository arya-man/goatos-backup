-- +goose Up
-- Calendar projector run state. Canonical accepted history remains outside the
-- hot event projection, but every list response exposes the freshness/version
-- of the operational Calendar generation.
CREATE TABLE calendar_projection_state (
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  slice_key text NOT NULL,
  projection_version bigint NOT NULL,
  projected_at timestamptz NOT NULL,
  date_from timestamptz NOT NULL,
  date_to timestamptz NOT NULL,
  freshness_status text NOT NULL DEFAULT 'unknown',
  serving_state text NOT NULL DEFAULT 'never_synced',
  last_error text,
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, slice_key),
  CONSTRAINT calendar_projection_state_slice_check CHECK (slice_key = 'vaccination'),
  CONSTRAINT calendar_projection_state_freshness_check CHECK (freshness_status IN ('green', 'yellow', 'red', 'unknown')),
  CONSTRAINT calendar_projection_state_serving_check CHECK (serving_state IN ('never_synced', 'fresh', 'stale', 'rebuilding', 'failed')),
  CONSTRAINT calendar_projection_state_window_check CHECK (date_to >= date_from)
);

CREATE INDEX calendar_projection_state_freshness_idx
  ON calendar_projection_state (tenant_id, slice_key, serving_state, projected_at DESC);

-- +goose Down
DROP TABLE IF EXISTS calendar_projection_state;
