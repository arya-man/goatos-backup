#!/usr/bin/env bash
# Negative self-test for check-android-screenshot-proof.sh.
#
# Without this you cannot distinguish "the guard passes" from "the guard is
# inert" — which is exactly the state the previous in-file guard was in.
set -uo pipefail

repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repo"
guard="tools/ci/check-android-screenshot-proof.sh"
src="tools/ci/run-local-ci.sh"
# Fixtures MUST live inside tools/ci: the guard now drives the target by
# EXECUTION (GOATOS_CI_TRACE_ONLY), and run-local-ci.sh resolves the repo root
# and sources its siblings relative to its own path. A copy in /tmp would resolve
# the wrong repo. These are removed on exit.
# Fixture copies are written FLAT into tools/ci (not /tmp, not a subdir):
# run-local-ci.sh resolves the repo root as <its own dir>/../.. and sources its
# siblings relative to its own path, so only a sibling copy resolves correctly.
# All fixtures are dot-prefixed and removed on exit.
tmp="tools/ci"
fixture_prefix="tools/ci/.screenshot-guard-selftest-"
cleanup() { rm -f "${fixture_prefix}"*.sh; }
trap cleanup EXIT
cleanup

rc=0
expect() { # expected_status, label, file
  bash "$guard" "$3" >/dev/null 2>&1
  local got=$?
  if [ "$got" != "$1" ]; then
    echo "!! screenshot guard self-test FAILED: $2 (expected exit $1, got $got)" >&2
    rc=1
  fi
}

mk() { # <name> ; reads a transform from stdin-less sed/grep pipeline via "$@"
  local out="${fixture_prefix}$1.sh"; shift
  "$@" <"$src" >"$out"
  printf '%s' "$out"
}

# (d) unmodified -> pass
f="$(mk ok cat)"
expect 0 "unmodified run-local-ci.sh should pass" "$f"

# (a) delete every step line -> fail
f="$(mk deleted grep -v 'step "android screenshots"')"
expect 1 "removing the screenshot step must fail" "$f"

# (b) narrow with --tests -> fail
f="$(mk narrowed sed 's/:app:verifyPaparazziDevDebug/:app:verifyPaparazziDevDebug --tests Foo/')"
expect 1 "narrowing with --tests must fail" "$f"

# (c) an invocation line that lost the full task -> fail
f="$(mk renamed sed 's/:app:verifyPaparazziDevDebug/:app:somethingElse/')"
expect 1 "dropping :app:verifyPaparazziDevDebug must fail" "$f"

# the removed receipt hole must not come back
f="$(mk hole sed 's/GOATOS_RUN_ANDROID_SCREENSHOTS/GOATOS_SKIP_ANDROID_SCREENSHOTS/')"
expect 1 "reintroducing GOATOS_SKIP_ANDROID_SCREENSHOTS must fail" "$f"

# (e) THE BANNER HOLE. Keep every string, keep the `step "android screenshots"`
#     line, but make the enabling case arm unmatchable. The old presence-grep
#     ('GOATOS_RUN_ANDROID_SCREENSHOTS' appears in the SKIP banner echo) passed
#     this; the reachability probe must not.
#     SCOPED ON PURPOSE. The earlier `sed 's/^\( *\)1|true|TRUE|True)/.../'`
#     rewrote EVERY such arm in the file, including `ci_trace_only`'s own — so
#     the fixture disabled trace mode, launched a REAL Android job (162s of
#     Gradle), and went red because the probe was blind rather than because the
#     screenshot branch was unreachable. Red for the wrong reason is the rot this
#     suite exists to catch. This awk neutralises only the arms that belong to a
#     GOATOS_RUN_ANDROID_SCREENSHOTS case — both of them, FAST and normal.
f="$(mk unreachable awk '/GOATOS_RUN_ANDROID_SCREENSHOTS:-0/{arm=1} arm && /1\|true\|TRUE\|True\)/{sub(/1\|true\|TRUE\|True\)/,"__goatos_never_matches__)"); arm=0} {print}')"
expect 1 "an unreachable screenshot branch must fail even with the banner intact" "$f"

