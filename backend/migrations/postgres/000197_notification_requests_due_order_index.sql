-- +goose Up
-- +goose NO TRANSACTION
-- Backs the notification dispatcher's hot-path claim (ClaimDue, ClaimDueSQL in
-- internal/notification/adapters/postgres/repository.go) and the backlog-age
-- probe (OldestDueRequestedAt, OldestDueRequestedAtSQL). Both share the same
-- DUE-row predicate:
--   WHERE tenant_id = $1 AND status IN ('queued', 'failed')
--     AND COALESCE(next_attempt_at, requested_at) <= $2
-- ClaimDue additionally does ORDER BY COALESCE(next_attempt_at, requested_at),
-- notification_request_id LIMIT $3 (claim priority order); the backlog-age
-- probe instead computes MIN(requested_at) over that same predicate -- it must
-- NOT reuse ClaimDue's ORDER BY/LIMIT shape, because the claim's priority
-- order (COALESCE(next_attempt_at, requested_at)) is not the same as a
-- request's original age (requested_at): under retry skew a batch of newer
-- requests can sort ahead of an hour-old failed request whose retry only just
-- became due, and an ORDER BY COALESCE(...) LIMIT 1 probe would report that
-- straggler's age as ~0 instead of ~1h (see
-- TestNotificationRepositoryOldestDueRequestedAtUnderSaturation and the doc
-- comment on OldestDueRequestedAtSQL). Both queries still benefit from THIS
-- index the same way: the existing notification_requests_queue_idx leads with
-- (tenant_id, status, COALESCE(...)), so with status spanning TWO values it
-- cannot range-scan the COALESCE order across both statuses — under heavy
-- future-retry skew the planner naturally falls back to a Seq Scan + Sort (for
-- ClaimDue, proven by TestNotificationRepositoryClaimDuePlanSkipsFutureRetriesUnderSkew)
-- or a Seq Scan + Aggregate (for the probe, proven by
-- TestNotificationRepositoryOldestDueRequestedAtPlanStaysIndexBoundedUnderSaturation)
-- of the whole queued/failed partition. This index leads with (tenant_id,
-- COALESCE(...)) and pushes status into the PARTIAL predicate, so the shared
-- predicate becomes a bounded index range: only the due rows are scanned,
-- future-scheduled retries are skipped. ClaimDue's LIMIT then stops at the
-- first rows in that range; the probe's MIN(...) still visits every row in
-- the range (bounded to the due-row count, not the full partition or table --
-- an accepted, documented O(due-count) cost, not O(1)). Predicate is kept
-- IDENTICAL to both queries' status set. notification_requests is a hot table
-- (validate-hot-index-migrations.sh), so build CONCURRENTLY, out of a
-- transaction, with a statement timeout so a stuck build cannot hang forever.
SET statement_timeout = '30min';
CREATE INDEX CONCURRENTLY IF NOT EXISTS notification_requests_due_order_idx
  ON notification_requests (tenant_id, COALESCE(next_attempt_at, requested_at), notification_request_id)
  WHERE status IN ('queued', 'failed');
RESET statement_timeout;

-- +goose Down
-- +goose NO TRANSACTION
DROP INDEX CONCURRENTLY IF EXISTS notification_requests_due_order_idx;
