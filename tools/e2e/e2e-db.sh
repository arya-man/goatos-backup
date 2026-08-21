#!/usr/bin/env bash
# E2E database control for a disposable Postgres clone.
#
# Baseline for every E2E case is a clone of a staging database held as a Postgres
# template, so a reset is `CREATE DATABASE ... TEMPLATE` (seconds) rather than
# restoring a dump.
#
#   ./e2e-db.sh baseline          (re)build the template from $E2E_SOURCE_DB
#   ./e2e-db.sh reset <name>      drop + recreate <name> from the template
#   ./e2e-db.sh url <name>        print the DATABASE_URL for <name>
#   ./e2e-db.sh counts <name>     obligation/version counts, for assertions
#   ./e2e-db.sh psql <name>       interactive shell
#
# Configuration comes from the environment; nothing host-specific is committed.
# Copy tools/e2e/e2e.env.example, fill it in locally, and source it. That file is
# gitignored on purpose — host addresses, key paths and credentials are personal
# infrastructure and must not live in this repo.
#
# SAFETY
#   * Connects only to $E2E_PG_HOST:$E2E_PG_PORT, which defaults to a local
#     forwarded port. It has no path to Cloud SQL.
#   * Every database name -- reset target, template AND source -- is allowlisted
#     to ^…[a-z0-9_]*$ before it reaches SQL. That character set cannot express a
#     quote, semicolon or comment, so no env var can inject.
#   * A refusal happens before any connection is opened. No combination of env
#     vars lets this script drop or read an unintended database.
#   * Guard behaviour is covered by tools/e2e/guards_test.sh, which needs no database.
set -euo pipefail

HOST="${E2E_PG_HOST:-127.0.0.1}"
PORT="${E2E_PG_PORT:-15432}"
USER="${E2E_PG_USER:-postgres}"
TEMPLATE="${E2E_TEMPLATE_DB:-goatos_base}"
SOURCE_DB="${E2E_SOURCE_DB:-goatos}"

# Every database this script may DROP must match one of these. A real database
# name can never match either pattern, so no combination of env vars lets this
# script destroy the source clone.
ALLOWED_RESET_RE='^goatos_e2e[a-z0-9_]*$'
ALLOWED_TEMPLATE_RE='^goatos_base[a-z0-9_]*$'
# The source clone is only ever read from, but it is still interpolated into SQL,
# so it is constrained to the same safe shape as everything else.
ALLOWED_SOURCE_RE='^goatos[a-z0-9_]*$'

require_password() {
  if [[ -z "${E2E_PG_PASSWORD:-}" ]]; then
    echo "E2E_PG_PASSWORD is not set. Copy tools/e2e/e2e.env.example, fill it in, and source it." >&2
    exit 1
  fi
  export PGPASSWORD="$E2E_PG_PASSWORD"
}

# NOTE: there is deliberately no escaping helper here. Every database name is
# validated against ^…[a-z0-9_]*$ before use, a character set that cannot express
# a quote, a semicolon or a comment marker. Names are therefore safe to
# interpolate, and there is no hand-rolled quoting to get wrong. An earlier
# version used a bespoke escaper that emitted backslash escapes and merely
# errored on hostile input — safe by accident, which is not a safety property.

q()     { psql -h "$HOST" -p "$PORT" -U "$USER" -d "${1:?db}" -v ON_ERROR_STOP=1 -tA -c "${2:?sql}"; }
admin() { psql -h "$HOST" -p "$PORT" -U "$USER" -d postgres  -v ON_ERROR_STOP=1 -c "${1:?sql}"; }

require_throwaway() {
  local name="${1:-}"
  [[ -n "$name" ]] || { echo "usage: $0 ${2:-reset} <dbname>" >&2; exit 2; }
  if [[ ! "$name" =~ $ALLOWED_RESET_RE ]]; then
    echo "refusing to touch '$name': only throwaway databases matching $ALLOWED_RESET_RE may be dropped." >&2
    exit 2
  fi
}

