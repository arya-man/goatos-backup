# shellcheck shell=bash
# gradle-run.sh — the one way developer/device scripts start Gradle.
#
#   goatos_gradlew "<label>" <android_dir> <gradle args...>
#
# 1. Shared Gradle home (gradle-home.sh): no private temp homes.
# 2. Machine-wide Gradle lock (gradle-worktree-lock.sh), the same lock
#    run-local-ci.sh's android job takes, so every session's Gradle builds run
#    one at a time. Waiting checks the holder pid is alive, prints the holder,
#    and never kills anything. The lock is fail-open: after the timeout
#    (GOATOS_CI_GRADLE_LOCK_TIMEOUT, default 1800 s) the build runs anyway.
# Needs JAVA_HOME already set to JDK 21 (tools/ci/java21.sh).

_GOATOS_GRADLE_RUN_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=tools/ci/gradle-home.sh
. "$_GOATOS_GRADLE_RUN_DIR/gradle-home.sh"
# shellcheck source=tools/ci/gradle-worktree-lock.sh
. "$_GOATOS_GRADLE_RUN_DIR/gradle-worktree-lock.sh"

_goatos_gradlew_exec() { # android_dir args...
  local dir="$1"; shift
  ( cd "$dir" && ./gradlew "$@" )
}

goatos_gradlew() {
  local label="$1" dir="$2"; shift 2
  gradle_home_normalize
  gradle_lock_run "$label" _goatos_gradlew_exec "$dir" "$@"
}
