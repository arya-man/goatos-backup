-- +goose Up
-- +goose NO TRANSACTION
-- goats is a hot operational table, so build this support index outside the
-- transactional partition cutover with CONCURRENTLY.
CREATE INDEX CONCURRENTLY IF NOT EXISTS goats_shed_group_idx
  ON public.goats (tenant_id, shed_group_id)
  WHERE shed_group_id IS NOT NULL;

-- +goose Down
-- +goose NO TRANSACTION
DROP INDEX CONCURRENTLY IF EXISTS public.goats_shed_group_idx;
