-- +goose Up
-- Tenant-leading claim index for ClaimDirtyScopes to avoid scan-and-filter on busy tenants.
-- The existing vaccination_projection_dirty_scopes_claim_idx remains for unscoped shared-worker paths.
CREATE INDEX vaccination_projection_dirty_scopes_tenant_claim_idx
  ON vaccination_projection_dirty_scopes (tenant_id, status, next_attempt_at, dirty_scope_id);

-- +goose Down
DROP INDEX IF EXISTS vaccination_projection_dirty_scopes_tenant_claim_idx;
