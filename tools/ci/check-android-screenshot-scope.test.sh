#!/usr/bin/env bash
set -uo pipefail

repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repo"
# shellcheck source=tools/ci/android-ui-diff.sh
. tools/ci/android-ui-diff.sh
# shellcheck source=tools/ci/android-screenshot-scope.sh
. tools/ci/android-screenshot-scope.sh

rc=0
changed_fixture=""

changed_since_base() {
  printf '%s\n' "$changed_fixture"
}

android_ui_diff_file_is_compose() {
  case "$1" in
    *FeedDistributionCompleteScreen.kt|*UnknownScreen.kt) return 0 ;;
    *) return 1 ;;
  esac
}

expect_feed() {
  local label="$1"
  changed_fixture="$2"
  local out
  out="$(android_screenshot_scope_for_diff 2>/dev/null)" || {
    echo "!! android screenshot scope self-test: $label should map to feed tests" >&2
    rc=1
    return
  }
  printf '%s\n' "$out" | grep -q 'sg.mesha.goatos.ui.ScreenshotTest.feed_distribution_capture_reference' || {
    echo "!! android screenshot scope self-test: $label missing feed distribution screenshot" >&2
    rc=1
  }
  printf '%s\n' "$out" | grep -q 'sg.mesha.goatos.ui.RoleChromeScreenshotTest.operator_feed' || {
    echo "!! android screenshot scope self-test: $label missing feed role chrome screenshot" >&2
    rc=1
  }
}

expect_full() {
  local label="$1"
  changed_fixture="$2"
  if android_screenshot_scope_for_diff >/dev/null 2>&1; then
    echo "!! android screenshot scope self-test: $label should fall back to full Paparazzi" >&2
    rc=1
  fi
}

expect_feed "feed composable source" "apps/goatos-android/feature/feature-feed/src/main/kotlin/sg/mesha/goatos/feature/feed/FeedDistributionCompleteScreen.kt"
expect_feed "feed golden" "apps/goatos-android/app/src/test/snapshots/images/sg.mesha.goatos.ui_ScreenshotTest_feed_packing_worklist_feed_packing_worklist.png"
expect_full "unknown composable source" "apps/goatos-android/feature/feature-health/src/main/kotlin/sg/mesha/goatos/feature/health/UnknownScreen.kt"
expect_full "shared drawable resource" "apps/goatos-android/app/src/main/res/drawable/icon.xml"
expect_full "non-ui android code" "apps/goatos-android/core/core-analytics/src/main/kotlin/sg/mesha/goatos/core/analytics/BackendAnalyticsAdapter.kt"

[ "$rc" = "0" ] && echo "android screenshot scope: self-test passed"
exit "$rc"
