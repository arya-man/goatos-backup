-- +goose Up
-- +goose NO TRANSACTION
-- Backs the notification dispatcher's hot-path claim (ClaimDue, ClaimDueSQL in
-- internal/notification/adapters/postgres/repository.go) and the backlog-age
-- probe (OldestDueRequestedAt). Both select the DUE, undelivered rows with
--   WHERE tenant_id = $1 AND status IN ('queued', 'failed')
--     AND COALESCE(next_attempt_at, requested_at) <= $2
--   ORDER BY COALESCE(next_attempt_at, requested_at), notification_request_id
-- The existing notification_requests_queue_idx leads with (tenant_id, status,
-- COALESCE(...)), so with status spanning TWO values it cannot range-scan the
-- COALESCE order across both statuses — under heavy future-retry skew the planner
-- naturally falls back to a Seq Scan + Sort of the whole queued/failed partition
-- (proven by TestNotificationRepositoryClaimDuePlanSkipsFutureRetriesUnderSkew).
-- This index leads with (tenant_id, COALESCE(...)) and pushes status into the
-- PARTIAL predicate, so the same predicate becomes a bounded index range: only
-- the due rows are scanned, future-scheduled retries are skipped, and the LIMIT
-- stops early. Predicate is kept IDENTICAL to the query's status set. notification_requests
-- is a hot table (validate-hot-index-migrations.sh), so build CONCURRENTLY, out
-- of a transaction, with a statement timeout so a stuck build cannot hang forever.
SET statement_timeout = '30min';
CREATE INDEX CONCURRENTLY IF NOT EXISTS notification_requests_due_order_idx
  ON notification_requests (tenant_id, COALESCE(next_attempt_at, requested_at), notification_request_id)
  WHERE status IN ('queued', 'failed');
RESET statement_timeout;

-- +goose Down
-- +goose NO TRANSACTION
DROP INDEX CONCURRENTLY IF EXISTS notification_requests_due_order_idx;
