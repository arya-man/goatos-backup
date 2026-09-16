-- +goose NO TRANSACTION
-- +goose Up
-- Daily rollups and bounded trailing-seven-day activity use event time. Keep
-- this narrow (no properties INCLUDE) and concurrent on the ingest table.
CREATE INDEX CONCURRENTLY IF NOT EXISTS app_events_tenant_event_time_idx
ON analytics.app_events (tenant_id, (COALESCE(client_event_time, received_at)));

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS analytics.app_events_tenant_event_time_idx;
