-- +goose Up
-- +goose NO TRANSACTION
--
-- THE last_published_at ARM OF GET /operations/kernel-health.
--
-- outbox/adapters/postgres/repository.go:Health used to aggregate every outbox row of the tenant
-- in one FILTERed pass: a parallel seq scan of ~362k rows / ~1 GB on goatos-stg, 0.9 s warm and
-- ~4 s cold, for a result made of four small status counts and three min/max stamps. The query
-- is now one scalar subquery per figure; the status counts and the pending / failure stamps are
-- answered by the existing tenant_status_attempt and replay_guard indexes (a few ms). The only
-- figure no index answered is MAX(published_at) over the published rows -- that is this index:
-- the planner's min/max rewrite reads one entry from its end.
--
-- The predicate matches the arm's constant filter character for character (status =
-- 'published'), so the partial index is provable for generic (prepared) plans too.
--
-- LOCK SAFETY: CONCURRENTLY + NO TRANSACTION. outbox_messages is written by every domain write
-- and by the relay; a blocking build would stall them.
CREATE INDEX CONCURRENTLY IF NOT EXISTS outbox_messages_tenant_published_at_idx
ON public.outbox_messages (tenant_id, published_at)
WHERE status = 'published';

-- +goose Down
-- +goose NO TRANSACTION
DROP INDEX CONCURRENTLY IF EXISTS public.outbox_messages_tenant_published_at_idx;
