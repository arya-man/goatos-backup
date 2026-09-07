-- +goose Up
-- +goose NO TRANSACTION
-- The pen-visit materializer's read (000274): every verification item raised on one business
-- date, by park. Existing verification_items indexes lead with status/category or the source
-- ref; none serves a created_at day window per tenant. verification_items is a populated hot
-- table on STG, so the index is built CONCURRENTLY outside a transaction and never takes the
-- SHARE lock a plain CREATE INDEX would hold against the verifier's writes.
CREATE INDEX CONCURRENTLY IF NOT EXISTS verification_items_created_pen_idx
    ON public.verification_items (tenant_id, created_at)
    WHERE shed_id IS NOT NULL;

-- +goose Down
-- +goose NO TRANSACTION
DROP INDEX CONCURRENTLY IF EXISTS public.verification_items_created_pen_idx;
