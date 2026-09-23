#!/usr/bin/env bash
# ro-sql.sh "<one SQL statement>" — the ONLY DB entry point the headless data-map
# reviewer (refresh-data-map.sh) is allowed. Refuses psql meta-commands (`\!` would
# run a shell) and forces a read-only transaction; the DB user is read-only too.
set -euo pipefail
[ "$#" = 1 ] || { echo "usage: ro-sql.sh '<sql>'" >&2; exit 2; }
case "$1" in *\\*) echo "ro-sql: backslash/meta-commands are not allowed" >&2; exit 2 ;; esac
export PGOPTIONS="-c default_transaction_read_only=on -c statement_timeout=30000"
exec psql -X -A -v ON_ERROR_STOP=1 -c "$1"
