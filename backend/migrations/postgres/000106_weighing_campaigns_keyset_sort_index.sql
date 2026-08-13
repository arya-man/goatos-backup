-- +goose Up
-- +goose NO TRANSACTION
-- Lock-safe: CREATE INDEX CONCURRENTLY runs outside a transaction and takes only
-- a shared lock (ACCESS SHARE) on the table. No queries waiting for this table's
-- write lock are blocked for the duration of the index build.
-- Supports the keyset pagination in listCampaigns (period_start_date ASC, created_at ASC, campaign_id ASC).
-- The index helps Postgres avoid a full sort when fetching the task list.
CREATE INDEX CONCURRENTLY IF NOT EXISTS weighing_campaigns_period_start_created_campaign_keyset_idx
  ON public.weighing_campaigns (period_start_date ASC, created_at ASC, campaign_id ASC);

-- +goose Down
-- +goose NO TRANSACTION
DROP INDEX CONCURRENTLY IF EXISTS public.weighing_campaigns_period_start_created_campaign_keyset_idx;
