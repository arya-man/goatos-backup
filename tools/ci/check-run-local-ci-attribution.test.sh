#!/usr/bin/env bash
# check-run-local-ci-attribution.test.sh — the RED re-run hint must point at a
# lane that can actually re-run the failure.
#
# run_android_guards is shared by the `android` job (which builds :app with
# Gradle) and the Gradle-free `guardrails` lane. It used to hardcode
# current_job="android", so a mobile-guard failure under `guardrails` printed
# "GOATOS_FAST_LOCAL_CI=1 tools/ci/run-local-ci.sh android" — routing the user
# into a full :app compile they never asked for.
set -uo pipefail

repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repo"
src="tools/ci/run-local-ci.sh"
rc=0

# Fixture must be a SIBLING of run-local-ci.sh: it resolves the repo root as
# <its dir>/../.. and sources tools/ci/android-ui-diff.sh relative to itself.
fx="tools/ci/.attribution-selftest.sh"
trap 'rm -f "$fx"' EXIT

# Stub every guard `make` target to succeed, except mobile-guard which fails.
# The stubs are injected just above run_job() so they override the real
# definitions. Nothing real is executed.
awk '
  /^run_job\(\) \{/ && !done {
    print "run_common()      { current_job=common; step \"stub common\" true; }";
    print "run_backend()     { current_job=backend; step \"stub backend\" true; }";
    # Replace the Gradle half of run_android with nothing: this test is about
    # attribution, and it must never start a Gradle build.
    print "run_android()     { current_job=android; run_android_guards; }";
    print "make() { case \"$1\" in mobile-guard) return 1 ;; *) return 0 ;; esac; }";
    done = 1
  } { print }
' "$src" >"$fx"

hint_for() { # <lane>
  bash "$fx" "$1" 2>&1 | sed -n 's/^  GOATOS_FAST_LOCAL_CI=1 tools\/ci\/run-local-ci.sh //p'
}

g="$(hint_for guardrails)"
a="$(hint_for android)"

case "$g" in
  guardrails) echo "  ok   — a guardrails-lane failure re-runs as 'guardrails'" ;;
  *) echo "!! guardrails failure hint was '${g}', expected 'guardrails'" >&2; rc=1 ;;
esac
case "$g" in
  *android*) echo "!! the Gradle-free guardrails lane routed the user into the android (Gradle) lane" >&2; rc=1 ;;
  *) echo "  ok   — the guardrails hint never mentions the Gradle lane" ;;
esac
case "$a" in
  android) echo "  ok   — the same guard failing under 'android' still re-runs as 'android'" ;;
  *) echo "!! android failure hint was '${a}', expected 'android'" >&2; rc=1 ;;
esac

# The unattributed-caller assertion must be live: calling run_android_guards with
# a foreign current_job has to fail loudly rather than inherit a stale job name.
fn="tools/ci/.attribution-selftest-fn.sh"
trap 'rm -f "$fx" "$fn"' EXIT
awk '/^run_android_guards\(\) \{/,/^\}/' "$src" >"$fn"
out="$(bash -c '
  set -uo pipefail
  . "$1"
  step() { :; }
  current_job="bogus"; fail=0
  run_android_guards 2>&1 >/dev/null
  echo "fail=$fail"
' _ "$fn" 2>&1)"
case "$out" in
  *"unattributed job 'bogus'"*fail=1*) echo "  ok   — an unattributed caller fails loudly" ;;
  *) echo "!! run_android_guards accepted an unattributed current_job: ${out}" >&2; rc=1 ;;
esac

[ "$rc" = "0" ] && echo "run-local-ci attribution: self-test passed"
exit $rc
