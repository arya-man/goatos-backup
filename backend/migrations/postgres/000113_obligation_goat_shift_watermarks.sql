-- +goose Up
CREATE TABLE obligation_goat_shift_watermarks (
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  goat_id uuid NOT NULL REFERENCES goats(goat_id),
  last_occurred_at timestamptz NOT NULL,
  last_event_id text NOT NULL,
  last_scope_type text NOT NULL,
  last_scope_id uuid NOT NULL REFERENCES locations(location_id),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, goat_id)
);

CREATE INDEX obligation_goat_shift_watermarks_timeline_idx
  ON obligation_goat_shift_watermarks(tenant_id, last_occurred_at DESC);

-- +goose Down
DROP INDEX IF EXISTS obligation_goat_shift_watermarks_timeline_idx;
DROP TABLE IF EXISTS obligation_goat_shift_watermarks;
