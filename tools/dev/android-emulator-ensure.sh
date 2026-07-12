#!/usr/bin/env bash
# Ensure at least one booted Android emulator exists. A physical USB device is
# preferred by android-dev-run.sh; this is the automatic fallback when none is
# connected. Prints only the selected adb serial on stdout.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=tools/dev/android-env.sh
source "$repo_root/tools/dev/android-env.sh"

log() { printf '[android-emulator] %s\n' "$*" >&2; }
die() { log "ERROR: $*"; exit 1; }

online_emulator() {
  adb devices | awk 'NR>1 && $2=="device" && $1 ~ /^emulator-/ {print $1; exit}'
}

serial="$(online_emulator)"
if [ -n "$serial" ]; then
  log "already running: $serial"
  printf '%s\n' "$serial"
  exit 0
fi

avd="${GOATOS_ANDROID_AVD:-}"
if [ -z "$avd" ]; then
  avd="$(emulator -list-avds | sed -n '1p')"
fi
[ -n "$avd" ] || die "no AVD exists. Android Studio -> Device Manager -> Create device; install an ARM64 API 36 image."

log_dir="$repo_root/.codex-goatos-render/logs"
mkdir -p "$log_dir"
log_file="$log_dir/android-emulator.log"
args=(-avd "$avd" -no-audio -no-snapshot-save)
if [ "${GOATOS_ANDROID_EMULATOR_HEADLESS:-0}" = "1" ]; then
  args+=(-no-window)
fi

log "starting AVD '$avd' (log: $log_file)"
nohup emulator "${args[@]}" >"$log_file" 2>&1 &
printf '%s\n' "$!" >"$log_dir/android-emulator.pid"

deadline=$((SECONDS + ${GOATOS_ANDROID_EMULATOR_START_TIMEOUT_SECONDS:-180}))
while [ "$SECONDS" -lt "$deadline" ]; do
  serial="$(online_emulator)"
  [ -n "$serial" ] && break
  sleep 2
done
[ -n "$serial" ] || die "AVD '$avd' did not appear in adb before timeout; inspect $log_file"

log "waiting for Android boot completion on $serial"
while [ "$SECONDS" -lt "$deadline" ]; do
  if [ "$(adb -s "$serial" shell getprop sys.boot_completed 2>/dev/null | tr -d '\r')" = "1" ]; then
    adb -s "$serial" shell input keyevent 82 >/dev/null 2>&1 || true
    log "ready: $serial"
    printf '%s\n' "$serial"
    exit 0
  fi
  sleep 2
done
die "$serial appeared but did not finish booting before timeout; inspect $log_file"
