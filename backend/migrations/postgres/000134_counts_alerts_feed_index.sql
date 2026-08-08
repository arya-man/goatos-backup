-- +goose Up
-- +goose NO TRANSACTION
--
-- Counts alerts feed (COUNTS-ALERTS-01).
--
-- Serves the per-recipient keyset read in
-- backend/internal/counts/adapters/postgres/alerts.go (listAlertsSQL):
--
--   WHERE tenant_id = $1
--     AND context->>'member_id' = <caller's workforce_member_id>
--     AND context->>'message_key' LIKE 'counts.%'
--     AND requested_at >= now() - 30 days
--   ORDER BY requested_at DESC, notification_request_id DESC
--
-- The leading (tenant_id, member_id) equality plus the requested_at DESC tail
-- gives the planner the exact keyset order, so paging the feed is an index walk
-- rather than a sort of one tenant's whole notification history.
--
-- PARTIAL on the counts message_key prefix: this index exists ONLY for the
-- counts module feed and must not silently bloat into a general
-- notification_requests index. `message_key` is the module discriminator every
-- counts producer stamps -- the shared verification consumer's COUNTS profile
-- (messageKeyPrefix "counts"). Weighing/vaccination/feed rows carry their own
-- prefixes and are excluded from this index entirely, mirroring 000083/000132/
-- 000133. Note: SHIFTING's own operator-facing notifications carry a SEPARATE
-- "shifting.%" message_key prefix and are intentionally NOT covered by this
-- index -- they are a different feed with a different audience shape.
--
-- LOCK SAFETY: CONCURRENTLY + NO TRANSACTION, so the build takes no
-- ACCESS EXCLUSIVE lock on notification_requests. That table is on the hot
-- notification dispatch path, so a blocking CREATE INDEX here would stall
-- delivery for every module, not just counts.
CREATE INDEX CONCURRENTLY IF NOT EXISTS notification_requests_counts_alerts_idx
ON public.notification_requests (
  tenant_id,
  (context->>'member_id'),
  requested_at DESC,
  notification_request_id DESC
)
WHERE context->>'message_key' LIKE 'counts.%';

-- +goose Down
-- +goose NO TRANSACTION
DROP INDEX CONCURRENTLY IF EXISTS public.notification_requests_counts_alerts_idx;
