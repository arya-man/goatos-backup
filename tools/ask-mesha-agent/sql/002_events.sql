-- Ask Mesha per-user lifecycle events (tools/ask-mesha-agent/events.mjs).
-- Idempotent: applied on start with ASK_MESHA_DB_MIGRATE=1 (after 001_init.sql).
-- One row per event; `row` holds the full structured log line.
CREATE SCHEMA IF NOT EXISTS ask_mesha;

CREATE TABLE IF NOT EXISTS ask_mesha.events (
  id          bigserial   PRIMARY KEY,
  ts          timestamptz NOT NULL DEFAULT now(),
  event_name  text        NOT NULL,   -- ask_started | ask_first_token | ask_tool | ask_completed | ask_failed | ask_stopped | budget_warning | budget_blocked | auth_denied | attachment_saved
  severity    text        NOT NULL DEFAULT 'INFO',
  request_id  text,
  chat_id     text,                   -- text: budget/auth events may carry a non-uuid or no chat
  email       text,
  tenant_id   text,
  row         jsonb       NOT NULL
);
CREATE INDEX IF NOT EXISTS events_ts_idx          ON ask_mesha.events (ts DESC);
CREATE INDEX IF NOT EXISTS events_email_ts_idx    ON ask_mesha.events (email, ts DESC);
CREATE INDEX IF NOT EXISTS events_name_ts_idx     ON ask_mesha.events (event_name, ts DESC);
CREATE INDEX IF NOT EXISTS events_request_idx     ON ask_mesha.events (request_id);
