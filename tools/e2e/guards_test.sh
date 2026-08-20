#!/usr/bin/env bash
# Guard tests for oci-db.sh. Needs no database: every case asserts that the
# script refuses BEFORE opening a connection, so these run anywhere.
#
#   ./tools/e2e/guards_test.sh
set -uo pipefail
cd "$(dirname "$0")"
SUT=./oci-db.sh
pass=0; fail=0

# Deliberately absent credentials and an unroutable host: if a guard ever lets
# execution through to psql, the test notices rather than silently connecting.
export E2E_PG_HOST=203.0.113.1 E2E_PG_PORT=1 E2E_PG_USER=nobody
unset E2E_PG_PASSWORD || true

refuses() { # refuses <description> <expected-substring> <args...>
  local desc="$1" want="$2"; shift 2
  local out rc
  out=$("$SUT" "$@" 2>&1); rc=$?
  if [[ $rc -ne 0 && "$out" == *"$want"* ]]; then
    echo "  ok   $desc"; pass=$((pass+1))
  else
    echo "  FAIL $desc"; echo "       rc=$rc out=$out"; fail=$((fail+1))
  fi
}

echo "reset: only throwaway databases may be dropped"
refuses "refuses the source clone"        "refusing to touch 'goatos'"        reset goatos
refuses "refuses the template itself"     "refusing to touch 'goatos_base'"   reset goatos_base
refuses "refuses a production-ish name"   "refusing to touch"                 reset goatos_prod
refuses "refuses SQL injection"           "refusing to touch"                 reset 'goatos"; DROP DATABASE goatos; --'
refuses "refuses a name with a space"     "refusing to touch"                 reset 'goatos_e2e evil'
refuses "refuses uppercase"               "refusing to touch"                 reset GOATOS_E2E
refuses "refuses an empty name"           "usage"                             reset

echo "baseline: the template is allowlisted too"
E2E_TEMPLATE_DB=goatos           refuses "refuses template=source clone"  "refusing to rebuild 'goatos'"      baseline
E2E_TEMPLATE_DB=goatos_prod      refuses "refuses an arbitrary template"  "refusing to rebuild"               baseline
E2E_TEMPLATE_DB='x; DROP DATABASE goatos'  refuses "refuses injection"    "refusing to rebuild"               baseline
E2E_TEMPLATE_DB=goatos_base E2E_SOURCE_DB=goatos_base \
                                 refuses "refuses template == source"     "are the same database"             baseline

echo "credentials are required once a name is accepted"
refuses "reset with a valid name still needs a password" "E2E_PG_PASSWORD is not set" reset goatos_e2e
refuses "baseline with a valid template still needs one" "E2E_PG_PASSWORD is not set" baseline

echo
echo "passed $pass, failed $fail"
[[ $fail -eq 0 ]]
