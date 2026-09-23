#!/usr/bin/env bash
# ro-sql.sh "<one SQL statement>" — the ONLY DB entry point the headless data-map
# reviewer (refresh-data-map.sh) is allowed. Refuses psql meta-commands (`\!` would
# run a shell) and forces a read-only transaction; the DB user is read-only too.
set -euo pipefail
[ "$#" = 1 ] || { echo "usage: ro-sql.sh '<sql>'" >&2; exit 2; }
SQL="${1%"${1##*[![:space:]]}"}"
SQL="${SQL#"${SQL%%[![:space:]]*}"}"
SQL_NO_TRAIL="${SQL%;}"
case "$SQL" in *\\*) echo "ro-sql: backslash/meta-commands are not allowed" >&2; exit 2 ;; esac
case "$SQL" in *\"*) echo "ro-sql: double-quoted identifiers are not allowed; use ceo_ai.<view>" >&2; exit 2 ;; esac
case "$SQL_NO_TRAIL" in *";"*) echo "ro-sql: exactly one SQL statement is allowed" >&2; exit 2 ;; esac
case "$SQL_NO_TRAIL" in
  [sS][eE][lL][eE][cC][tT]*) ;;
  *) echo "ro-sql: only flat SELECT queries are allowed" >&2; exit 2 ;;
esac
if printf '%s\n' "$SQL_NO_TRAIL" | grep -Eiq '\b(insert|update|delete|merge|alter|create|drop|truncate|grant|revoke|copy|vacuum|analyze|call|do|execute|prepare|set|reset|listen|notify|lock)\b'; then
  echo "ro-sql: mutating/session-control SQL is not allowed" >&2
  exit 2
fi
SOURCE_CLAUSE="$(printf '%s\n' "$SQL_NO_TRAIL" | perl -0777 -ne 'print $1 if /\bfrom\b([\s\S]*?)(?:\bwhere\b|\bgroup\s+by\b|\border\s+by\b|\blimit\b|$)/i')"
if printf '%s\n' "$SOURCE_CLAUSE" | grep -Eiq ',[[:space:]]*[a-z_]'; then
  echo "ro-sql: comma-joined sources are not allowed; use explicit ceo_ai.* JOINs" >&2
  exit 2
fi
if printf '%s\n' "$SQL_NO_TRAIL" | grep -Eiq '\([[:space:]]*select\b'; then
  echo "ro-sql: subqueries are not allowed in this read-only tool" >&2
  exit 2
fi
if ! printf '%s\n' "$SQL_NO_TRAIL" | grep -Eiq '\b(from|join)[[:space:]]+'; then
  echo "ro-sql: query must read from a ceo_ai.* view" >&2
  exit 2
fi
if printf '%s\n' "$SQL_NO_TRAIL" | grep -Eio '\b(from|join)[[:space:]]+[a-z_][a-z0-9_$]*(\.[a-z_][a-z0-9_$]*)?' | grep -Eiv '\b(from|join)[[:space:]]+ceo_ai\.' >/dev/null; then
  echo "ro-sql: queries may read only explicitly-qualified ceo_ai.* views" >&2
  exit 2
fi
export PGOPTIONS="-c default_transaction_read_only=on -c statement_timeout=30000"
exec psql -X -A -v ON_ERROR_STOP=1 -c "BEGIN READ ONLY; ${SQL_NO_TRAIL}; ROLLBACK;"
