-- Ask Mesha agent state. Owned by a dedicated writable app role (never
-- mesha_ceo_readonly). Idempotent: safe to run on every start with
-- ASK_MESHA_DB_MIGRATE=1.
CREATE SCHEMA IF NOT EXISTS ask_mesha;

CREATE TABLE IF NOT EXISTS ask_mesha.chats (
  id          uuid PRIMARY KEY,
  email       text        NOT NULL,
  tenant_id   text        NOT NULL DEFAULT '',   -- X-GoatOS-Tenant-ID the chat was created under
  title       text        NOT NULL DEFAULT 'New chat',
  session_id  text,
  worktree    text,
  busy_until  timestamptz,              -- per-chat run lock (DB-safe across instances)
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  deleted_at  timestamptz
);
CREATE INDEX IF NOT EXISTS chats_email_updated_idx ON ask_mesha.chats (email, updated_at DESC);

CREATE TABLE IF NOT EXISTS ask_mesha.messages (
  id          uuid PRIMARY KEY,
  chat_id     uuid        NOT NULL REFERENCES ask_mesha.chats(id) ON DELETE CASCADE,
  role        text        NOT NULL,
  content     text        NOT NULL DEFAULT '',
  chart       jsonb,
  files       jsonb,
  source      text,
  mode        text,
  request_id  text,
  created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS messages_chat_created_idx ON ask_mesha.messages (chat_id, created_at);

CREATE TABLE IF NOT EXISTS ask_mesha.metrics (
  id  bigserial PRIMARY KEY,
  ts  timestamptz NOT NULL DEFAULT now(),
  row jsonb       NOT NULL
);
CREATE INDEX IF NOT EXISTS metrics_ts_idx ON ask_mesha.metrics (ts DESC);

-- Claude Agent SDK transcript mirror (Options.sessionStore), so a chat can be
-- resumed on any Cloud Run instance.
CREATE TABLE IF NOT EXISTS ask_mesha.session_entries (
  seq          bigserial PRIMARY KEY,
  project_key  text  NOT NULL,
  session_id   text  NOT NULL,
  subpath      text  NOT NULL DEFAULT '',
  uuid         text,
  entry        jsonb NOT NULL,
  created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS session_entries_key_idx ON ask_mesha.session_entries (project_key, session_id, subpath, seq);
CREATE UNIQUE INDEX IF NOT EXISTS session_entries_uuid_uq
  ON ask_mesha.session_entries (project_key, session_id, subpath, uuid) WHERE uuid IS NOT NULL;
CREATE INDEX IF NOT EXISTS session_entries_session_idx ON ask_mesha.session_entries (session_id);

-- Existing installs: add tenant scoping and index it with the owner.
ALTER TABLE ask_mesha.chats ADD COLUMN IF NOT EXISTS tenant_id text NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS chats_owner_tenant_idx ON ask_mesha.chats (email, tenant_id, updated_at DESC);
