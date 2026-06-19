-- +goose Up
CREATE INDEX IF NOT EXISTS locations_tenant_type_status_order_idx
  ON locations (tenant_id, location_type, status, display_order, name, location_id);

CREATE INDEX IF NOT EXISTS locations_tenant_status_order_idx
  ON locations (tenant_id, status, location_type, display_order, name, location_id);

CREATE INDEX IF NOT EXISTS locations_tenant_parent_order_idx
  ON locations (tenant_id, parent_location_id, location_type, display_order, name, location_id);

CREATE INDEX IF NOT EXISTS location_aliases_canonical_order_idx
  ON location_aliases (tenant_id, canonical_location_id, status, source_context, alias_code, alias_id);

-- +goose Down
DROP INDEX IF EXISTS location_aliases_canonical_order_idx;
DROP INDEX IF EXISTS locations_tenant_parent_order_idx;
DROP INDEX IF EXISTS locations_tenant_status_order_idx;
DROP INDEX IF EXISTS locations_tenant_type_status_order_idx;
