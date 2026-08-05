-- no-mismatch-review-queue:ignore: owner=ravi issue=maintainer-decision-2026-08-06 scope=verifier-watch-telemetry-not-a-reconciliation-queue expiry=2026-11-30
-- seed-migration-guard:ignore owner=ravi issue=maintainer-decision-2026-08-06 reason=append-only-telemetry-accrues-at-runtime-no-seed-companion expiry=2026-11-30
-- +goose Up
-- +goose NO TRANSACTION

-- Supports ceo_ai.verifier_review_integrity (migration 000114): the base scan is every DECIDED
-- item (verified_by IS NOT NULL, status IN ('approved','rejected')), grouped by
-- (tenant_id, verified_by, park_id, category, business_day derived from verified_at). This
-- partial index on verified_by IS NOT NULL matches that predicate exactly and carries park_id/
-- category/verified_at as the trailing columns the view groups and buckets by, so the CEO
-- aggregate reads an index-scoped slice of verification_items rather than the whole hot table.
CREATE INDEX CONCURRENTLY IF NOT EXISTS verification_items_verified_by_review_idx
    ON public.verification_items USING btree (tenant_id, verified_by, park_id, category, verified_at)
    WHERE verified_by IS NOT NULL;

-- +goose Down
-- +goose NO TRANSACTION
DROP INDEX CONCURRENTLY IF EXISTS public.verification_items_verified_by_review_idx;
