-- +goose Up
CREATE INDEX IF NOT EXISTS counts_source_rows_current_watermark_order_idx
  ON counts_source_rows (tenant_id, source_watermark_date, source_id, source_row_key, counts_source_row_id)
  WHERE row_status = 'current';

CREATE INDEX IF NOT EXISTS counts_projection_state_tenant_view_updated_idx
  ON counts_projection_state (tenant_id, view_id, updated_at DESC);

-- +goose Down
DROP INDEX IF EXISTS counts_projection_state_tenant_view_updated_idx;
DROP INDEX IF EXISTS counts_source_rows_current_watermark_order_idx;
