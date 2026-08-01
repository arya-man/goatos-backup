#!/usr/bin/env bash
# Boot one emulator per role, each signed in as a different person, all at the same time.
#
# Why concurrent rather than sequential: the interesting notification lands on SOMEONE ELSE'S
# device. An operator submits and the director, CEO and verifier are supposed to be told. If you
# test that by logging out and back in as the director, the push has already been delivered or
# dropped -- and an empty screen looks identical whether the notification was never generated,
# went to the wrong person, or simply arrived while you were someone else. Every role has to be
# signed in and listening BEFORE the operator acts.
#
# It also proves the negative, which is the part that actually matters here: the vaccination
# director must NOT receive a weighing proof, and the operator must not be notified as their own
# verifier. You cannot observe a notification that correctly did not arrive unless that person's
# device was awake and listening at the time.
#
# One AVD image runs N times via -read-only (each instance gets its own writable overlay).
#
# Usage:
#   tools/local/multi-role-emulators.sh boot          # boot every role's emulator
#   tools/local/multi-role-emulators.sh status        # who is booted, and as whom
#   tools/local/multi-role-emulators.sh install       # build once, install on all, sign each in
#   tools/local/multi-role-emulators.sh shot <label>  # screenshot every device at once
#   tools/local/multi-role-emulators.sh kill          # shut them all down
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
EMU="${ANDROID_EMULATOR:-$HOME/Library/Android/sdk/emulator/emulator}"
AVD="${GOATOS_QA_AVD:-Medium_Phone_API_36.1}"
SHOTS="${GOATOS_QA_SHOTS:-$REPO_ROOT/.codex-goatos-render/multi-role}"

# role:port:user_id -- ports must be even and 5554..5584.
# User ids come from docs/runbooks/phone-qa-throwaway-rbac.md.
ROLES=(
  "operator-cbe:5554:90000000-0000-4000-8000-000000000202"   # Pramod, CBE only
  "weighing-director:5556:90000000-0000-4000-8000-000000000103" # Dinakar, both parks
  "ceo:5558:90000000-0000-4000-8000-000000000101"            # CEO QA, tenant
  "verifier:5560:90000000-0000-4000-8000-000000000104"       # Jyothi, tenant-level video review
)

die() { echo "multi-role: $*" >&2; exit 1; }
log() { printf '[multi-role] %s\n' "$*"; }

serial_for() { echo "emulator-$1"; }

booted() { adb devices | grep -q "^$(serial_for "$1")[[:space:]]*device$"; }

free_gb() {
  vm_stat | awk '/Pages free/{f=$3} /Pages inactive/{i=$3} END{gsub(/\./,"",f); gsub(/\./,"",i); printf "%.1f", (f+i)*4096/1073741824}'
}

