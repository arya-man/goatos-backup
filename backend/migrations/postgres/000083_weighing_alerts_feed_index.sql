-- +goose Up
-- +goose NO TRANSACTION
--
-- Weighing alerts feed (WEIGHING-ALERTS-01).
--
-- Serves the per-recipient keyset read in
-- backend/internal/weighing/adapters/postgres/alerts.go (listAlertsSQL):
--
--   WHERE tenant_id = $1
--     AND context->>'member_id' = <caller's workforce_member_id>
--     AND context->>'message_key' LIKE 'weighing.%'
--     AND requested_at >= now() - 30 days
--   ORDER BY requested_at DESC, notification_request_id DESC
--
-- The leading (tenant_id, member_id) equality plus the requested_at DESC tail
-- gives the planner the exact keyset order, so paging the feed is an index walk
-- rather than a sort of one tenant's whole notification history.
--
-- PARTIAL on the weighing message_key prefix: this index exists ONLY for the
-- weighing module feed and must not silently bloat into a general
-- notification_requests index. `message_key` is the module discriminator every
-- weighing producer stamps -- the weighing lifecycle/submission consumers and
-- the weighing PROFILE of the shared verification consumer (messageKeyPrefix
-- "weighing"). Vaccination rows carry "vaccination.%" and are excluded from the
-- index entirely.
--
-- ISOLATION (maintainer ruling 2026-08-03): weighing is free-flow and fully
-- herd-isolated. This index is on notification_requests alone. It references no
-- goats, goat_identifiers, herd_animals, obligation, protocol, or vaccination
-- object, and creates no path by which the alerts read could acquire one.
--
-- LOCK SAFETY: CONCURRENTLY + NO TRANSACTION, so the build takes no
-- ACCESS EXCLUSIVE lock on notification_requests. That table is on the hot
-- notification dispatch path (the queue/lease indexes above it are read and
-- updated continuously by the dispatcher), so a blocking CREATE INDEX here
-- would stall delivery for every module, not just weighing.
CREATE INDEX CONCURRENTLY IF NOT EXISTS notification_requests_weighing_alerts_idx
ON public.notification_requests (
  tenant_id,
  (context->>'member_id'),
  requested_at DESC,
  notification_request_id DESC
)
WHERE context->>'message_key' LIKE 'weighing.%';

-- +goose Down
-- +goose NO TRANSACTION
DROP INDEX CONCURRENTLY IF EXISTS public.notification_requests_weighing_alerts_idx;
