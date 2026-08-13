# shellcheck shell=bash
# android-ui-diff.sh — does this diff change what an Android screenshot would show?
#
# Sourced by tools/ci/run-local-ci.sh. It lives in its own file ONLY so that
# tools/ci/check-android-ui-diff.test.sh can source it and drive it with a
# stubbed `changed_since_base`, i.e. so the detector cannot silently regress.
#
# WHY NOT A PATH REGEX. The previous detector was
#   '^apps/goatos-android/.*(/ui/|/snapshots/)'
# which missed ~37 of ~63 @Composable files: every feature/feature-*/ screen,
# core/core-designsystem/, and app/src/main/kotlin/.../feature/weighing/ have no
# `/ui/` segment. A path pattern wide enough to cover them (app/src/main/kotlin/**,
# feature/**) also sweeps in ViewModels, DI modules, repositories and network DTOs
# that no golden covers. The precise discriminator is not the path, it is whether
# the changed file is a Compose file. So: scope by path, then discriminate by
# CONTENT (@Composable). Goldens and Compose-visible resources are UI
# unconditionally.
#
# KNOWN BLIND SPOTS (documented on purpose, per AGENTS.md):
#   * The @Composable check reads the WORKTREE file, not the diff hunk, so a
#     non-UI edit inside a Compose file counts as UI. Over-detection is the safe
#     direction: it yields screenshots="skipped-with-ui-diff", i.e. "prove it".
#   * For a deleted/renamed-away file the base blob is consulted; if the base ref
#     cannot be resolved this silently yields "not UI". Deleting a whole screen
#     normally also deletes its golden, which app/src/test/snapshots/* catches
#     unconditionally.
#   * core/core-testing, device/ and benchmark/ are deliberately out of scope. If
#     a Paparazzi fixture helper moves into core-testing, add it to the scope list.

# Base ref for the deleted/renamed-away lookup below. When sourced by
# run-local-ci.sh this DELEGATES to that script's single resolver, so the
# detector can never diff against a different base than the one the run records
# on its push receipt. Standalone (the test harness sources this file alone) it
# keeps its own resolution.
android_ui_diff_base_ref() {
  if declare -f resolve_ci_base >/dev/null 2>&1; then
    resolve_ci_base
    printf '%s' "$ci_base_ref"
    return 0
  fi
  local base_ref="${GOATOS_CI_BASE:-origin/main}"
  if ! git rev-parse --verify "${base_ref}^{commit}" >/dev/null 2>&1; then
    base_ref="HEAD~1"
  fi
  printf '%s' "$base_ref"
}

android_ui_diff_file_is_compose() { # path
  local f="$1"
  if [ -f "$f" ]; then
    grep -q '@Composable' "$f" && return 0
    return 1
  fi
  local base
  base="$(android_ui_diff_base_ref)"
  git show "${base}:${f}" 2>/dev/null | grep -q '@Composable'
}

# android_ui_diff_detected — 0 (true) when the diff could change a screenshot.
# Reads the file list from `changed_since_base`, which the caller must define.
android_ui_diff_detected() {
  local f
  while IFS= read -r f; do
    [ -n "$f" ] || continue
    case "$f" in
      apps/goatos-android/app/src/test/snapshots/*) return 0 ;;      # a golden itself
      apps/goatos-android/*/src/*/res/values*/*|\
      apps/goatos-android/*/src/*/res/drawable*/*|\
      apps/goatos-android/*/src/*/res/font*/*|\
      apps/goatos-android/*/src/*/res/mipmap*/*) return 0 ;;         # Compose-visible resources
      apps/goatos-android/app/src/main/kotlin/*.kt|\
      apps/goatos-android/app/src/debug/kotlin/*.kt|\
      apps/goatos-android/app/src/test/kotlin/*ScreenshotTest.kt|\
      apps/goatos-android/core/core-ui/*.kt|\
      apps/goatos-android/core/core-designsystem/*.kt|\
      apps/goatos-android/feature/feature-*/*.kt) ;;                 # in scope — fall through
      *) continue ;;                                                 # not android UI scope
    esac
    android_ui_diff_file_is_compose "$f" && return 0
  done < <(changed_since_base 2>/dev/null | sort -u)
  return 1
}
