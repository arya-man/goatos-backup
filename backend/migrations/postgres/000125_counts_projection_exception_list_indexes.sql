-- +goose Up
-- Feed Direction G2: support bounded operator queues for Counts/Shifting
-- projection exceptions. These indexes keep exception review on tenant/status
-- and location-scoped keyset reads instead of broad scans.

CREATE INDEX count_projection_exceptions_status_list_idx
  ON count_projection_exceptions (
    tenant_id,
    status,
    updated_at DESC,
    count_projection_exception_id DESC
  );

CREATE INDEX count_projection_exceptions_location_list_idx
  ON count_projection_exceptions (
    tenant_id,
    status,
    park_id,
    shed_id,
    updated_at DESC,
    count_projection_exception_id DESC
  );

-- +goose Down
DROP INDEX IF EXISTS count_projection_exceptions_location_list_idx;
DROP INDEX IF EXISTS count_projection_exceptions_status_list_idx;
