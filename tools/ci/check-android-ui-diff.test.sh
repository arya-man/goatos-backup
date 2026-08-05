#!/usr/bin/env bash
# check-android-ui-diff.test.sh — self-test for the Android UI-diff detector.
#
# The detector decides whether a ci-local run records screenshots="skipped" or
# the blocking "skipped-with-ui-diff". Without this test you cannot distinguish
# "no UI diff" from "the detector is blind" — which is exactly the state the old
# '^apps/goatos-android/.*(/ui/|/snapshots/)' regex was in for every
# feature/feature-*/ screen.
set -uo pipefail

repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repo"
# shellcheck source=tools/ci/android-ui-diff.sh
. tools/ci/android-ui-diff.sh

rc=0
FILES=""
changed_since_base() { printf '%s\n' $FILES; }

# The content probe must be deterministic in this test: assert path scoping and
# the Compose discriminator separately from the state of the working tree.
COMPOSE_FILES=""
android_ui_diff_file_is_compose() {
  case " $COMPOSE_FILES " in *" $1 "*) return 0 ;; esac
  return 1
}

expect() { # expected(0=UI|1=not), label, files..., with COMPOSE_FILES preset
  local want="$1" label="$2"; shift 2
  FILES="$*"
  android_ui_diff_detected
  local got=$?
  if [ "$got" != "$want" ]; then
    echo "!! android ui-diff detector: FAILED — $label (expected $want, got $got)" >&2
    rc=1
  fi
}

a=apps/goatos-android

# ── positive: things a screenshot would show ───────────────────────────────
COMPOSE_FILES="$a/feature/feature-counts/src/main/kotlin/sg/mesha/goatos/feature/counts/CountsScreen.kt"
expect 0 "feature-* Compose screen (MISSED by the old regex)" "$COMPOSE_FILES"

COMPOSE_FILES="$a/core/core-designsystem/src/main/kotlin/sg/mesha/goatos/core/designsystem/MeshaTheme.kt"
expect 0 "core-designsystem Compose file (MISSED by the old regex)" "$COMPOSE_FILES"

COMPOSE_FILES="$a/app/src/main/kotlin/sg/mesha/goatos/feature/weighing/WeighingScreen.kt"
expect 0 "app feature/weighing Compose screen (MISSED by the old regex)" "$COMPOSE_FILES"

COMPOSE_FILES="$a/core/core-ui/src/main/kotlin/sg/mesha/goatos/core/ui/SyncStatusIndicator.kt"
expect 0 "core-ui Compose file" "$COMPOSE_FILES"

COMPOSE_FILES=""
expect 0 "a Paparazzi golden is UI unconditionally" "$a/app/src/test/snapshots/images/foo.png"
expect 0 "res/values is UI unconditionally" "$a/app/src/main/res/values/strings.xml"
expect 0 "res/drawable is UI unconditionally" "$a/core/core-ui/src/main/res/drawable/ic_x.xml"

COMPOSE_FILES="$a/feature/feature-feed/src/main/kotlin/X.kt"
expect 0 "a UI file mixed into a larger diff still trips the detector" \
  backend/internal/foo.go docs/readme.md "$COMPOSE_FILES"

# ── negative: things no golden covers ──────────────────────────────────────
COMPOSE_FILES=""
expect 1 "in-scope but non-Compose (ViewModel) is not UI" \
  "$a/feature/feature-counts/src/main/kotlin/sg/mesha/goatos/feature/counts/CountsViewModel.kt"
expect 1 "core-network is out of scope" "$a/core/core-network/src/main/kotlin/Api.kt"
expect 1 "backend/docs are out of scope" backend/internal/foo.go docs/runbooks/x.md
expect 1 "android gradle files alone are not a UI diff" "$a/app/build.gradle.kts"
expect 1 "an empty diff is not a UI diff" ""

# ── regression pin: the old path-only regex was blind to feature-* ─────────
old_regex_hits=0
printf '%s\n' \
  "$a/feature/feature-counts/src/main/kotlin/sg/mesha/goatos/feature/counts/CountsScreen.kt" \
  | grep -Eq '^apps/goatos-android/.*(/ui/|/snapshots/)' && old_regex_hits=1
[ "$old_regex_hits" = "0" ] || { echo "!! the old regex unexpectedly matched; update this pin" >&2; rc=1; }

# ── the REAL content probe (unstubbed) against real repo files ────────────
# The cases above stub android_ui_diff_file_is_compose to test path scoping in
# isolation. This block proves the real probe agrees with `grep -l @Composable`.
real_probe() { # path -> 0 if the real probe says Compose
  ( . tools/ci/android-ui-diff.sh; android_ui_diff_file_is_compose "$1" )
}
while IFS= read -r f; do
  [ -n "$f" ] || continue
  real_probe "$f" || { echo "!! real probe missed a @Composable file: $f" >&2; rc=1; }
  break
done < <(grep -rl '@Composable' apps/goatos-android/feature 2>/dev/null | head -1)
while IFS= read -r f; do
  [ -n "$f" ] || continue
  real_probe "$f" && { echo "!! real probe wrongly called a non-Compose file UI: $f" >&2; rc=1; }
  break
done < <(printf '%s\n' apps/goatos-android/settings.gradle.kts)

[ "$rc" = "0" ] && echo "android ui-diff detector: self-test passed"
exit $rc
