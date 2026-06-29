-- +goose Up

CREATE INDEX IF NOT EXISTS domain_event_processed_events_retention_idx
  ON domain_event_processed_events (processed_at, tenant_id, subscription_id, event_id)
  WHERE status = 'processed' AND processed_at IS NOT NULL;

-- +goose Down

DROP INDEX IF EXISTS domain_event_processed_events_retention_idx;
