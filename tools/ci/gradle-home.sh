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
# $HOME/.gradle, but only when $HOME/.gradle is writable (a sandbox that cannot
# write it keeps its private home, with a warning).
# GOATOS_ALLOW_PRIVATE_GRADLE_HOME=1 keeps it (for a deliberate throwaway run). Any other explicit home is left alone. Then installs the
# machine Gradle queue + managed gradle.properties (gradle-machine-setup.sh)
# into the home in use. Never fails.

_GOATOS_GH_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=tools/ci/gradle-machine-setup.sh
. "$_GOATOS_GH_DIR/gradle-machine-setup.sh"

gradle_home_is_temp() {
  case "${1:-}" in
    /tmp|/tmp/*|/private/tmp|/private/tmp/*|/var/folders/*|/private/var/folders/*) return 0 ;;
  esac
  return 1
}

gradle_home_normalize() {
  _gradle_home_normalize_only
  # Keep the machine queue + user gradle.properties installed in the home
  # this run will use (idempotent, never fails).
  gradle_machine_install
  return 0
}

_gradle_home_normalize_only() {
  local home="${GRADLE_USER_HOME:-}"
  [ -n "$home" ] || return 0
  gradle_home_is_temp "$home" || return 0
  case "${GOATOS_ALLOW_PRIVATE_GRADLE_HOME:-0}" in
    1|true|TRUE|yes) echo "gradle-home: keeping private GRADLE_USER_HOME=$home (GOATOS_ALLOW_PRIVATE_GRADLE_HOME=1)" >&2; return 0 ;;
  esac
  # A sandbox may not be allowed to write ~/.gradle; resetting there would
  # break the build, so keep the private home and just warn.
  local shared="$HOME/.gradle"
  if ! { [ -d "$shared" ] && [ -w "$shared" ]; } && ! { [ ! -e "$shared" ] && [ -w "$HOME" ]; }; then
    echo "gradle-home: WARNING: GRADLE_USER_HOME=$home is a temp dir, but $shared is not writable here; keeping the private home (cold cache, extra daemon, outside the machine Gradle lock)." >&2
    return 0
  fi
  echo "gradle-home: WARNING: GRADLE_USER_HOME=$home is a temp dir (cold cache, extra daemon, bypasses the machine Gradle lock). Using $HOME/.gradle instead. Set GOATOS_ALLOW_PRIVATE_GRADLE_HOME=1 to keep it." >&2
  export GRADLE_USER_HOME="$HOME/.gradle"
  return 0
}
