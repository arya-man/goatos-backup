#!/usr/bin/env bash
# Pins every branch of land_route_decide (tools/ci/land-route.sh), with the
# per-machine opt-ins (GOATOS_WORKSPACE_ROOT, GOATOS_LAND_VIA_QUEUE) both unset
# (other developers: exactly today's local landing) and set (maintainer laptop).
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
fails=0
check() { # expected, repo, env...
  local want="$1" repo="$2"; shift 2
  local got
  got="$(env -u GITHUB_ACTIONS -u GOATOS_LAND_TEST_MODE -u GOATOS_LAND_LOCAL \
    -u GOATOS_LAND_VIA_QUEUE -u GOATOS_WORKSPACE_ROOT "$@" \
    bash -c ". '$here/land-route.sh'; land_route_decide '$repo'")"
  if [ "$got" = "$want" ]; then echo "ok   $want <- $repo $*"; else echo "FAIL want=$want got=$got <- $repo $*"; fails=$((fails+1)); fi
}
M=/Users/tester/mesha
# ── unset: other developers — local landing anywhere, never the queue ──
check local  /home/dev/src/goatos
check local  /private/tmp/scratchpad/goatos-pr1
check local  $M/goatos-wt-x
check local  $M/goatos-wt-x GOATOS_LAND_LOCAL=1
# ── queue opt-in only ──
check queue  /home/dev/src/goatos GOATOS_LAND_VIA_QUEUE=1
check local  /home/dev/src/goatos GOATOS_LAND_VIA_QUEUE=1 GOATOS_LAND_LOCAL=1
# ── workspace root only: location enforced, still local ──
check local  $M/goatos-wt-x GOATOS_WORKSPACE_ROOT=$M
check local  $M/goatos-wt-x GOATOS_WORKSPACE_ROOT=$M/
check refuse /private/tmp/scratchpad/goatos-pr1 GOATOS_WORKSPACE_ROOT=$M
check refuse /Users/tester/meshaX/goatos GOATOS_WORKSPACE_ROOT=$M
# ── both set: the maintainer laptop ──
check queue  $M/goatos-wt-x GOATOS_WORKSPACE_ROOT=$M GOATOS_LAND_VIA_QUEUE=1
check queue  $M/goatos GOATOS_WORKSPACE_ROOT=$M GOATOS_LAND_VIA_QUEUE=1
check local  $M/goatos-wt-x GOATOS_WORKSPACE_ROOT=$M GOATOS_LAND_VIA_QUEUE=1 GOATOS_LAND_LOCAL=1
check refuse /private/tmp/scratchpad/goatos-pr1 GOATOS_WORKSPACE_ROOT=$M GOATOS_LAND_VIA_QUEUE=1
check refuse /private/tmp/scratchpad/goatos-pr1 GOATOS_WORKSPACE_ROOT=$M GOATOS_LAND_VIA_QUEUE=1 GOATOS_LAND_LOCAL=1
# ── the runner and the test harness always land locally ──
check local  /private/tmp/runner/_work/goatos GITHUB_ACTIONS=true GOATOS_WORKSPACE_ROOT=$M GOATOS_LAND_VIA_QUEUE=1
check local  /tmp/goatos-land-main-test.abc/candidate GOATOS_LAND_TEST_MODE=1 GOATOS_WORKSPACE_ROOT=$M GOATOS_LAND_VIA_QUEUE=1
[ "$fails" -eq 0 ] || { echo "land-route self-test: $fails failure(s)"; exit 1; }
echo "land-route self-test: all cases pass"
