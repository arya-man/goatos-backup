# shellcheck shell=bash
# android-screenshot-scope.sh — narrow Paparazzi only when the diff maps exactly
# to an owned screenshot allowlist. Unknown Android UI diffs return non-zero so
# callers fall back to the full :app:verifyPaparazziDevDebug proof.

android_screenshot_scope_feed_tests() {
  cat <<'EOF'
sg.mesha.goatos.ui.RoleChromeScreenshotTest.operator_feed
sg.mesha.goatos.ui.RoleChromeScreenshotTest.role_feed_director
sg.mesha.goatos.ui.ScreenshotTest.feed_direction_sheet
sg.mesha.goatos.ui.ScreenshotTest.feed_distribution_capture_reference
sg.mesha.goatos.ui.ScreenshotTest.feed_packing_worklist
sg.mesha.goatos.ui.ScreenshotTest.feed_transport_capture_matches_distribution_anatomy
sg.mesha.goatos.ui.ScreenshotTest.feed_transport_task_list_matches_distribution_anatomy
EOF
}

android_screenshot_scope_file_is_feed() { # path
  case "$1" in
    apps/goatos-android/feature/feature-feed/src/main/kotlin/sg/mesha/goatos/feature/feed/*.kt) return 0 ;;
    apps/goatos-android/app/src/test/snapshots/images/sg.mesha.goatos.ui_RoleChromeScreenshotTest_operator_feed_operator_feed.png) return 0 ;;
    apps/goatos-android/app/src/test/snapshots/images/sg.mesha.goatos.ui_RoleChromeScreenshotTest_role_feed_director_role_feed_director.png) return 0 ;;
    apps/goatos-android/app/src/test/snapshots/images/sg.mesha.goatos.ui_ScreenshotTest_feed_direction_sheet_feed_direction_sheet.png) return 0 ;;
    apps/goatos-android/app/src/test/snapshots/images/sg.mesha.goatos.ui_ScreenshotTest_feed_distribution_capture_reference_feed_distribution_capture_reference.png) return 0 ;;
    apps/goatos-android/app/src/test/snapshots/images/sg.mesha.goatos.ui_ScreenshotTest_feed_packing_worklist_feed_packing_worklist.png) return 0 ;;
    apps/goatos-android/app/src/test/snapshots/images/sg.mesha.goatos.ui_ScreenshotTest_feed_transport_capture_matches_distribution_anatomy_feed_transport_capture_matches_distribution_anatomy.png) return 0 ;;
    apps/goatos-android/app/src/test/snapshots/images/sg.mesha.goatos.ui_ScreenshotTest_feed_transport_task_list_matches_distribution_anatomy_feed_transport_task_list_matches_distribution_anatomy.png) return 0 ;;
    *) return 1 ;;
  esac
}

android_screenshot_scope_for_diff() {
  local saw_ui=0
  local f
  while IFS= read -r f; do
    [ -n "$f" ] || continue
    case "$f" in
      apps/goatos-android/app/src/test/snapshots/*|\
      apps/goatos-android/*/src/*/res/values*/*|\
      apps/goatos-android/*/src/*/res/drawable*/*|\
      apps/goatos-android/*/src/*/res/font*/*|\
      apps/goatos-android/*/src/*/res/mipmap*/*|\
      apps/goatos-android/app/src/main/kotlin/*.kt|\
      apps/goatos-android/app/src/debug/kotlin/*.kt|\
      apps/goatos-android/app/src/test/kotlin/*ScreenshotTest.kt|\
      apps/goatos-android/core/core-ui/*.kt|\
      apps/goatos-android/core/core-designsystem/*.kt|\
      apps/goatos-android/feature/feature-*/*.kt)
        if android_ui_diff_file_is_compose "$f" || [[ "$f" == apps/goatos-android/app/src/test/snapshots/* ]] || [[ "$f" == apps/goatos-android/*/src/*/res/* ]]; then
          saw_ui=1
          android_screenshot_scope_file_is_feed "$f" || return 1
        fi
        ;;
    esac
  done < <(changed_since_base 2>/dev/null | sort -u)
  [ "$saw_ui" = "1" ] || return 1
  android_screenshot_scope_feed_tests
}

android_screenshot_gradle_filter_args() {
  local tests
  tests="$(android_screenshot_scope_for_diff)" || return 1
  local args=()
  local test
  while IFS= read -r test; do
    [ -n "$test" ] || continue
    args+=(--tests "$test")
  done <<<"$tests"
  printf '%q ' "${args[@]}"
}
