#!/usr/bin/env bash
# java21.sh — pick a JDK 21 for Gradle, or say why there is none.
#
# Why: run-local-ci.sh used `${JAVA_HOME:-openjdk@21}` with no version check, so
# a JAVA_HOME inherited from Android Studio (jbr-17) ran the Gradle daemon on
# Java 17. That daemon cannot be reused by a JDK 21 build, so each machine ended
# up with extra daemons (measured 2026-09-25: 4 idle daemons, ~6.4 GB, one on 17).
#
# Same rule as tools/dev/android-env.sh: an inherited JAVA_HOME is kept only if
# it is major 21; otherwise the known JDK 21 locations are tried in order.
# Sourced; defines java21_major and java21_resolve. Nothing runs on source.

java21_major() {
  # $1 = JDK home. Prints the major version, or nothing.
  [ -n "${1:-}" ] && [ -x "$1/bin/java" ] || return 0
  "$1/bin/java" -version 2>&1 | sed -n '1s/.*version "\([0-9][0-9]*\).*/\1/p'
}

java21_candidates() {
  # Overridable so the test can run without real JDKs.
  if [ -n "${GOATOS_JAVA21_CANDIDATES:-}" ]; then
    printf '%s\n' $GOATOS_JAVA21_CANDIDATES
    return 0
  fi
  # Homebrew first. /usr/libexec/java_home goes LAST: on a Mac where only an
  # old jbr-17 is registered, `java_home -v 21` can still print that 17 path.
  # Every candidate is version-checked, so a wrong answer is skipped.
  printf '%s\n' \
    /opt/homebrew/opt/openjdk@21 \
    /usr/local/opt/openjdk@21 \
    /opt/homebrew/opt/openjdk@21/libexec/openjdk.jdk/Contents/Home \
    /usr/lib/jvm/java-21-openjdk-amd64 \
    /usr/lib/jvm/java-21-openjdk-arm64
  [ -x /usr/libexec/java_home ] && /usr/libexec/java_home -v 21 2>/dev/null || true
}

java21_resolve() {
  # $1 = inherited JAVA_HOME (may be empty). Prints a JDK 21 home and returns 0,
  # or prints a reason on stderr and returns 1. Never guesses a non-21 JDK.
  local inherited="${1:-}" major candidate
  major="$(java21_major "$inherited")"
  if [ "$major" = "21" ]; then
    printf '%s\n' "$inherited"
    return 0
  fi
  if [ -n "$inherited" ]; then
    echo "java21: inherited JAVA_HOME=$inherited is Java ${major:-unknown}, not 21; looking for JDK 21" >&2
  fi
  while IFS= read -r candidate; do
    [ -n "$candidate" ] || continue
    if [ "$(java21_major "$candidate")" = "21" ]; then
      printf '%s\n' "$candidate"
      return 0
    fi
  done < <(java21_candidates)
  echo "java21: no JDK 21 found (inherited JAVA_HOME=${inherited:-unset}). macOS: brew install openjdk@21" >&2
  return 1
}

java21_export_or_die() {
  # For Gradle entrypoints: export a verified JDK 21 as JAVA_HOME (and first on
  # PATH), or exit before Gradle starts. Never keeps a non-21 inherited JDK.
  local jdk
  jdk="$(java21_resolve "${JAVA_HOME:-}")" || { echo "ERROR: Gradle needs JDK 21; refusing to start." >&2; exit 1; }
  export JAVA_HOME="$jdk"
  export PATH="$JAVA_HOME/bin:$PATH"
}
