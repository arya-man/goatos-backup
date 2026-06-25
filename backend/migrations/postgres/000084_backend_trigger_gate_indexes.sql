-- +goose Up
-- Backend trigger-gate hot paths: operations audit list/summary and relay tenant/status scans.

CREATE INDEX IF NOT EXISTS audit_log_tenant_recorded_idx
  ON audit_log (tenant_id, recorded_at DESC, audit_id DESC);

CREATE INDEX IF NOT EXISTS audit_log_tenant_resource_recorded_idx
  ON audit_log (tenant_id, resource_type, resource_id, recorded_at DESC);

CREATE INDEX IF NOT EXISTS audit_log_tenant_actor_recorded_idx
  ON audit_log (tenant_id, actor_id, recorded_at DESC);

CREATE INDEX IF NOT EXISTS audit_log_tenant_scope_recorded_idx
  ON audit_log (tenant_id, scope_type, scope_id, recorded_at DESC);

CREATE INDEX IF NOT EXISTS audit_log_tenant_actor_type_recorded_idx
  ON audit_log (tenant_id, actor_type, recorded_at DESC, audit_id DESC);

CREATE INDEX IF NOT EXISTS audit_log_tenant_domain_module_category_recorded_idx
  ON audit_log (tenant_id, (metadata->>'domain'), (metadata->>'module'), (metadata->>'category'), recorded_at DESC, audit_id DESC);

CREATE INDEX IF NOT EXISTS audit_log_tenant_status_recorded_idx
  ON audit_log (tenant_id, (metadata->>'status'), recorded_at DESC, audit_id DESC);

CREATE INDEX IF NOT EXISTS audit_log_tenant_result_recorded_idx
  ON audit_log (tenant_id, (metadata->>'result'), recorded_at DESC, audit_id DESC);

CREATE INDEX IF NOT EXISTS outbox_messages_tenant_status_attempt_idx
  ON outbox_messages (tenant_id, status, next_attempt_at, created_at, outbox_id);

-- +goose Down
DROP INDEX IF EXISTS outbox_messages_tenant_status_attempt_idx;
DROP INDEX IF EXISTS audit_log_tenant_result_recorded_idx;
DROP INDEX IF EXISTS audit_log_tenant_status_recorded_idx;
DROP INDEX IF EXISTS audit_log_tenant_domain_module_category_recorded_idx;
DROP INDEX IF EXISTS audit_log_tenant_actor_type_recorded_idx;
DROP INDEX IF EXISTS audit_log_tenant_scope_recorded_idx;
DROP INDEX IF EXISTS audit_log_tenant_actor_recorded_idx;
DROP INDEX IF EXISTS audit_log_tenant_resource_recorded_idx;
DROP INDEX IF EXISTS audit_log_tenant_recorded_idx;
