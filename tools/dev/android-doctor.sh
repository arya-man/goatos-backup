#!/usr/bin/env bash
# Fail-fast Android toolchain diagnostic. A missing physical USB phone is not a
# failure when a runnable AVD exists; android-dev-run will start it automatically.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
bash "$repo_root/tools/dev/ensure-android-cli.sh"
# shellcheck source=tools/dev/android-env.sh
source "$repo_root/tools/dev/android-env.sh"

fail=0
ok() { printf '[android-doctor] OK: %s\n' "$*"; }
bad() { printf '[android-doctor] FAIL: %s\n' "$*" >&2; fail=1; }

java_major="$("$JAVA_HOME/bin/java" -version 2>&1 | sed -n '1s/.*version "\([0-9][0-9]*\).*/\1/p')"
[ "$java_major" = "21" ] && ok "JDK 21 at $JAVA_HOME" || bad "expected JDK 21, got ${java_major:-unknown} at $JAVA_HOME"
[ -d "$ANDROID_HOME/platforms/android-36" ] && ok "Android platform 36 installed" || bad "platforms;android-36 missing"
[ -d "$ANDROID_HOME/build-tools/36.0.0" ] || [ -d "$ANDROID_HOME/build-tools/36.1.0" ] \
  && ok "Android build-tools 36 installed" || bad "build-tools 36.x missing"
[ -x "$ANDROID_HOME/platform-tools/adb" ] && ok "adb available" || bad "adb missing"
[ -x "$ANDROID_HOME/emulator/emulator" ] && ok "emulator available" || bad "emulator missing"

avds="$(emulator -list-avds 2>/dev/null || true)"
if [ -n "$avds" ]; then
  ok "AVD available: $(printf '%s\n' "$avds" | paste -sd, -)"
else
  bad "no AVD configured (Android Studio -> Device Manager -> Create device)"
fi

devices="$(adb devices | awk 'NR>1 && $2=="device" {print $1}')"
if [ -n "$devices" ]; then
  ok "booted adb device(s): $(printf '%s\n' "$devices" | paste -sd, -)"
else
  ok "no USB/booted device; android-dev-run will start the configured AVD"
fi

if (cd "$repo_root/apps/goatos-android" && ./gradlew --version --no-daemon >/dev/null); then
  ok "Gradle wrapper starts with the resolved JDK"
else
  bad "Gradle wrapper failed to start"
fi

if [ "$fail" -ne 0 ]; then
  exit 1
fi
ok "mobile toolchain ready"
