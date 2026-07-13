#!/usr/bin/env bash
# Tests for the DRV-R3 local-DB mutation guard (tools/dev/lib/db-mutation-guard.sh) and the
# DRV-R3a/DRV-R3b behaviours in the local stack scripts. Run via `make db-mutation-guard-test`.
set -uo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=tools/dev/lib/db-mutation-guard.sh
. "$here/lib/db-mutation-guard.sh"

fail=0
check() { # desc expected actual
  if [ "$2" = "$3" ]; then echo "ok   - $1"; else echo "FAIL - $1: expected [$2] got [$3]"; fail=1; fi
}

fake_detected() { echo "postgres://postgres:goatos@127.0.0.1:55432/goatos?sslmode=disable"; }
fake_none() { :; } # echoes nothing (no recognized docker container)

# 1. Inherited DATABASE_URL -> UNTRUSTED.
r=$( DATABASE_URL="postgres://u:p@10.0.0.5:5432/prod?sslmode=disable" db_url_inherited= bash -c '
  . "'"$here"'/lib/db-mutation-guard.sh"; resolve_database_url; echo "$db_url_inherited"' )
check "inherited DATABASE_URL is untrusted" 1 "$r"

# 2. Detected docker container -> TRUSTED, DATABASE_URL = detected.
out=$( bash -c '
  . "'"$here"'/lib/db-mutation-guard.sh"
  fake_detected() { echo "postgres://postgres:goatos@127.0.0.1:55432/goatos?sslmode=disable"; }
  unset DATABASE_URL; resolve_database_url fake_detected; echo "$db_url_inherited|$DATABASE_URL"' )
check "detected docker URL is trusted" "0|postgres://postgres:goatos@127.0.0.1:55432/goatos?sslmode=disable" "$out"

# 3. No docker container -> hardcoded 127.0.0.1:5433 fallback is UNTRUSTED (DRV-R3a).
out=$( bash -c '
  . "'"$here"'/lib/db-mutation-guard.sh"
  fake_none() { :; }
  unset DATABASE_URL; resolve_database_url fake_none; echo "$db_url_inherited|$DATABASE_URL"' )
check "no-docker 127.0.0.1:5433 fallback is untrusted (DRV-R3a)" "1|postgres://postgres:goatos@127.0.0.1:5433/goatos?sslmode=disable" "$out"

# 4. assert refuses UNTRUSTED without opt-in.
( db_url_inherited=1; unset GOATOS_ALLOW_DB_MUTATION; assert_mutable_local_db ) >/dev/null 2>&1 && r=0 || r=1
check "assert refuses untrusted DB without opt-in" 1 "$r"

# 5. assert allows UNTRUSTED with explicit opt-in.
( db_url_inherited=1; GOATOS_ALLOW_DB_MUTATION=1; assert_mutable_local_db ) >/dev/null 2>&1 && r=1 || r=0
check "assert allows untrusted DB with GOATOS_ALLOW_DB_MUTATION=1" 1 "$r"

# 6. assert allows TRUSTED.
( db_url_inherited=0; unset GOATOS_ALLOW_DB_MUTATION; assert_mutable_local_db ) >/dev/null 2>&1 && r=1 || r=0
check "assert allows trusted DB" 1 "$r"

sup="$here/run-local-stack-supervised.sh"

# 7. DRV-R3b: the refresher sleeps BEFORE its first recompute (no immediate mutation on start/restart).
sleep_line=$(grep -nE '^[[:space:]]*sleep "\$interval"' "$sup" | head -1 | cut -d: -f1)
recompute_line=$(grep -nE 'run_projection_refresh vaccination_shed' "$sup" | head -1 | cut -d: -f1)
if [ -n "$sleep_line" ] && [ -n "$recompute_line" ] && [ "$sleep_line" -lt "$recompute_line" ]; then r=1; else r=0; fi
check "supervised refresher sleeps before first recompute (DRV-R3b)" 1 "$r"

# 8. DRV-R3b: migrate/seed/closeout are NOT invoked inside the restart while-loop.
loop_body=$(awk '/^while \[ "\$stop_requested" = "0" \]; do/{f=1} f{print} f&&/^done$/{exit}' "$sup")
if printf '%s\n' "$loop_body" | grep -qE 'migrate_local_database|seed_closeout_if_present|seed_dev_grant'; then r=0; else r=1; fi
check "supervised does not re-migrate/seed inside the restart loop (DRV-R3b)" 1 "$r"

# 9. Both stack scripts hand off GOATOS_LOCAL_DB_PREPARED=1 to dev:local (single-owner prep).
if grep -q 'GOATOS_LOCAL_DB_PREPARED=1 npm' "$here/run-local-stack.sh" && grep -q 'GOATOS_LOCAL_DB_PREPARED=1 npm' "$sup"; then r=1; else r=0; fi
check "stack scripts hand off GOATOS_LOCAL_DB_PREPARED=1 to dev:local" 1 "$r"

if [ "$fail" = "0" ]; then echo "db-mutation-guard: all tests passed"; else echo "db-mutation-guard: FAILURES"; exit 1; fi
