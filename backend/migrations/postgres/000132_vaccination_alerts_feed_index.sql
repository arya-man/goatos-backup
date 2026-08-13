-- +goose Up
-- +goose NO TRANSACTION
--
-- Vaccination alerts feed index -- the sibling of
-- 000083_weighing_alerts_feed_index.sql, for the read added in
-- backend/internal/vaccinationexecution/adapters/postgres/alerts.go (listAlertsSQL):
--
--   WHERE tenant_id = $1
--     AND context->>'member_id' = <caller's workforce_member_id>
--     AND context->>'message_key' LIKE 'vaccination.%'
--     AND requested_at >= now() - 30 days
--   ORDER BY requested_at DESC, notification_request_id DESC
--
-- The leading (tenant_id, member_id) equality plus the requested_at DESC tail gives
-- the planner the exact keyset order, so paging the feed is an index walk rather
-- than a sort of one tenant's whole notification history. Without it the feed is
-- keyset-PAGINATED but not keyset-SERVED: correct today on a 41-row fixture, and a
-- growing sort of tenant notification history in production.
--
-- PARTIAL on the vaccination message_key prefix, exactly as the weighing index is
-- partial on its own: this index exists ONLY for the vaccination module feed and
-- must not bloat into a general notification_requests index. `message_key` is the
-- module discriminator every vaccination producer stamps
-- ("vaccination.record.closed", "vaccination.proof.rework",
-- "vaccination.proof.approved", "vaccination.proof.pending.verifier",
-- "vaccination.proof.pending.leadership"). Weighing rows carry "weighing.%" and are
-- excluded from this index entirely, just as vaccination rows are excluded from
-- theirs -- the two module feeds share the table and nothing else.
--
-- LOCK SAFETY: CONCURRENTLY + NO TRANSACTION, so the build takes no
-- ACCESS EXCLUSIVE lock on notification_requests. That table is on the hot
-- notification dispatch path (the queue/lease indexes are read and updated
-- continuously by the dispatcher), so a blocking CREATE INDEX here would stall
-- delivery for every module, not just vaccination.
CREATE INDEX CONCURRENTLY IF NOT EXISTS notification_requests_vaccination_alerts_idx
ON public.notification_requests (
  tenant_id,
  (context->>'member_id'),
  requested_at DESC,
  notification_request_id DESC
)
WHERE context->>'message_key' LIKE 'vaccination.%';

-- +goose Down
-- +goose NO TRANSACTION
DROP INDEX CONCURRENTLY IF EXISTS public.notification_requests_vaccination_alerts_idx;
