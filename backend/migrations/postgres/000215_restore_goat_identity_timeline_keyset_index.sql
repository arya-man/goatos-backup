-- +goose Up
-- +goose NO TRANSACTION
-- Restore the tenant-leading ordered goat timeline index that existed on the partitioned history
-- tables before 000212 departed goat_identity_events. ListGoatTimeline filters tenant_id/goat_id and
-- orders by (occurred_at DESC, identity_event_id DESC); the weaker post-departition index can require
-- extra filtering/sorting on hot goat timelines.
SET statement_timeout = '30min';
CREATE INDEX CONCURRENTLY IF NOT EXISTS goat_identity_events_tenant_goat_timeline_keyset_idx
  ON goat_identity_events (tenant_id, goat_id, occurred_at DESC, identity_event_id DESC);
RESET statement_timeout;

-- +goose Down
-- +goose NO TRANSACTION
DROP INDEX CONCURRENTLY IF EXISTS goat_identity_events_tenant_goat_timeline_keyset_idx;
