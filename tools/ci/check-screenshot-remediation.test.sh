#!/usr/bin/env bash
# check-screenshot-remediation.test.sh — adversarial self-test.
#
# A guard that only ever runs against a healthy repo is a guard nobody has seen
# fail. Each case below builds a fixture repo carrying the real tools/ci and a
# deliberately broken remediation wiring, and asserts the guard goes RED for the
# right reason. Case (a) is the exact defect that shipped.
set -uo pipefail

repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
GUARD="$repo/tools/ci/check-screenshot-remediation.sh"
rc=0
FIXTURES=()
cleanup() { for f in "${FIXTURES[@]:-}"; do rm -rf "$f"; done; }
trap cleanup EXIT

# fixture <screenshots-target-args> -> path to a repo the guard can be pointed at
fixture() {
  local dir; dir="$(mktemp -d)"; FIXTURES+=("$dir")
  mkdir -p "$dir/tools"
  cp -R "$repo/tools/ci" "$dir/tools/ci"
  {
    printf '.PHONY: ci-local-screenshots\n'
    printf 'ci-local-screenshots:\n'
    printf '\tGOATOS_RUN_ANDROID_SCREENSHOTS=1 bash tools/ci/run-local-ci.sh %s\n' "$1"
  } >"$dir/Makefile"
  echo "$dir"
}

expect() { # <case> <expected rc> <substring> <repo>
  local name="$1" want="$2" needle="$3" target="$4" out got
  out="$(bash "$GUARD" "$target" 2>&1)"; got=$?
  if [ "$got" -ne "$want" ]; then
    echo "!! $name: expected exit $want, got $got" >&2
    printf '%s\n' "$out" >&2
    rc=1
    return
  fi
  if [ -n "$needle" ] && ! printf '%s' "$out" | grep -qF "$needle"; then
    echo "!! $name: exit was $got but the message did not mention '$needle'" >&2
    printf '%s\n' "$out" >&2
    rc=1
    return
  fi
  echo "ok  $name"
}

# (a) THE SHIPPED DEFECT: the remediation runs an explicit partial job, which
#     writes no receipt, so following the block message can never clear it.
expect "(a) partial 'android' remediation is REJECTED" 1 "left the receipt UNTOUCHED" "$(fixture android)"

# (b) a remediation that is a complete auto-scoped run with screenshots ON is
#     accepted — the whole point of (a) is that a real fix exists.
expect "(b) auto-scoped remediation is ACCEPTED" 0 "" "$(fixture '')"

# (c) MODE=all is equally valid: still complete, still screenshot-covering.
expect "(c) forced-full remediation is ACCEPTED" 0 "" "$(fixture all)"

# (d) a remediation that runs a complete gate but WITHOUT screenshots re-records
#     the very gap the block is about; it must not be accepted as remediation.
d="$(mktemp -d)"; FIXTURES+=("$d")
mkdir -p "$d/tools"; cp -R "$repo/tools/ci" "$d/tools/ci"
{
  printf '.PHONY: ci-local-screenshots\n'
  printf 'ci-local-screenshots:\n'
  printf '\tbash tools/ci/run-local-ci.sh\n'
} >"$d/Makefile"
expect "(d) full run WITHOUT the screenshot opt-in is REJECTED" 1 "not re-record the gap" "$d"

# (e) the live repo itself must pass.
expect "(e) this checkout PASSES" 0 "" "$repo"

exit $rc
