-- +goose Up
CREATE INDEX IF NOT EXISTS mortality_source_rows_current_observed_order_idx
  ON mortality_source_rows (tenant_id, source_observed_at, source_table, source_row_key, mortality_source_row_id)
  WHERE row_status = 'current';

CREATE INDEX IF NOT EXISTS mortality_projection_state_tenant_period_updated_idx
  ON mortality_projection_state (tenant_id, period, updated_at DESC);

-- +goose Down
DROP INDEX IF EXISTS mortality_projection_state_tenant_period_updated_idx;
DROP INDEX IF EXISTS mortality_source_rows_current_observed_order_idx;
