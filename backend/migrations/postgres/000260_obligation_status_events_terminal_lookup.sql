-- +goose Up
-- +goose NO TRANSACTION
-- Speeds as-of vaccination/admin reads that reconstruct the latest terminal state
-- (missed/waived/deferred) per obligation. This is a read-path index only; it
-- does not mutate operational data.
CREATE INDEX CONCURRENTLY IF NOT EXISTS obligation_status_events_terminal_lookup_idx
  ON obligation_status_events (tenant_id, obligation_id, occurred_at DESC, obligation_event_id DESC)
  WHERE event_type IN ('missed', 'waived', 'deferred');

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS obligation_status_events_terminal_lookup_idx;
