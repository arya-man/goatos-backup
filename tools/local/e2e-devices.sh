#!/usr/bin/env bash
# Point every attached device/emulator at the THROWAWAY E2E stack, one role per device.
#
# Why this exists: every device-side failure in the 2026-08-04 E2E was a setup step someone
# had to remember, and each one failed silently in a way that looked like an app bug:
#
#   * `adb reverse tcp:8080 tcp:8080` sent two phones at the maintainer's SHARED local replica
#     on host 8080. The app rendered a completely different farm (Sumathi, 76 animals) and
#     nothing anywhere said "wrong database".
#   * Restarting the adb server (a stray pkill) silently wiped every reverse mapping, so the
#     phones fell back to stale Room cache and showed a plausible, entirely wrong screen.
#   * An `assembleDevDebug` run WITHOUT -PgoatosDevBearerToken produced an APK with an empty
#     baked token. The app quietly fell back to Firebase auth, authenticated as a subject with
#     no grants, and every request 403'd -- which reads as "backend broken", not "wrong build".
#   * Installing one APK on four phones collapsed all four roles onto a single account, because
#     the role identity is baked in at COMPILE time, not chosen at runtime.
#
# None of that is worth remembering. Run this instead.
#
#   tools/local/e2e-devices.sh                 # build + install + verify every mapped device
#   tools/local/e2e-devices.sh --verify-only   # check the fleet without rebuilding
#
# Ports: the throwaway API is 8090. 8080/3300/5433 belong to the maintainer's own stack and are
# NEVER bound or targeted here -- the reverse maps the DEVICE's 8080 (what the app was compiled
# to call) onto the HOST's 8090.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
ANDROID_DIR="$REPO_ROOT/apps/goatos-android"
API_PORT="${GOATOS_E2E_API_PORT:-8090}"
DEVICE_PORT=8080
PKG="sg.mesha.goatos.dev"

# device serial : role label : token file. Edit here when the fleet changes.
#
# THE OPERATOR ROLE IS PINNED TO THE POCO X5 (dd861eff, model 22101320I). That handset is the
# one with the BLE RFID reader PAIRED to it, so it is the only device in the fleet that can
# actually scan a tag. Putting the operator build on any other phone makes scanning impossible
# and forces a re-flash of two devices to recover. Do not "balance" the roles across handsets:
# every other role is read-only and can live anywhere, the operator cannot.
FLEET=(
  "dd861eff:operator:/tmp/tok-202.txt"
  "143382555G111292:verifier:/tmp/tok-104.txt"
  "F5625U031150:pc_director:/tmp/tok-102.txt"
  "emulator-5554:cxo:/tmp/tok-101.txt"
)

VERIFY_ONLY=0
[[ "${1:-}" == "--verify-only" ]] && VERIFY_ONLY=1

fail() { echo "e2e-devices: $*" >&2; exit 1; }

# The API must be up BEFORE anything is installed: a device pointed at a dead port caches an
# error state and then renders it as if it were data.
curl -sf -o /dev/null "http://127.0.0.1:${API_PORT}/healthz" \
  || fail "throwaway API is not answering on 127.0.0.1:${API_PORT} -- start it first"

for entry in "${FLEET[@]}"; do
  serial="${entry%%:*}"; rest="${entry#*:}"; role="${rest%%:*}"; token_file="${rest##*:}"

  adb devices | grep -q "^${serial}[[:space:]]*device$" || { echo "SKIP ${serial} (${role}): not attached"; continue; }
  [[ -r "$token_file" ]] || fail "${role}: token file $token_file is missing"
  token="$(tr -d '[:space:]' < "$token_file")"
  [[ -n "$token" ]] || fail "${role}: token file $token_file is empty"

  # Always re-assert the tunnel. It does not survive an adb server restart, and a stale
  # device->8080 mapping silently serves the WRONG DATABASE rather than failing.
  adb -s "$serial" reverse --remove-all >/dev/null 2>&1 || true
  adb -s "$serial" reverse "tcp:${DEVICE_PORT}" "tcp:${API_PORT}" >/dev/null

  if [[ $VERIFY_ONLY -eq 0 ]]; then
    # One build PER ROLE: the bearer token is a compile-time BuildConfig field, so a single APK
    # across the fleet makes every phone the same person.
    ( cd "$ANDROID_DIR" && ./gradlew :app:assembleDevDebug -PgoatosDevBearerToken="$token" --console=plain -q )
    apk="$ANDROID_DIR/app/build/outputs/apk/dev/debug/app-dev-debug.apk"

    # Some handsets run the app under a secondary Android user; installing to user 0 there
    # leaves the running instance on the OLD build with no error.
    user_id="$(adb -s "$serial" shell pm list users 2>/dev/null | grep -o '{1[0-9]*:' | head -1 | tr -d '{:' || true)"
    if [[ -n "$user_id" ]]; then
      adb -s "$serial" install -r --user "$user_id" "$apk" >/dev/null
    else
      adb -s "$serial" install -r "$apk" >/dev/null
    fi
    # Room cache survives a reinstall and will happily render another database's farm.
    adb -s "$serial" shell pm clear "$PKG" >/dev/null 2>&1 || true
  fi

  adb -s "$serial" shell am force-stop "$PKG" >/dev/null 2>&1 || true
  adb -s "$serial" logcat -c >/dev/null 2>&1 || true
  adb -s "$serial" shell monkey -p "$PKG" -c android.intent.category.LAUNCHER 1 >/dev/null 2>&1 || true
  echo "${role} (${serial}): installed, tunnel ${DEVICE_PORT}->${API_PORT}"
done

echo "waiting for bootstrap..."
sleep 25

status=0
for entry in "${FLEET[@]}"; do
  serial="${entry%%:*}"; rest="${entry#*:}"; role="${rest%%:*}"
  adb devices | grep -q "^${serial}[[:space:]]*device$" || continue

  log="$(adb -s "$serial" logcat -d -s GoatOSAnalytics 2>/dev/null || true)"
  seen_role="$(printf '%s' "$log" | grep -o 'user_property role=[a-z_]*' | tail -1 | cut -d= -f2 || true)"
  ids="$(printf '%s' "$log" | grep -oE 'device_id=[0-9a-f]{8}' | sort -u | wc -l | tr -d ' ')"
  denied="$(adb -s "$serial" logcat -d 2>/dev/null | grep -c 'HTTP 403' || true)"

  # A role mismatch means the wrong token was baked in; >1 device_id means the install-id is
  # not stable (that was a real defect); any 403 means it is talking to the wrong backend.
  if [[ "$seen_role" != "$role" ]]; then
    echo "FAIL ${role} (${serial}): app reports role='${seen_role:-none}'"; status=1
  elif [[ "$denied" != "0" ]]; then
    echo "FAIL ${role} (${serial}): ${denied} x HTTP 403 -- wrong backend or unseeded identity"; status=1
  elif [[ "$ids" != "1" ]]; then
    echo "FAIL ${role} (${serial}): ${ids} distinct device_ids, expected exactly 1"; status=1
  else
    echo "OK   ${role} (${serial}): role=${seen_role} device_ids=1 403s=0"
  fi
done

exit $status
