#!/usr/bin/env bash
# Shared, sourceable Android/JDK environment resolver for Goat OS developer tools.
# It deliberately does not depend on a developer's shell rc files, so CI agents,
# Codex, Claude, IDE terminals, and plain `bash` all resolve the same toolchain.

android_env_die() {
  printf '[android-env] ERROR: %s\n' "$*" >&2
  return 1
}

android_resolve_env() {
  local candidate=""
  local java_major=""

  if [ -n "${JAVA_HOME:-}" ] && [ -x "$JAVA_HOME/bin/java" ]; then
    java_major="$("$JAVA_HOME/bin/java" -version 2>&1 | sed -n '1s/.*version "\([0-9][0-9]*\).*/\1/p')"
  fi
  if [ "$java_major" != "21" ]; then
    for candidate in \
      "$(/usr/libexec/java_home -v 21 2>/dev/null || true)" \
      /opt/homebrew/opt/openjdk@21 \
      /usr/local/opt/openjdk@21 \
      /opt/homebrew/opt/openjdk@21/libexec/openjdk.jdk/Contents/Home; do
      if [ -n "$candidate" ] && [ -x "$candidate/bin/java" ]; then
        export JAVA_HOME="$candidate"
        java_major="$("$JAVA_HOME/bin/java" -version 2>&1 | sed -n '1s/.*version "\([0-9][0-9]*\).*/\1/p')"
        [ "$java_major" = "21" ] && break
      fi
    done
  fi
  [ -n "${JAVA_HOME:-}" ] && [ -x "$JAVA_HOME/bin/java" ] && [ "$java_major" = "21" ] || \
    android_env_die "JDK 21 not found. macOS: brew install openjdk@21"

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
