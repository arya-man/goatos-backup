#!/usr/bin/env bash
# ===========================================================================
# grant-assistant-public-read.sh
#
# Idempotently (re)apply the read-only public + ceo_ai SELECT grants to the two
# leadership-assistant DB roles (mesha_ceo_readonly, mesha_cube_readonly). This
# is the ONE command to run AFTER creating the Cloud SQL readonly roles in any
# environment, because migration 000031 is guarded (IF EXISTS) and no-ops for a
# role that did not yet exist at migrate time. Read-only only (SELECT + schema
# USAGE + default privileges); grants no write/DDL.
#
# Future-table coverage: ALTER DEFAULT PRIVILEGES only affects tables later
# created by the role it NAMES. Migrations may create tables as an object owner
# different from the admin running this script, so this covers EVERY current
# table owner per schema via ALTER DEFAULT PRIVILEGES FOR ROLE <owner>. Setting
# default privileges FOR ROLE <owner> requires the connecting role to be a MEMBER
# of <owner> (or a superuser). An owner this role cannot cover is SKIPPED with a
# warning, and the script then FAILS (non-zero) so the incomplete coverage is not
# silently reported as success — unless ALLOW_PARTIAL=1 is set to accept it.
#
# Connection: pass a full admin DSN as $1 or via $ADMIN_DSN, or set PG* env
# (PGHOST/PGPORT/PGUSER/PGPASSWORD/PGDATABASE). Connect as a role that can grant
# on the schemas AND is a member of the object-owner role(s) migrations use (on
# Cloud SQL, typically cloudsqlsuperuser; the migration/admin role otherwise).
#
# Usage:
#   tools/dev/grant-assistant-public-read.sh "postgres://owner:...@host:5432/goatos"
#   ADMIN_DSN=... tools/dev/grant-assistant-public-read.sh
#   ALLOW_PARTIAL=1 ADMIN_DSN=... tools/dev/grant-assistant-public-read.sh   # accept partial coverage
#   PGHOST=127.0.0.1 PGPORT=5433 PGUSER=postgres PGPASSWORD=... PGDATABASE=goatos \
#     tools/dev/grant-assistant-public-read.sh
# ===========================================================================
set -euo pipefail

DSN="${1:-${ADMIN_DSN:-}}"
ALLOW_PARTIAL_FLAG="off"
if [[ "${ALLOW_PARTIAL:-}" == "1" || "${ALLOW_PARTIAL:-}" == "true" ]]; then
    ALLOW_PARTIAL_FLAG="on"
fi
PSQL=(psql -v ON_ERROR_STOP=1 -X -v "allow_partial=${ALLOW_PARTIAL_FLAG}")
if [[ -n "$DSN" ]]; then PSQL+=("$DSN"); fi

"${PSQL[@]}" <<'SQL'
-- Session-lifetime temp table (NOT ON COMMIT DROP: each psql statement autocommits,
-- so it must survive across the DO block and the \gset check below).
CREATE TEMP TABLE _grant_skips(role text, schema_name text, owner_role text);

DO $grants$
DECLARE
    r text;
    sch text;
    owner_role text;
BEGIN
    FOREACH r IN ARRAY ARRAY['mesha_ceo_readonly','mesha_cube_readonly'] LOOP
        IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = r) THEN
            RAISE WARNING 'role % does not exist; create it first, then re-run this script', r;
            CONTINUE;
        END IF;
        FOREACH sch IN ARRAY ARRAY['public','ceo_ai'] LOOP
            EXECUTE format('GRANT USAGE ON SCHEMA %I TO %I', sch, r);
            EXECUTE format('GRANT SELECT ON ALL TABLES IN SCHEMA %I TO %I', sch, r);
            -- Cover future tables of EVERY current owner in the schema, not just the
            -- connecting role. An owner this admin is not a member of is recorded as a
            -- skip (the shell then fails unless ALLOW_PARTIAL is set).
            FOR owner_role IN
                SELECT DISTINCT tableowner FROM pg_tables WHERE schemaname = sch
            LOOP
                BEGIN
                    EXECUTE format(
                        'ALTER DEFAULT PRIVILEGES FOR ROLE %I IN SCHEMA %I GRANT SELECT ON TABLES TO %I',
                        owner_role, sch, r);
                EXCEPTION WHEN insufficient_privilege THEN
                    INSERT INTO _grant_skips(role, schema_name, owner_role) VALUES (r, sch, owner_role);
                    RAISE WARNING 'skipped default privileges for future %.* tables owned by % (grantee %): re-run as a member of % or that owner''s DSN', sch, owner_role, r, owner_role;
                END;
            END LOOP;
        END LOOP;
        RAISE NOTICE 'granted read-only public + ceo_ai SELECT to %', r;
    END LOOP;
END;
$grants$;

-- Fail (non-zero) when any owner's future-table coverage was skipped, unless the
-- operator explicitly accepted partial coverage via ALLOW_PARTIAL=1.
SELECT (count(*) > 0) AS has_skips, count(*)::text AS skip_count FROM _grant_skips \gset
\if :has_skips
  \if :allow_partial
    \echo 'grant-assistant-public-read: ALLOW_PARTIAL set — continuing despite' :skip_count 'skipped owner(s); their future tables will miss SELECT'
  \else
    \echo 'grant-assistant-public-read: FAILED —' :skip_count 'owner(s) skipped; future tables they create will miss SELECT. Re-run as a member of those owners (or set ALLOW_PARTIAL=1 to accept partial coverage).'
    DO $fail$ BEGIN RAISE EXCEPTION 'incomplete future-table coverage: % owner(s) skipped', (SELECT count(*) FROM _grant_skips); END $fail$;
  \endif
\endif
SQL
echo "grant-assistant-public-read: done"