# (f) inverted default: the proof running when the opt-in is OFF is also a bug.
f="$(mk inverted sed 's/${GOATOS_RUN_ANDROID_SCREENSHOTS:-0}/${GOATOS_RUN_ANDROID_SCREENSHOTS:-1}/')"
expect 1 "flipping the opt-in default to ON must fail" "$f"

# (h) THE FAST-ARM HOLE. There are TWO Paparazzi call sites. Delete ONLY the
#     FAST (GOATOS_FAST_LOCAL_CI=1) one — the normal arm, every string, and the
#     invocation-count check all stay satisfied — and a probe that only drives
#     the normal lane stays green. The FAST arm is the one WITHOUT --no-daemon.
f="$(mk fastarm awk '!(/step "android screenshots"/ && !/--no-daemon/)')"
expect 1 "deleting only the FAST-lane screenshot arm must fail" "$f"

# (g) trace mode must never look green: nothing executed, exit 3, NO receipt.
receipt="$(git rev-parse --git-path goatos-ci-local-receipt.json)"
before="$( [ -e "$receipt" ] && md5 -q "$receipt" 2>/dev/null || shasum "$receipt" 2>/dev/null || echo absent )"
GOATOS_CI_TRACE_ONLY=1 GOATOS_RUN_ANDROID_SCREENSHOTS=1 bash "$src" android >/dev/null 2>&1
got=$?
[ "$got" = "3" ] || { echo "!! trace mode must exit 3, never 0 (got $got)" >&2; rc=1; }
after="$( [ -e "$receipt" ] && md5 -q "$receipt" 2>/dev/null || shasum "$receipt" 2>/dev/null || echo absent )"
[ "$before" = "$after" ] || { echo "!! trace mode mutated the CI receipt" >&2; rc=1; }
# and the recorder itself refuses under the trace variable.
#
# DISCRIMINATION NOTE: the argv below MUST be otherwise-valid. The earlier
# version omitted --base, so `record()` exited 2 on the missing-base check and
# this case stayed green with the GOATOS_CI_TRACE_ONLY block deleted entirely —
# red for an incidental reason, which is the exact rot this suite exists to
# catch. It is now proven both ways: identical argv exits 2 WITH the variable and
# 0 WITHOUT it, so only the trace lock can explain the difference.
trace_sha="$(printf 'a%.0s' $(seq 40))"
trace_base="$(printf 'b%.0s' $(seq 40))"
trace_receipt="$(git rev-parse --git-path goatos-ci-local-receipt.json)"
trace_saved=""
[ -e "$trace_receipt" ] && trace_saved="$(cat "$trace_receipt")"
GOATOS_CI_TRACE_ONLY=1 node tools/ci/check-local-ci-evidence.mjs \
  --record "$trace_sha" --base "$trace_base" --mode all --jobs common --screenshots skipped >/dev/null 2>&1
[ $? -eq 2 ] || { echo "!! check-local-ci-evidence.mjs must refuse --record under GOATOS_CI_TRACE_ONLY" >&2; rc=1; }
env -u GOATOS_CI_TRACE_ONLY node tools/ci/check-local-ci-evidence.mjs \
  --record "$trace_sha" --base "$trace_base" --mode all --jobs common --screenshots skipped >/dev/null 2>&1
[ $? -eq 0 ] || { echo "!! the trace-lock probe is not discriminating: the SAME argv already fails without GOATOS_CI_TRACE_ONLY, so the exit-2 above proves nothing about the trace lock" >&2; rc=1; }
if [ -n "$trace_saved" ]; then printf '%s' "$trace_saved" >"$trace_receipt"; else rm -f "$trace_receipt"; fi

[ "$rc" = "0" ] && echo "android screenshot proof guard: self-test passed"
exit $rc
