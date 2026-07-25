#!/usr/bin/env bash
# ===========================================================================
# setup-ceo-ai-local-role.sh
#
# Creates the TWO read-only LOGIN roles the Mesha leadership assistant stack
# uses against the LOCAL Postgres, and writes their DSNs to a gitignored
# .env.ceo-ai.local. NEVER commit a secret VALUE — this script generates fresh
# local-only passwords and writes them to the ignored env file. Runtime + CI
# pull the real values from Google Secret Manager (project goatos-stg / repo
# vgoats/goatos), documented in docs/ceo-ai/README (names + retrieval steps).
#
#   mesha_ceo_readonly   — MCP Toolbox / SQL-fallback reader (MESHA_MCP_DB_*)
#   mesha_cube_readonly  — Cube Core's DB user            (MESHA_CUBE_DB_*)
#
# Both roles:
#   * LOGIN with a generated local password
#   * statement_timeout + idle_in_transaction_session_timeout
#   * default_transaction_read_only = on
#   * GRANT USAGE + SELECT on ALL of schema public (current + future tables)
#     — maintainer decision 2026-07-23, matches migration 000031. Read-only is
#     enforced by default_transaction_read_only=on + SELECT-only grants.
#   * GRANT USAGE + SELECT on the ceo_ai reporting schema
#   * NO execute on ceo_ai.run_readonly_sql (denied until the backend validator)
#
# Re-runnable: roles are created if missing and their grants re-applied. Passing
# --rotate regenerates the passwords.
#
# Usage:
#   tools/dev/setup-ceo-ai-local-role.sh [--rotate]
#
# Env overrides (defaults target the canonical local stack):
#   GOATOS_LOCAL_PGHOST      (default 127.0.0.1)
#   GOATOS_LOCAL_PGPORT      (default 5433)
#   GOATOS_LOCAL_PGDATABASE  (default goatos)
#   GOATOS_LOCAL_PG_SUPERUSER / GOATOS_LOCAL_PG_SUPERPASS (admin conn; default postgres/goatos)
#   GOATOS_LOCAL_PG_CONTAINER (default goatos-local-current) — used when psql is
#                             not on PATH; runs psql inside the container.
# ===========================================================================
set -euo pipefail

ROTATE=0
[[ "${1:-}" == "--rotate" ]] && ROTATE=1

PGHOST="${GOATOS_LOCAL_PGHOST:-127.0.0.1}"
PGPORT="${GOATOS_LOCAL_PGPORT:-5433}"
PGDATABASE="${GOATOS_LOCAL_PGDATABASE:-goatos}"
SUPERUSER="${GOATOS_LOCAL_PG_SUPERUSER:-postgres}"
SUPERPASS="${GOATOS_LOCAL_PG_SUPERPASS:-goatos}"
CONTAINER="${GOATOS_LOCAL_PG_CONTAINER:-goatos-local-current}"

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
ENV_FILE="${REPO_ROOT}/.env.ceo-ai.local"

CEO_ROLE="mesha_ceo_readonly"
CUBE_ROLE="mesha_cube_readonly"

STATEMENT_TIMEOUT="8000ms"
IDLE_TX_TIMEOUT="5000ms"

# ---- admin psql runner: prefer local psql, fall back to docker exec ----------
run_admin_sql() {
    local sql="$1"
    if command -v psql >/dev/null 2>&1; then
        PGPASSWORD="$SUPERPASS" psql -v ON_ERROR_STOP=1 -q \
            -h "$PGHOST" -p "$PGPORT" -U "$SUPERUSER" -d "$PGDATABASE" \
            -c "$sql"
    else
        docker exec -e PGPASSWORD="$SUPERPASS" -i "$CONTAINER" \
            psql -v ON_ERROR_STOP=1 -q -U "$SUPERUSER" -d "$PGDATABASE" -c "$sql"
    fi
}

gen_password() {
    # URL-safe local password; not a production secret. openssl avoids the
    # tr|head SIGPIPE under `set -o pipefail`.
    if command -v openssl >/dev/null 2>&1; then
        openssl rand -hex 24
    else
        LC_ALL=C tr -dc 'A-Za-z0-9' < /dev/urandom 2>/dev/null | dd bs=32 count=1 2>/dev/null
    fi
}

role_exists() {
    local role="$1" out
    if command -v psql >/dev/null 2>&1; then
        out=$(PGPASSWORD="$SUPERPASS" psql -tA -h "$PGHOST" -p "$PGPORT" -U "$SUPERUSER" -d "$PGDATABASE" \
            -c "SELECT 1 FROM pg_roles WHERE rolname='$role'")
    else
        out=$(docker exec -e PGPASSWORD="$SUPERPASS" -i "$CONTAINER" \
            psql -tA -U "$SUPERUSER" -d "$PGDATABASE" -c "SELECT 1 FROM pg_roles WHERE rolname='$role'")
    fi
    [[ "$(echo "$out" | tr -d '[:space:]')" == "1" ]]
}

