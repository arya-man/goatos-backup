-- +goose Up
-- +goose NO TRANSACTION
-- seed-fixture-guard:ignore: operational Weighing planner tables only; no Vaccination HRMS seed contract change
--
-- A FINISHED SHED IS SCHEDULABLE AGAIN (maintainer decision, 2026-08-03).
--
-- This REVERSES the rule migration 000062 wrote into its own header:
--
--   "'completed' IS open for this purpose -- the shed was already weighed on
--    that date, so scheduling it a second time on the same date is the exact
--    duplicate work this index exists to stop."
--
-- That is not how the farm works. A shed whose weighing is DONE is finished
-- work, not an occupied slot: the CEO may schedule it again in the same week
-- and on the same date, exactly as they may schedule any shed that was never
-- scheduled at all. Deciding a shed is worth weighing twice is a planning call,
-- and the schema must not be the thing that refuses it. 000081 removed the
-- park-week block for the same reason; this removes its per-shed twin.
--
-- What the guard still means, and still enforces: ONE OPEN CLAIM per
-- (park, weigh date, shed). Two operators can never simultaneously owe the same
-- shed on the same date. OPEN now means work still owed -- 'canceled',
-- 'closed' and 'completed' are all finished history.
--
-- Both indexes are re-cut with the wider terminal set. There is no backfill and
-- no data change: relaxing a partial unique index's predicate can only ever
-- admit rows, never invalidate existing ones.
--
-- WEIGHING IS FREE-FLOW AND ISOLATED. Nothing here touches goats, rosters,
-- scanned identifiers, vaccination, or any obligation/rule engine.
--
-- Lock safety: CREATE/DROP INDEX CONCURRENTLY take only SHARE UPDATE EXCLUSIVE,
-- so operator capture and planner writes continue throughout. The new index is
-- built BEFORE the old one is dropped, so the slot is never unguarded. That is
-- why this file runs with NO TRANSACTION.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_weighing_open_shed_per_park_date_v2
  ON public.weighing_campaign_sheds (tenant_id, park_id, start_business_date, location_id)
  WHERE status NOT IN ('canceled', 'closed', 'completed');

CREATE INDEX CONCURRENTLY IF NOT EXISTS weighing_campaign_sheds_open_date_v2_idx
  ON public.weighing_campaign_sheds (tenant_id, start_business_date, location_id)
  INCLUDE (campaign_id, park_id, operator_user_id, weighing_category, status)
  WHERE status NOT IN ('canceled', 'closed', 'completed');

DROP INDEX CONCURRENTLY IF EXISTS public.uq_weighing_open_shed_per_park_date;
DROP INDEX CONCURRENTLY IF EXISTS public.weighing_campaign_sheds_open_date_idx;

-- +goose Down
-- +goose NO TRANSACTION
-- Restores 000062's narrower predicate. This CAN fail: once a shed has been
-- scheduled again on a date it already completed -- the behaviour this migration
-- exists to allow -- those rows violate the old index. That is expected, not a
-- bug in the Down.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_weighing_open_shed_per_park_date
  ON public.weighing_campaign_sheds (tenant_id, park_id, start_business_date, location_id)
  WHERE status NOT IN ('canceled', 'closed');

CREATE INDEX CONCURRENTLY IF NOT EXISTS weighing_campaign_sheds_open_date_idx
  ON public.weighing_campaign_sheds (tenant_id, start_business_date, location_id)
  INCLUDE (campaign_id, park_id, operator_user_id, weighing_category, status)
  WHERE status NOT IN ('canceled', 'closed');

DROP INDEX CONCURRENTLY IF EXISTS public.weighing_campaign_sheds_open_date_v2_idx;
DROP INDEX CONCURRENTLY IF EXISTS public.uq_weighing_open_shed_per_park_date_v2;
