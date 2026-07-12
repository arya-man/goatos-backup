-- +goose Up
-- +goose NO TRANSACTION
-- Backs the device-deregister suppress step (internal/workforce/adapters/postgres/repository.go
-- DeregisterDevice): closes the offline-logout / shared-phone delivery window by suppressing a
-- device's still-pending notification_requests rows, matched by the exact raw FCM token snapshotted
-- into recipient_ref at queue time (calendar's QueueRoleNotifications --
-- docs/decisions/vaccination-notification-rules.md §4c). Without this index the suppress UPDATE's
-- WHERE tenant_id = $1 AND recipient_ref = $2 AND status IN ('queued', 'failed') predicate falls back
-- to notification_requests_queue_idx (tenant_id, status, ...), which does not carry recipient_ref and
-- forces a filter scan of every queued/failed row for the tenant. notification_requests is a hot table
-- (validate-hot-index-migrations.sh), so this is built CONCURRENTLY to avoid blocking writes/reads.
CREATE INDEX CONCURRENTLY IF NOT EXISTS notification_requests_recipient_ref_pending_idx
  ON notification_requests (tenant_id, recipient_ref, status)
  WHERE recipient_ref IS NOT NULL AND status IN ('queued', 'failed');

-- +goose Down
-- +goose NO TRANSACTION
DROP INDEX CONCURRENTLY IF EXISTS notification_requests_recipient_ref_pending_idx;
