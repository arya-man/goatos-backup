-- +goose Up
-- +goose NO TRANSACTION
--
-- PER-BUCKET OBSERVATION PROBES FOR THE WEIGHING READS.
--
-- Every weighing leadership read joins its scoped bucket set to weighing_observations on
-- (tenant_id, campaign_shed_id), usually with an accepted_at window: the weight demographics,
-- growth and shed-weights scoped CTEs, and the readyToCloseCountsSQL projection that the
-- /app/weighing/campaigns list runs a dozen correlated counts of per bucket. No index leads with
-- (tenant_id, campaign_shed_id): the shed keyset index puts campaign_id second, so each of those
-- nested-loop probes walked the tenant's whole keyset index instead of one bucket's rows. On the
-- STG clone the campaigns list read 76k buffers (128 ms) and weight demographics 55k; with this
-- index 20k (42 ms) and 29k. Results are unchanged -- an index only changes the access path.
--
-- LOCK SAFETY: CONCURRENTLY + NO TRANSACTION. weighing_observations takes every capture write;
-- a blocking build would stall weighing in the field.
CREATE INDEX CONCURRENTLY IF NOT EXISTS weighing_observations_shed_window_idx
ON public.weighing_observations (tenant_id, campaign_shed_id, accepted_at);

-- +goose Down
-- +goose NO TRANSACTION
DROP INDEX CONCURRENTLY IF EXISTS public.weighing_observations_shed_window_idx;
