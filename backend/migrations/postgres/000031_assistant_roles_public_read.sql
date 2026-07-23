-- +goose Up
-- +goose StatementBegin
-- ===========================================================================
-- Full read (SELECT) on schema public for the leadership-assistant read-only
-- DB roles.
--
-- MAINTAINER DECISION 2026-07-23: the leadership assistant is an INTERNAL,
-- CEO/CXO-only, READ-ONLY chat. It should not hit a permission wall on current
-- public tables. The prior boundary (ceo_ai.* SELECT only, public
-- revoked) caused governed Cube metrics that read public tables to fail with
-- `permission denied for table ...`. Per the maintainer, remove that schema
-- restriction: grant the assistant roles SELECT on ALL current public tables.
-- Future tables are covered by ALTER DEFAULT PRIVILEGES FOR ROLE <owner>, applied
-- per current table owner by tools/dev/grant-assistant-public-read.sh (a brand-new
-- owner role needs a re-run). Access stays READ-ONLY — only SELECT is
-- granted and the roles carry `default_transaction_read_only = on`; no INSERT/
-- UPDATE/DELETE/DDL is granted. This supersedes the "Cube never reads raw
-- Postgres" restriction for these two read-only roles only.
--
-- ORDERING (P1): this is GUARDED with IF EXISTS. A role that does NOT yet exist
-- when this migration runs receives NO grant here. Cloud Deploy running the
-- migration ALONE does not guarantee the grant lands. Create the Cloud SQL
-- readonly roles BEFORE migrating, or re-apply afterward (idempotent) with:
--     make grant-assistant-public-read   (tools/dev/grant-assistant-public-read.sh)
-- The local path (tools/dev/setup-ceo-ai-local-role.sh) creates the roles then
-- grants in the correct order already.
--
-- DEFAULT PRIVILEGES SCOPE (P2): ALTER DEFAULT PRIVILEGES below covers only
-- tables created by the SAME role that executes this migration. This assumes all
-- schema migrations run as one owner (the migration role). Tables later created
-- by a DIFFERENT owner do NOT auto-grant SELECT and need a re-run of the grant
-- command above.
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
