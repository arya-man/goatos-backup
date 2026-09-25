#!/usr/bin/env bash
# Shared, sourceable Android/JDK environment resolver for Goat OS developer tools.
# It deliberately does not depend on a developer's shell rc files, so CI agents,
# Codex, Claude, IDE terminals, and plain `bash` all resolve the same toolchain.

# shellcheck source=tools/ci/java21.sh
. "$(cd "$(dirname "${BASH_SOURCE[0]}")/../ci" && pwd)/java21.sh"

android_env_die() {
  printf '[android-env] ERROR: %s\n' "$*" >&2
  return 1
}

android_resolve_env() {
  local candidate=""
  local java_major=""

  # JDK 21 via the shared resolver (tools/ci/java21.sh). Always exports an
  # explicit, verified JDK 21, whatever JAVA_HOME was inherited: on Ravi's Mac
  # the only JVM registered with /usr/libexec/java_home is an old jbr-17, so
  # anything that falls back to the system Java gets 17.
  local jdk21
  jdk21="$(java21_resolve "${JAVA_HOME:-}")" || { android_env_die "JDK 21 not found. macOS: brew install openjdk@21"; return 1; }
  export JAVA_HOME="$jdk21"

  if [ -z "${ANDROID_HOME:-}" ]; then
    for candidate in "$HOME/Library/Android/sdk" "$HOME/Android/Sdk"; do
      if [ -d "$candidate" ]; then
        export ANDROID_HOME="$candidate"
        break
      fi
    done
  fi
  [ -n "${ANDROID_HOME:-}" ] && [ -d "$ANDROID_HOME" ] || \
    android_env_die "Android SDK not found. Install it with Android Studio, or set ANDROID_HOME."

  export ANDROID_SDK_ROOT="$ANDROID_HOME"
  export PATH="$HOME/.local/bin:$JAVA_HOME/bin:$ANDROID_HOME/platform-tools:$ANDROID_HOME/emulator:$ANDROID_HOME/cmdline-tools/latest/bin:$PATH"

  [ -x "$ANDROID_HOME/platform-tools/adb" ] || \
    android_env_die "adb missing. Install Android SDK Platform-Tools."
  [ -x "$ANDROID_HOME/emulator/emulator" ] || \
    android_env_die "Android emulator missing. Install it from Android Studio SDK Manager."
}

android_resolve_env
