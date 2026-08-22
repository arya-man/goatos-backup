-- +goose Up
-- +goose NO TRANSACTION

-- Supports the People/HRMS directory's per-operator proof statistics
-- (GET /admin/workforce/people): each directory row aggregates that member's
-- verification_items by status — proof uploads, approved, rejected — joined on
-- operator_id = workforce_members.user_id. This partial index on
-- operator_id IS NOT NULL carries status so the per-member LATERAL aggregate
-- reads an index-scoped slice of the hot table (one operator's items) instead
-- of scanning it, at any herd/proof volume.
-- seed-migration-guard:ignore owner=maintainer issue=people-hrms-rewrite reason=index-only-migration-on-runtime-proof-telemetry-no-seed-companion expiry=2026-11-30
CREATE INDEX CONCURRENTLY IF NOT EXISTS verification_items_operator_status_idx
    ON public.verification_items USING btree (tenant_id, operator_id, status)
    WHERE operator_id IS NOT NULL;

-- +goose Down
-- +goose NO TRANSACTION
DROP INDEX CONCURRENTLY IF EXISTS public.verification_items_operator_status_idx;
