-- +goose Up
-- +goose NO TRANSACTION
-- Backs the notification dispatcher's backlog-age probe (OldestDueRequestedAt,
-- OldestDueRequestedAtSQL in internal/notification/adapters/postgres/repository.go)
-- with an early-stop optimization. The probe must return the GLOBALLY oldest
-- currently-due request's requested_at value (for alerting on backlog age), which
-- is semantically equivalent to finding the minimum requested_at among all rows
-- WHERE tenant_id = $1 AND status IN ('queued','failed') AND COALESCE(next_attempt_at,
-- requested_at) <= $2, but previous implementations used an aggregate MIN(...) that
-- visited every matching row -- O(due-count), unavoidable at scale when the alert
-- matters. The optimization: rewrite to ORDER BY requested_at ASC LIMIT 1. Since
-- rows are ordered by requested_at in this index, Postgres can stop at the first
-- row that passes the COALESCE filter (early-stop), avoiding a full scan. The two
-- shapes are semantically identical: LIMIT 1 ORDER BY requested_at returns the
-- same value as MIN(requested_at) after filtering, but terminates early instead
-- of visiting all due rows. This index is lock-safe: build CONCURRENTLY, out of
-- transaction, with statement timeout for a hot table.
SET statement_timeout = '30min';
CREATE INDEX CONCURRENTLY IF NOT EXISTS notification_requests_oldest_due_requested_at_idx
  ON notification_requests (tenant_id, requested_at)
  INCLUDE (next_attempt_at)
  WHERE status IN ('queued', 'failed');
RESET statement_timeout;

-- +goose Down
-- +goose NO TRANSACTION
DROP INDEX CONCURRENTLY IF EXISTS notification_requests_oldest_due_requested_at_idx;
