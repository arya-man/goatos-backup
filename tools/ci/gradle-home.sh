# shellcheck shell=bash
# gradle-home.sh — keep every Goat OS Gradle run on the ONE shared Gradle home.
#
# Why: measured 2026-09-25, a session had GRADLE_USER_HOME inside its scratchpad
# (/private/tmp/...). It started with a cold cache, ran its own extra daemon
# (daemons are per Gradle home), and escaped the machine-wide Gradle lock, which
# is keyed on the Gradle home. On a 32 GB Mac already 20 GB into swap that is
# the wrong trade.
#
# gradle_home_normalize: if GRADLE_USER_HOME points into a temp dir (/tmp,
# /private/tmp, /var/folders, /private/var/folders), warn and reset it to
# $HOME/.gradle. GOATOS_ALLOW_PRIVATE_GRADLE_HOME=1 keeps it (for a deliberate
# throwaway run). Any other explicit home is left alone. Never fails.

gradle_home_is_temp() {
  case "${1:-}" in
    /tmp|/tmp/*|/private/tmp|/private/tmp/*|/var/folders/*|/private/var/folders/*) return 0 ;;
  esac
  return 1
}

gradle_home_normalize() {
  local home="${GRADLE_USER_HOME:-}"
  [ -n "$home" ] || return 0
  gradle_home_is_temp "$home" || return 0
  case "${GOATOS_ALLOW_PRIVATE_GRADLE_HOME:-0}" in
    1|true|TRUE|yes) echo "gradle-home: keeping private GRADLE_USER_HOME=$home (GOATOS_ALLOW_PRIVATE_GRADLE_HOME=1)" >&2; return 0 ;;
  esac
  echo "gradle-home: WARNING: GRADLE_USER_HOME=$home is a temp dir (cold cache, extra daemon, bypasses the machine Gradle lock). Using $HOME/.gradle instead. Set GOATOS_ALLOW_PRIVATE_GRADLE_HOME=1 to keep it." >&2
  export GRADLE_USER_HOME="$HOME/.gradle"
  return 0
}
