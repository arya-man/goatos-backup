-- +goose Up
-- +goose NO TRANSACTION
-- Weighing leadership GALLERY bucket-page index.
--
-- The gallery had no bucket-grain read: the client fetched a page of tasks,
-- expanded every embedded bucket, and then called the single-shed evidence read
-- once per bucket -- roughly 1,500 sequential HTTP round trips on a 76-shed park,
-- on every screen resume. ListLeadershipSheds replaces that with ONE keyset page
-- of buckets, walked in the same task order the task list already uses:
--
--   FROM weighing_campaigns wc
--   JOIN weighing_campaign_sheds cs ON (cs.tenant_id, cs.campaign_id) = (wc.tenant_id, wc.campaign_id)
--   ORDER BY wc.period_start_date DESC, wc.created_at DESC, wc.campaign_id DESC, cs.campaign_shed_id DESC
--   LIMIT $6
--
-- The campaign side is already index-ordered by the task-list keyset. The inner
-- side needs the buckets of ONE campaign yielded in campaign_shed_id order so the
-- join can be walked and stopped by LIMIT. The committed bucket indexes do not
-- give that: weighing_campaign_sheds_detail_keyset_idx orders by display_name,
-- weighing_campaign_sheds_campaign_location_uidx by location_id,
-- weighing_campaign_sheds_operator_status_idx by operator, and
-- weighing_campaign_sheds_open_date_idx is a partial on the open-claim date.
-- Without this index the planner sorts each campaign's buckets on every page --
-- moving the over-fetch into the database instead of removing it.
--
-- Column order mirrors the predicate exactly: the two equality columns first, then
-- the ordering column. Postgres scans it backward for the DESC order.
--
-- Lock-safe: CREATE INDEX CONCURRENTLY runs outside a transaction and takes only
-- SHARE UPDATE EXCLUSIVE, so planner writes and operator submits are not blocked
-- while it builds. Additive only -- no column, constraint, or data change, and it
-- does not touch any already-applied migration.
CREATE INDEX CONCURRENTLY IF NOT EXISTS weighing_campaign_sheds_campaign_shed_keyset_idx
ON public.weighing_campaign_sheds (tenant_id, campaign_id, campaign_shed_id);

-- +goose Down
-- +goose NO TRANSACTION
DROP INDEX CONCURRENTLY IF EXISTS public.weighing_campaign_sheds_campaign_shed_keyset_idx;
