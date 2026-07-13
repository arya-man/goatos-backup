-- +goose Up
-- +goose NO TRANSACTION
-- Tenant-leading claim index for ClaimDirtyScopes to avoid scan-and-filter on busy tenants.
-- The existing vaccination_projection_dirty_scopes_claim_idx remains for unscoped shared-worker paths.
-- CONCURRENTLY (+ NO TRANSACTION) so building it never takes a write-blocking lock on the live
-- dirty-scope queue -- enqueue and claim writes keep flowing during deploy (CON-002).
CREATE INDEX CONCURRENTLY IF NOT EXISTS vaccination_projection_dirty_scopes_tenant_claim_idx
  ON vaccination_projection_dirty_scopes (tenant_id, status, next_attempt_at, dirty_scope_id);

-- +goose Down
-- +goose NO TRANSACTION
DROP INDEX CONCURRENTLY IF EXISTS vaccination_projection_dirty_scopes_tenant_claim_idx;
