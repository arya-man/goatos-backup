-- +goose Up

CREATE TABLE IF NOT EXISTS domain_event_processed_events (
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  subscription_id text NOT NULL,
  event_id text NOT NULL,
  event_type text NOT NULL,
  message_id text NOT NULL DEFAULT '',
  delivery_attempt int NOT NULL DEFAULT 0,
  status text NOT NULL DEFAULT 'processing',
  attempt_count int NOT NULL DEFAULT 1,
  started_at timestamptz NOT NULL DEFAULT now(),
  processed_at timestamptz NULL,
  last_error text NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, subscription_id, event_id),
  CONSTRAINT domain_event_processed_events_status_check CHECK (status IN ('processing', 'processed', 'failed')),
  CONSTRAINT domain_event_processed_events_attempt_check CHECK (attempt_count >= 1),
  CONSTRAINT domain_event_processed_events_delivery_attempt_check CHECK (delivery_attempt >= 0)
);

CREATE INDEX IF NOT EXISTS domain_event_processed_events_status_idx
  ON domain_event_processed_events (tenant_id, subscription_id, status, updated_at);

-- +goose Down

DROP INDEX IF EXISTS domain_event_processed_events_status_idx;
DROP TABLE IF EXISTS domain_event_processed_events;