boot_all() {
  command -v "$EMU" >/dev/null || die "emulator not found at $EMU (set ANDROID_EMULATOR)"
  local want=$(( ${#ROLES[@]} * 2 ))   # ~2GB each, rough
  local have; have="$(free_gb)"
  # A booting emulator that runs out of memory does not fail cleanly -- it hangs half-started and
  # every adb command against it blocks. Refuse up front instead.
  if awk -v h="$have" -v w="$want" 'BEGIN{exit !(h < w)}'; then
    echo "multi-role: ${have}GB free, need roughly ${want}GB for ${#ROLES[@]} emulators." >&2
    echo "Close what you can (agent fleets and Gradle daemons are the usual culprits) and retry," >&2
    echo "or boot a subset by trimming ROLES in this script." >&2
    exit 1
  fi

  for entry in "${ROLES[@]}"; do
    IFS=: read -r role port _ <<<"$entry"
    if booted "$port"; then log "$role already up on $(serial_for "$port")"; continue; fi
    log "booting $role on port $port"
    # -read-only lets one AVD image back several instances; -no-snapshot keeps them independent
    # so a stale snapshot cannot silently restore another role's session.
    "$EMU" -avd "$AVD" -port "$port" -read-only -no-snapshot -no-boot-anim >/dev/null 2>&1 &
    sleep 4
  done

  for entry in "${ROLES[@]}"; do
    IFS=: read -r role port _ <<<"$entry"
    local serial; serial="$(serial_for "$port")"
    log "waiting for $role ($serial)"
    adb -s "$serial" wait-for-device
    # wait-for-device returns as soon as adb answers, which is well before Android is usable.
    for _ in $(seq 1 90); do
      [ "$(adb -s "$serial" shell getprop sys.boot_completed 2>/dev/null | tr -d '\r')" = "1" ] && break
      sleep 2
    done
    # Each emulator needs its own tunnel to the laptop API; adb reverse is per-device.
    adb -s "$serial" reverse tcp:8080 tcp:8080 >/dev/null 2>&1 || true
    log "$role ready"
  done
  status
}

status() {
  printf '%-20s %-18s %s\n' ROLE SERIAL STATE
  for entry in "${ROLES[@]}"; do
    IFS=: read -r role port _ <<<"$entry"
    local serial; serial="$(serial_for "$port")"
    if booted "$port"; then
      local pkg_state="app not installed"
      adb -s "$serial" shell pm list packages 2>/dev/null | grep -q 'sg.mesha.goatos' && pkg_state="app installed"
      printf '%-20s %-18s %s\n' "$role" "$serial" "$pkg_state"
    else
      printf '%-20s %-18s %s\n' "$role" "$serial" "down"
    fi
  done
}

install_all() {
  local android_dir="$REPO_ROOT/apps/goatos-android"
  local apk="$android_dir/app/build/outputs/apk/dev/debug/app-dev-debug.apk"

  # The dev bearer token is a BuildConfig field (app/build.gradle.kts:56), so one APK carries
  # exactly one identity. Four roles therefore need four builds. They MUST be sequential: two
  # concurrent Gradle invocations deadlock on the project lock and leave orphaned test JVMs
  # running for hours. Only the app module's BuildConfig changes between roles, so builds 2..N
  # are incremental and cheap.
  for entry in "${ROLES[@]}"; do
    IFS=: read -r role port user <<<"$entry"
    booted "$port" || { log "skip $role (not booted)"; continue; }
    local serial; serial="$(serial_for "$port")"

    log "minting a token for $role"
    local token
    token="$(GOATOS_LOCAL_USER_ID="$user" "$REPO_ROOT/tools/dev/mint-dev-token.sh" 2>/dev/null || true)"
    if [ -z "$token" ]; then
      log "WARNING: could not mint a token for $role; sign in manually on $serial"
      continue
    fi

    log "building for $role (sequential -- never run two Gradle builds at once)"
    ( cd "$android_dir" && ./gradlew :app:assembleDevDebug --console=plain -q \
        -PgoatosDevBearerToken="$token" ) || die "build failed for $role"

    log "installing on $role ($serial)"
    adb -s "$serial" install -r -g "$apk" >/dev/null
    adb -s "$serial" reverse tcp:8080 tcp:8080 >/dev/null 2>&1 || true
    # Wipe the previous role's session BEFORE anything can re-persist it. A stale DataStore token
    # silently outranks the freshly baked one (AppModule prefers SessionStore.cachedToken over
    # BuildConfig.DEV_BEARER_TOKEN), so a device that already had the app stays signed in as the
    # PREVIOUS person -- verified on a real phone: an operator build showed "CEO QA / CXO" until
    # this ran. Every role would appear to test correctly while actually testing one identity.
    #
    # force-stop FIRST: clearing a running app races with the process writing its session back,
    # and `pm clear` alone did not stick.
    adb -s "$serial" shell am force-stop sg.mesha.goatos.dev >/dev/null 2>&1 || true
    adb -s "$serial" shell pm clear sg.mesha.goatos.dev >/dev/null 2>&1 || true
    adb -s "$serial" shell monkey -p sg.mesha.goatos.dev -c android.intent.category.LAUNCHER 1 >/dev/null 2>&1 || true
    log "$role signed in"
  done
  status
}

shots() {
  local label="${1:-shot}"
  mkdir -p "$SHOTS"
  for entry in "${ROLES[@]}"; do
    IFS=: read -r role port _ <<<"$entry"
    booted "$port" || continue
    local serial; serial="$(serial_for "$port")"
    local out="$SHOTS/${label}__${role}.png"
    adb -s "$serial" exec-out screencap -p > "$out"
    log "captured $out"
  done
}

kill_all() {
  for entry in "${ROLES[@]}"; do
    IFS=: read -r _ port _ <<<"$entry"
    booted "$port" && adb -s "$(serial_for "$port")" emu kill >/dev/null 2>&1 || true
  done
  log "all role emulators stopped"
}

case "${1:-status}" in
  boot)    boot_all ;;
  status)  status ;;
  install) install_all ;;
  shot)    shots "${2:-shot}" ;;
  kill)    kill_all ;;
  *) die "usage: $0 boot|status|install|shot <label>|kill" ;;
esac
