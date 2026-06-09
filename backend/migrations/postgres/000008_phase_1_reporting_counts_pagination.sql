-- +goose Up
CREATE INDEX goat_identity_counters_page_idx
  ON goat_identity_counters(counter_grain, tenant_id, count_value DESC, counter_id);

-- +goose Down
DROP INDEX IF EXISTS goat_identity_counters_page_idx;
