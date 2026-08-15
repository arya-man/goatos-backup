-- +goose NO TRANSACTION
-- +goose Up
-- Retry-safe analytics ingestion: a client resending the same event (response lost mid-flight)
-- must not double-count. client_event_id is the client-minted operation id; the partial unique
-- index makes the handler's ON CONFLICT DO NOTHING a true idempotency gate.
-- CONCURRENTLY: analytics.app_events is one of the largest, hottest-write tables in the DB —
-- a plain index build takes a SHARE lock and blocks every insert for the build duration
-- (same reasoning as 000154 / 000158).
ALTER TABLE analytics.app_events ADD COLUMN IF NOT EXISTS client_event_id text;

CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS app_events_tenant_client_event_id_idx
  ON analytics.app_events (tenant_id, client_event_id)
  WHERE client_event_id IS NOT NULL;

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS analytics.app_events_tenant_client_event_id_idx;
ALTER TABLE analytics.app_events DROP COLUMN IF EXISTS client_event_id;