configure_role() {
    local role="$1" password="$2"
    # ALWAYS apply the resolved password to the role, whether it was reused from
    # the env file or freshly generated. The written DSN (below) carries this
    # exact value, so the role password and the DSN can never diverge. Skipping
    # the ALTER for a pre-existing role would emit a DSN whose password was never
    # set on the role (fresh password + absent/regenerated env file), producing
    # an un-authenticatable DSN and a non-reproducible readonly-denial proof.
    if role_exists "$role"; then
        run_admin_sql "ALTER ROLE ${role} LOGIN PASSWORD '${password}';"
        if [[ "$ROTATE" == "1" ]]; then
            echo "  rotated password for ${role}"
        else
            echo "  ${role} already exists (password re-applied to match DSN)"
        fi
    else
        run_admin_sql "CREATE ROLE ${role} LOGIN PASSWORD '${password}';"
        echo "  created ${role}"
    fi
    # hardening + read-only defaults
    run_admin_sql "ALTER ROLE ${role} SET statement_timeout = '${STATEMENT_TIMEOUT}';"
    run_admin_sql "ALTER ROLE ${role} SET idle_in_transaction_session_timeout = '${IDLE_TX_TIMEOUT}';"
    run_admin_sql "ALTER ROLE ${role} SET default_transaction_read_only = on;"
    # Maintainer decision 2026-07-23: the internal CEO-only assistant must never
    # hit a permission wall. Grant read-only SELECT on ALL of public (current +
    # future tables) in addition to the ceo_ai reporting schema. Read-only is
    # still enforced by default_transaction_read_only=on + SELECT-only grants.
    run_admin_sql "GRANT USAGE ON SCHEMA public TO ${role};"
    run_admin_sql "GRANT SELECT ON ALL TABLES IN SCHEMA public TO ${role};"
    run_admin_sql "ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT ON TABLES TO ${role};"
    run_admin_sql "GRANT USAGE ON SCHEMA ceo_ai TO ${role};"
    run_admin_sql "GRANT SELECT ON ALL TABLES IN SCHEMA ceo_ai TO ${role};"
    run_admin_sql "ALTER DEFAULT PRIVILEGES IN SCHEMA ceo_ai GRANT SELECT ON TABLES TO ${role};"
    # the dynamic-SQL fallback stays denied until the backend validator lands
    run_admin_sql "REVOKE ALL ON FUNCTION ceo_ai.run_readonly_sql(text, uuid) FROM ${role};"
}

echo "==> ceo_ai read-only role setup on ${PGHOST}:${PGPORT}/${PGDATABASE}"

# ceo_ai schema must exist (migration 000020). Fail loudly if not.
if ! run_admin_sql "SELECT 1 FROM pg_namespace WHERE nspname='ceo_ai';" | grep -q 1; then
    echo "ERROR: schema ceo_ai not found. Apply migrations first:" >&2
    echo "       (cd backend && DATABASE_URL=... go run ./cmd/migrate)" >&2
    exit 1
fi

# ---- reuse existing passwords from the env file unless rotating --------------
CEO_PW=""; CUBE_PW=""
if [[ "$ROTATE" == "0" && -f "$ENV_FILE" ]]; then
    CEO_PW="$(grep -E '^MESHA_MCP_DB_PASSWORD=' "$ENV_FILE" | head -1 | cut -d= -f2- || true)"
    CUBE_PW="$(grep -E '^MESHA_CUBE_DB_PASSWORD=' "$ENV_FILE" | head -1 | cut -d= -f2- || true)"
fi
[[ -z "$CEO_PW" ]]  && CEO_PW="$(gen_password)"
[[ -z "$CUBE_PW" ]] && CUBE_PW="$(gen_password)"

configure_role "$CEO_ROLE"  "$CEO_PW"
configure_role "$CUBE_ROLE" "$CUBE_PW"

# ---- write gitignored env file ----------------------------------------------
CEO_DSN="postgres://${CEO_ROLE}:${CEO_PW}@${PGHOST}:${PGPORT}/${PGDATABASE}?sslmode=disable"
CUBE_DSN="postgres://${CUBE_ROLE}:${CUBE_PW}@${PGHOST}:${PGPORT}/${PGDATABASE}?sslmode=disable"

umask 077
cat > "$ENV_FILE" <<EOF
# GENERATED by tools/dev/setup-ceo-ai-local-role.sh — DO NOT COMMIT.
# Local-only read-only DSNs for the Mesha leadership assistant stack.
# Real staging/prod values come from Google Secret Manager (goatos-stg) and
# GitHub Actions secrets (vgoats/goatos); this file is for local dev only.

# --- MCP Toolbox / SQL-fallback reader (public + ceo_ai SELECT only) ---
MESHA_MCP_DB_USER=${CEO_ROLE}
MESHA_MCP_DB_PASSWORD=${CEO_PW}
MESHA_MCP_DB_DSN=${CEO_DSN}
MESHA_MCP_TOOLSET=mesha_ceo_toolset

# --- Cube Core governed metric layer's DB user (public + ceo_ai SELECT only) ---
MESHA_CUBE_DB_USER=${CUBE_ROLE}
MESHA_CUBE_DB_PASSWORD=${CUBE_PW}
MESHA_CUBE_DB_DSN=${CUBE_DSN}
MESHA_CUBE_URL=http://127.0.0.1:4000

# --- shared local Postgres coordinates ---
MESHA_DATABASE_NAME=${PGDATABASE}
EOF

echo "==> wrote ${ENV_FILE} (chmod 600, gitignored)"
echo "    ${CEO_ROLE}  -> MESHA_MCP_DB_*"
echo "    ${CUBE_ROLE} -> MESHA_CUBE_DB_*"
echo "Done. Both roles are READ-ONLY over all of public + ceo_ai (SELECT only)."
