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
# Connection: pass a full admin DSN as $1 or via $ADMIN_DSN, or set PG* env
# (PGHOST/PGPORT/PGUSER/PGPASSWORD/PGDATABASE). Must connect as a role that owns
# / can grant on the public + ceo_ai schemas (e.g. the migration/admin role).
#
# Usage:
#   tools/dev/grant-assistant-public-read.sh "postgres://admin:...@host:5432/goatos"
#   ADMIN_DSN=... tools/dev/grant-assistant-public-read.sh
#   PGHOST=127.0.0.1 PGPORT=5433 PGUSER=postgres PGPASSWORD=... PGDATABASE=goatos \
#     tools/dev/grant-assistant-public-read.sh
# ===========================================================================
set -euo pipefail

DSN="${1:-${ADMIN_DSN:-}}"
PSQL=(psql -v ON_ERROR_STOP=1 -X)
if [[ -n "$DSN" ]]; then PSQL+=("$DSN"); fi

"${PSQL[@]}" <<'SQL'
DO $grants$
DECLARE
    r text;
BEGIN
    FOREACH r IN ARRAY ARRAY['mesha_ceo_readonly','mesha_cube_readonly'] LOOP
        IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = r) THEN
            EXECUTE format('GRANT USAGE ON SCHEMA public TO %I', r);
            EXECUTE format('GRANT SELECT ON ALL TABLES IN SCHEMA public TO %I', r);
            EXECUTE format('ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT ON TABLES TO %I', r);
            EXECUTE format('GRANT USAGE ON SCHEMA ceo_ai TO %I', r);
            EXECUTE format('GRANT SELECT ON ALL TABLES IN SCHEMA ceo_ai TO %I', r);
            EXECUTE format('ALTER DEFAULT PRIVILEGES IN SCHEMA ceo_ai GRANT SELECT ON TABLES TO %I', r);
            RAISE NOTICE 'granted read-only public + ceo_ai SELECT to %', r;
        ELSE
            RAISE WARNING 'role % does not exist; create it first, then re-run this script', r;
        END IF;
    END LOOP;
END;
$grants$;
SQL
echo "grant-assistant-public-read: done"
