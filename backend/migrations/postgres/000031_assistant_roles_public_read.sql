-- +goose Up
-- +goose StatementBegin
-- ===========================================================================
-- Full read (SELECT) on schema public for the leadership-assistant read-only
-- DB roles.
--
-- MAINTAINER DECISION 2026-07-23: the leadership assistant is an INTERNAL,
-- CEO/CXO-only, READ-ONLY chat. It must never hit a permission wall on any
-- current or future table. The prior boundary (ceo_ai.* SELECT only, public
-- revoked) caused governed Cube metrics that read public tables to fail with
-- `permission denied for table ...`. Per the maintainer, remove that schema
-- restriction: grant the assistant roles SELECT on ALL public tables plus
-- default privileges for future tables. Access stays READ-ONLY — only SELECT is
-- granted and the roles carry `default_transaction_read_only = on`; no INSERT/
-- UPDATE/DELETE/DDL is granted. This supersedes the "Cube never reads raw
-- Postgres" restriction for these two read-only roles only.
--
-- Idempotent + guarded: applies only to roles that exist, so it is safe in
-- local, stg, and prod (roles are provisioned per-environment).
-- ===========================================================================
DO $grants$
DECLARE
    r text;
BEGIN
    FOREACH r IN ARRAY ARRAY['mesha_ceo_readonly','mesha_cube_readonly'] LOOP
        IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = r) THEN
            EXECUTE format('GRANT USAGE ON SCHEMA public TO %I', r);
            EXECUTE format('GRANT SELECT ON ALL TABLES IN SCHEMA public TO %I', r);
            EXECUTE format('ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT ON TABLES TO %I', r);
        END IF;
    END LOOP;
END;
$grants$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $revoke$
DECLARE
    r text;
BEGIN
    FOREACH r IN ARRAY ARRAY['mesha_ceo_readonly','mesha_cube_readonly'] LOOP
        IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = r) THEN
            EXECUTE format('ALTER DEFAULT PRIVILEGES IN SCHEMA public REVOKE SELECT ON TABLES FROM %I', r);
            EXECUTE format('REVOKE SELECT ON ALL TABLES IN SCHEMA public FROM %I', r);
            EXECUTE format('REVOKE USAGE ON SCHEMA public FROM %I', r);
        END IF;
    END LOOP;
END;
$revoke$;
-- +goose StatementEnd
