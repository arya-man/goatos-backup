#!/usr/bin/env bash
# Pins every branch of land_route_decide (tools/ci/land-route.sh).
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=tools/ci/land-route.sh
. "$here/land-route.sh"
fails=0
check() { # expected, repo, env...
  local want="$1" repo="$2"; shift 2
  local got
  got="$(env -u GITHUB_ACTIONS -u GOATOS_LAND_TEST_MODE -u GOATOS_LAND_LOCAL \
    HOME=/Users/tester GOATOS_LAND_REQUIRED_ROOT= "$@" \
    bash -c ". '$here/land-route.sh'; land_route_decide '$repo'")"
  if [ "$got" = "$want" ]; then echo "ok   $want <- $repo $*"; else echo "FAIL want=$want got=$got <- $repo $*"; fails=$((fails+1)); fi
}
check queue  /Users/tester/mesha/goatos-wt-x
check queue  /Users/tester/mesha/goatos
check local  /Users/tester/mesha/goatos-wt-x GOATOS_LAND_LOCAL=1
check refuse /private/tmp/scratchpad/goatos-pr1
check refuse /private/tmp/scratchpad/goatos-pr1 GOATOS_LAND_LOCAL=1
check refuse /Users/tester/meshaX/goatos
check refuse /Users/tester/airnd/goatos
check local  /private/tmp/runner/_work/goatos GITHUB_ACTIONS=true
check local  /private/tmp/runner/_work/goatos GITHUB_ACTIONS=true GOATOS_LAND_LOCAL=0
check local  /tmp/goatos-land-main-test.abc/candidate GOATOS_LAND_TEST_MODE=1
check local  /opt/x GOATOS_LAND_REQUIRED_ROOT=/opt GOATOS_LAND_LOCAL=1
check queue  /opt/x GOATOS_LAND_REQUIRED_ROOT=/opt
[ "$fails" -eq 0 ] || { echo "land-route self-test: $fails failure(s)"; exit 1; }
echo "land-route self-test: all cases pass"