# `baseline` also drops a database. Guard it with its own allowlist, and refuse
# outright if the template has been pointed at the source clone.
require_source() {
  if [[ ! "$SOURCE_DB" =~ $ALLOWED_SOURCE_RE ]]; then
    echo "refusing to read from '$SOURCE_DB': E2E_SOURCE_DB must match $ALLOWED_SOURCE_RE." >&2
    exit 2
  fi
}

require_template() {
  if [[ ! "$TEMPLATE" =~ $ALLOWED_TEMPLATE_RE ]]; then
    echo "refusing to rebuild '$TEMPLATE': E2E_TEMPLATE_DB must match $ALLOWED_TEMPLATE_RE." >&2
    exit 2
  fi
  if [[ "$TEMPLATE" == "$SOURCE_DB" ]]; then
    echo "refusing to rebuild '$TEMPLATE': E2E_TEMPLATE_DB and E2E_SOURCE_DB are the same database." >&2
    exit 2
  fi
}

case "${1:-}" in
  baseline)
    # Name guards run before anything else, so a refusal never needs credentials
    # or a connection.
    require_template
    require_source
    require_password
    # Rebuild the immutable baseline from the current clone. The template itself
    # is never written to by a test.
    admin "DROP DATABASE IF EXISTS $TEMPLATE;"
    admin "CREATE DATABASE $TEMPLATE TEMPLATE $SOURCE_DB;"
    q postgres "select 'baseline rebuilt: '||pg_size_pretty(pg_database_size('$TEMPLATE'))" ;;

  reset)
    require_throwaway "${2:-}" reset
    require_template
    require_password
    # Terminating backends is not enough on its own: the API under test holds a
    # connection POOL, which reconnects between the terminate and the drop and makes
    # DROP DATABASE fail with "is being accessed by other users". Connections are
    # refused first, so the pool cannot get back in, and allowed again afterwards.
    admin "ALTER DATABASE $2 WITH ALLOW_CONNECTIONS false;" >/dev/null 2>&1 || true
    admin "SELECT pg_terminate_backend(pid) FROM pg_stat_activity
           WHERE datname = '$2' AND pid <> pg_backend_pid();" >/dev/null
    if ! admin "DROP DATABASE IF EXISTS $2;" >/dev/null 2>&1; then
      # A late reconnect can still win the race once; retry after another sweep
      # rather than failing the whole suite on a timing accident.
      sleep 1
      admin "SELECT pg_terminate_backend(pid) FROM pg_stat_activity
             WHERE datname = '$2' AND pid <> pg_backend_pid();" >/dev/null
      admin "DROP DATABASE IF EXISTS $2;"
    fi
    admin "CREATE DATABASE $2 TEMPLATE $TEMPLATE;"
    q postgres "select 'reset $2 from $TEMPLATE'" ;;

  url)
    require_password
    [[ -n "${2:-}" ]] || { echo "usage: $0 url <dbname>" >&2; exit 2; }
    echo "postgres://$USER:$E2E_PG_PASSWORD@$HOST:$PORT/$2?sslmode=disable" ;;

  counts)
    require_password
    [[ -n "${2:-}" ]] || { echo "usage: $0 counts <dbname>" >&2; exit 2; }
    psql -h "$HOST" -p "$PORT" -U "$USER" -d "$2" -tA -F'|' <<'SQL'
select 'obligations_total', count(*) from obligation_instances
union all select 'obl_'||status, count(*) from obligation_instances group by status
union all select 'protocol_versions', count(*) from protocol_versions
union all select 'protocol_versions_published', count(*) from protocol_versions where status='published'
union all select 'protocol_rules', count(*) from protocol_rules
order by 1;
SQL
    ;;

  psql)
    require_password
    [[ -n "${2:-}" ]] || { echo "usage: $0 psql <dbname>" >&2; exit 2; }
    psql -h "$HOST" -p "$PORT" -U "$USER" -d "$2" ;;

  *) sed -n '2,20p' "$0"; exit 1 ;;
esac
