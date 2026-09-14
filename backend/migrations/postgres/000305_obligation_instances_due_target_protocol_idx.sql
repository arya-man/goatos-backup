-- +goose Up
-- +goose NO TRANSACTION
-- Work Board vaccination uses a cheap park-day existence precheck before invoking
-- the heavier process-integrity canonical read. Keep that precheck on the due
-- window instead of looping from every goat in a park into obligation_instances.

CREATE INDEX CONCURRENTLY IF NOT EXISTS obligation_instances_tenant_due_target_protocol_idx
  ON public.obligation_instances (tenant_id, due_at, target_id, protocol_version_id)
  WHERE target_type = 'goat';

-- +goose Down
-- +goose NO TRANSACTION
DROP INDEX CONCURRENTLY IF EXISTS public.obligation_instances_tenant_due_target_protocol_idx;
