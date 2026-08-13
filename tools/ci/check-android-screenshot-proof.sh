#!/usr/bin/env bash
# check-android-screenshot-proof.sh — anti-narrowing guard for the Android
# Paparazzi screenshot proof.
#
# This lives OUTSIDE tools/ci/run-local-ci.sh on purpose. The previous in-file
# version grepped the very script it was defined in, so its own function body
# satisfied the `:app:verifyPaparazziDevDebug` check and the guard was inert.
#
# Intent: the Paparazzi proof is opt-in by default, but WHEN it runs it must
# run either the FULL `:app:verifyPaparazziDevDebug` task or the guarded
# diff-mapped targeted wrapper. Raw ad-hoc `--tests` narrowing stays forbidden.
set -uo pipefail

TARGET="${1:-tools/ci/run-local-ci.sh}"
code() { grep -vE '^[[:space:]]*#' "$TARGET"; }
rc=0
fail() { echo "!! android screenshot proof guard: $*" >&2; rc=1; }

screenshot_dir="apps/goatos-android/app/src/test/kotlin/sg/mesha/goatos/ui"
n="$(find "$screenshot_dir" -name '*ScreenshotTest.kt' -type f 2>/dev/null | wc -l | tr -d ' ')"
[ "${n:-0}" -gt 0 ] || fail "no Paparazzi screenshot test classes under $screenshot_dir"

# There are legitimate full and targeted invocation sites, both opt-in. EVERY
# one of them must carry the Paparazzi task. Zero invocation lines means the
# proof became unreachable.
inv_lines="$(code | grep -E '^[[:space:]]*(optional_)?step(_cached)? "android screenshots( \\(targeted\\))?"')"
inv="$(printf '%s' "$inv_lines" | grep -c . || true)"
if [ "${inv:-0}" -lt 1 ]; then
  fail "no 'step \"android screenshots\"' invocation found (rename/hoist blinds this guard)"
else
  full="$(printf '%s\n' "$inv_lines" | grep -Fc ':app:verifyPaparazziDevDebug' || true)"
  [ "$full" = "$inv" ] \
    || fail "every screenshot step must invoke the full :app:verifyPaparazziDevDebug ($full/$inv do)"
  raw_narrowed="$(printf '%s\n' "$inv_lines" | grep -- '--tests' | grep -Fv 'android_screenshot_gradle_filter_args' || true)"
  [ -z "$raw_narrowed" ] \
    || fail "local CI narrows Paparazzi with raw --tests; targeted narrowing must come only from android_screenshot_gradle_filter_args"
fi

# The proof must remain REACHABLE on demand — asserted by execution, not by
# string presence. The old check was `grep -Fq 'GOATOS_RUN_ANDROID_SCREENSHOTS'`,
# which was satisfied by the flag name appearing inside the SKIP banner's `echo`.
# Deleting the `1|true|TRUE|True)` arm that actually runs Paparazzi left that
# check green. So instead drive the target in GOATOS_CI_TRACE_ONLY mode (step
# records the command and executes nothing, and the run exits 3 without writing a
# receipt) and assert the branch is entered with the opt-in ON and not with it OFF.
# `off` UNSETS the opt-in rather than setting it to 0, so that flipping the
# `${GOATOS_RUN_ANDROID_SCREENSHOTS:-0}` default to :-1 is also caught.
#
# BOTH arms are probed. run_android has TWO Paparazzi call sites — the normal
# landing arm and the GOATOS_FAST_LOCAL_CI=1 developer-loop arm — and probing
# only the normal one left the FAST arm deletable with this guard green.
# GOATOS_FAST_LOCAL_CI is a pre-existing mode selector, not a new bypass: the
# probe drives the SAME opt-in (GOATOS_RUN_ANDROID_SCREENSHOTS) inside it.
trace() { # <on|off> <fast:0|1> -> traced step lines
  if [ "$1" = on ]; then
    env GOATOS_CI_TRACE_ONLY=1 GOATOS_FAST_LOCAL_CI="$2" GOATOS_RUN_ANDROID_SCREENSHOTS=1 \
      bash "$TARGET" android 2>/dev/null | grep '^CI-TRACE ' || true
  else
    env -u GOATOS_RUN_ANDROID_SCREENSHOTS GOATOS_CI_TRACE_ONLY=1 GOATOS_FAST_LOCAL_CI="$2" \
      bash "$TARGET" android 2>/dev/null | grep '^CI-TRACE ' || true
  fi
}
trace_on="$(trace on 0)"
trace_off="$(trace off 0)"
fast_on="$(trace on 1)"
fast_off="$(trace off 1)"

printf '%s\n' "$trace_on" | grep -q 'CI-TRACE android :app ' \
  || fail "trace mode did not reach the android job at all; this probe is blind (check GOATOS_CI_TRACE_ONLY wiring in $TARGET)"
printf '%s\n' "$trace_on" | grep -Eq 'CI-TRACE android screenshots( \(targeted\))? ::.*:app:verifyPaparazziDevDebug' \
  || fail "with GOATOS_RUN_ANDROID_SCREENSHOTS=1 the Paparazzi proof is UNREACHABLE (no traced :app:verifyPaparazziDevDebug); a banner mentioning the flag is not a run branch"
printf '%s\n' "$trace_off" | grep -q 'CI-TRACE android screenshots ::' \
  && fail "with GOATOS_RUN_ANDROID_SCREENSHOTS=0 the screenshot proof ran anyway; the opt-in default is broken"

# Same three assertions again, inside the FAST developer lane. The first one is
# the anti-blindness anchor: if the FAST arm stops being entered at all, the
# other two would pass vacuously.
printf '%s\n' "$fast_on" | grep -q 'CI-TRACE android fast compile/unit/lint ::' \
  || fail "GOATOS_FAST_LOCAL_CI=1 did not reach the FAST android arm; the FAST half of this probe is blind"
printf '%s\n' "$fast_on" | grep -Eq 'CI-TRACE android screenshots( \(targeted\))? ::.*:app:verifyPaparazziDevDebug' \
  || fail "in the FAST lane with GOATOS_RUN_ANDROID_SCREENSHOTS=1 the Paparazzi proof is UNREACHABLE; the FAST arm was deleted or narrowed"
printf '%s\n' "$fast_off" | grep -q 'CI-TRACE android screenshots ::' \
  && fail "in the FAST lane with GOATOS_RUN_ANDROID_SCREENSHOTS unset the screenshot proof ran anyway; the opt-in default is broken"

# The receipt hole must not return.
code | grep -Fq 'GOATOS_SKIP_ANDROID_SCREENSHOTS' \
  && fail "GOATOS_SKIP_ANDROID_SCREENSHOTS is a receipt hole; it was removed deliberately"

exit $rc
