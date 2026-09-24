-- Ask Mesha pending CEO write actions (tools/ask-mesha-agent/actions.mjs).
-- Idempotent: applied on start with ASK_MESHA_DB_MIGRATE=1 (after 002_events.sql).
-- Holds the resolved backend request + preview only. NEVER a bearer token: the CEO's own
-- Firebase token comes with the confirm request and is used once, in memory.
CREATE SCHEMA IF NOT EXISTS ask_mesha;

CREATE TABLE IF NOT EXISTS ask_mesha.pending_actions (
  id                       uuid        PRIMARY KEY,   -- also the backend Idempotency-Key
  email                    text        NOT NULL,
  tenant_id                text        NOT NULL DEFAULT '',
  chat_id                  text,
  action                   text        NOT NULL,
  request                  jsonb       NOT NULL,       -- {method, path, body}
  title                    text        NOT NULL,
  summary                  jsonb       NOT NULL,       -- preview lines
  risk                     text        NOT NULL DEFAULT 'normal',  -- normal | high
  requires_double_confirm  boolean     NOT NULL DEFAULT false,
  status                   text        NOT NULL DEFAULT 'pending', -- pending | executing | executed | failed | transport_failed | refused | cancelled
  attempts                 int         NOT NULL DEFAULT 0,
  result                   jsonb,
  created_at               timestamptz NOT NULL DEFAULT now(),
  expires_at               timestamptz NOT NULL
);
CREATE INDEX IF NOT EXISTS pending_actions_owner_idx ON ask_mesha.pending_actions (email, tenant_id, created_at DESC);
