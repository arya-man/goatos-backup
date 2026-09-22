-- +goose Up
-- 000387_manual_db_change_audit.sql
--
-- Manual DB change audit (docs/runbooks/manual-db-change-audit.md).
--
-- Codex / Claude / psql sessions sometimes write goatos-stg rows directly. Every such
-- INSERT / UPDATE / DELETE / TRUNCATE on a public table now lands in audit.db_changes
-- with before/after row JSON, who (app.actor), why (app.reason), DB user, client
-- application name and transaction id.
--
-- Scope = NON-SERVICE sessions only. Every Goat OS binary connects through
-- platform/postgres.Connect, which stamps application_name 'goatos-*'. The triggers
-- carry a WHEN clause on application_name, so service traffic never enters PL/pgSQL
-- (a C-level GUC compare per row, no audit write) and API latency is unaffected.
-- psql, psycopg2, one-off scripts, and agent sessions do not carry that prefix and
-- are recorded.
--
-- New tables: backend/cmd/migrate calls audit.attach_all() after every run, so a table
-- created by a later migration is covered on its next deploy.

SET LOCAL lock_timeout = '5s';

CREATE SCHEMA IF NOT EXISTS audit;

CREATE TABLE IF NOT EXISTS audit.db_changes (
  id               bigserial PRIMARY KEY,
  changed_at       timestamptz NOT NULL DEFAULT now(),
  txid             bigint      NOT NULL DEFAULT txid_current(),
  actor            text,
  reason           text,
  db_user          text        NOT NULL DEFAULT current_user,
  application_name text        DEFAULT current_setting('application_name', true),
  client_addr      inet        DEFAULT inet_client_addr(),
  table_schema     text        NOT NULL,
  table_name       text        NOT NULL,
  op               text        NOT NULL CHECK (op IN ('INSERT', 'UPDATE', 'DELETE', 'TRUNCATE')),
  old_row          jsonb,
  new_row          jsonb
);

CREATE INDEX IF NOT EXISTS db_changes_changed_at_idx ON audit.db_changes (changed_at DESC);
CREATE INDEX IF NOT EXISTS db_changes_table_idx ON audit.db_changes (table_name, changed_at DESC);
CREATE INDEX IF NOT EXISTS db_changes_txid_idx ON audit.db_changes (txid);

COMMENT ON TABLE audit.db_changes IS
  'Append-only log of manual (non-service) row changes. See docs/runbooks/manual-db-change-audit.md.';

-- Convenience: SELECT audit.begin_change('ravi via claude', 'why'); inside a transaction.
-- Transaction-local, so it cannot leak into a pooled connection.
CREATE OR REPLACE FUNCTION audit.begin_change(p_actor text, p_reason text)
RETURNS void LANGUAGE plpgsql AS $$
BEGIN
  IF coalesce(btrim(p_actor), '') = '' OR coalesce(btrim(p_reason), '') = '' THEN
    RAISE EXCEPTION 'audit.begin_change requires a non-empty actor and reason';
  END IF;
  PERFORM set_config('app.actor', p_actor, true);
  PERFORM set_config('app.reason', p_reason, true);
END $$;

CREATE OR REPLACE FUNCTION audit.log_row_change()
RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, audit AS $$
BEGIN
  INSERT INTO audit.db_changes (actor, reason, table_schema, table_name, op, old_row, new_row)
  VALUES (
    nullif(current_setting('app.actor', true), ''),
    nullif(current_setting('app.reason', true), ''),
    TG_TABLE_SCHEMA, TG_TABLE_NAME, TG_OP,
    CASE WHEN TG_OP IN ('UPDATE', 'DELETE') THEN to_jsonb(OLD) END,
    CASE WHEN TG_OP IN ('INSERT', 'UPDATE') THEN to_jsonb(NEW) END
  );
  RETURN NULL;
END $$;

CREATE OR REPLACE FUNCTION audit.log_truncate()
RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, audit AS $$
BEGIN
  INSERT INTO audit.db_changes (actor, reason, table_schema, table_name, op)
  VALUES (
    nullif(current_setting('app.actor', true), ''),
    nullif(current_setting('app.reason', true), ''),
    TG_TABLE_SCHEMA, TG_TABLE_NAME, 'TRUNCATE'
  );
  RETURN NULL;
END $$;

-- Append-only guard. Deters accidental edits of the trail; the schema owner can still
-- drop it deliberately, which itself shows up in migrations / git.
CREATE OR REPLACE FUNCTION audit.reject_mutation()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'audit.db_changes is append-only';
END $$;

DROP TRIGGER IF EXISTS db_changes_append_only ON audit.db_changes;
CREATE TRIGGER db_changes_append_only
  BEFORE UPDATE OR DELETE ON audit.db_changes
  FOR EACH ROW EXECUTE FUNCTION audit.reject_mutation();

DROP TRIGGER IF EXISTS db_changes_no_truncate ON audit.db_changes;
CREATE TRIGGER db_changes_no_truncate
  BEFORE TRUNCATE ON audit.db_changes
  FOR EACH STATEMENT EXECUTE FUNCTION audit.reject_mutation();

-- Idempotent: attaches the row + truncate triggers to every public base / partitioned
-- table that lacks them. Partitions are skipped (the parent's trigger clones to them).
-- Returns the number of tables newly attached.
CREATE OR REPLACE FUNCTION audit.attach_all()
RETURNS integer LANGUAGE plpgsql AS $$
DECLARE
  t record;
  n integer := 0;
BEGIN
  FOR t IN
    SELECT c.oid, n2.nspname, c.relname
    FROM pg_class c
    JOIN pg_namespace n2 ON n2.oid = c.relnamespace
    WHERE n2.nspname = 'public'
      AND c.relkind IN ('r', 'p')
      AND NOT c.relispartition
      AND c.relname <> 'goatos_schema_migrations'
      AND NOT EXISTS (
        SELECT 1 FROM pg_trigger tg
        WHERE tg.tgrelid = c.oid AND tg.tgname = 'zz_manual_change_audit'
      )
    ORDER BY c.relname
  LOOP
    EXECUTE format(
      'CREATE TRIGGER zz_manual_change_audit AFTER INSERT OR UPDATE OR DELETE ON %I.%I '
      'FOR EACH ROW WHEN (current_setting(''application_name'') NOT LIKE ''goatos-%%'') '
      'EXECUTE FUNCTION audit.log_row_change()', t.nspname, t.relname);
    EXECUTE format(
      'CREATE TRIGGER zz_manual_change_audit_truncate AFTER TRUNCATE ON %I.%I '
      'FOR EACH STATEMENT WHEN (current_setting(''application_name'') NOT LIKE ''goatos-%%'') '
      'EXECUTE FUNCTION audit.log_truncate()', t.nspname, t.relname);
    n := n + 1;
  END LOOP;
  RETURN n;
END $$;

SELECT audit.attach_all();
