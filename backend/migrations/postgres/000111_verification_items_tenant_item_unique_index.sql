-- +goose Up
-- +goose NO TRANSACTION

-- verification_items is a hot table; build the supporting unique index CONCURRENTLY (no
-- ACCESS EXCLUSIVE table lock) before the next migration attaches a composite FK to it
-- (verification_review_events.item_id -> (tenant_id, item_id)). item_id is already globally
-- unique via the PK, so this index cannot fail on duplicate data -- it only exists to give
-- Postgres a (tenant_id, item_id) key to reference.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS verification_items_tenant_item_unique_idx
    ON public.verification_items USING btree (tenant_id, item_id);

-- +goose Down
-- +goose NO TRANSACTION
DROP INDEX CONCURRENTLY IF EXISTS public.verification_items_tenant_item_unique_idx;
