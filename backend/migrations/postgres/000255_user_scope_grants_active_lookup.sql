-- +goose Up
-- +goose NO TRANSACTION
-- Speed up per-request authorization without caching permission state.
--
-- ActiveTenantGrants filters by user_id + tenant_id + active window on every protected
-- admin/mobile API request. The older user_scope_grants_user_active_idx starts with user_id but
-- does not include tenant_id, so multi-tenant users can still scan unrelated active grants before
-- scope checks. Keep status in the partial predicate rather than the key: every protected request
-- asks only for live grants, and the remaining key order matches ORDER BY role, scope_type,
-- scope_id so the auth path does not pay an extra sort/table lookup before the business query.
-- This index is read-only serving infrastructure; it does not mutate grant data.
DROP INDEX CONCURRENTLY IF EXISTS public.user_scope_grants_active_lookup_idx;

CREATE INDEX CONCURRENTLY IF NOT EXISTS user_scope_grants_active_lookup_idx
  ON public.user_scope_grants (user_id, tenant_id, role, scope_type, scope_id)
  INCLUDE (valid_from, valid_to)
  WHERE status = 'active';

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS public.user_scope_grants_active_lookup_idx;
