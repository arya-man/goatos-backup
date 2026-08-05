#!/usr/bin/env bash
# android-dev-run.sh — one command to run the Goat OS Android dev app against the
# LOCAL laptop backend on a USB device (physical phone or emulator).
#
# Why this exists (so nobody re-figures it every time):
#   1. The dev flavor authenticates with a MINTED HS256 bearer token baked at build
#      time (BuildConfig.DEV_BEARER_TOKEN <- gradle prop `goatosDevBearerToken`).
#      That token EXPIRES (<=24h) and must be signed with the SAME secret the
#      RUNNING backend uses — the supervised-stack default secret is NOT always the
#      running one (it can be started with an env override), which yields a silent
#      401 "Couldn't load your workspace". This script mints a fresh token and
#      VALIDATES it against /app/bootstrap (200) BEFORE building.
#   2. The app talks to http://localhost:8080; `adb reverse tcp:8080 tcp:8080`
#      tunnels the device loopback to the laptop over USB (works on emulator AND a
#      physical phone; 10.0.2.2 is emulator-only and does NOT work on a real phone).
#
# Prereqs: device connected via USB with USB debugging enabled ("Allow" the RSA
# prompt). The script starts/repairs the local backend service on :8080 before
# minting the dev token, so a dead API cannot strand the phone at the workspace
# error screen.
#
# Usage:
#   tools/dev/android-dev-run.sh                 # auto-pick device, full run
#   tools/dev/android-dev-run.sh -s <serial>     # target a specific adb serial
#   tools/dev/android-dev-run.sh --no-clear      # keep app data (don't pm clear)
#   tools/dev/android-dev-run.sh --token-only    # just mint+bake a fresh token
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
backend_dir="$repo_root/backend"
android_dir="$repo_root/apps/goatos-android"
# Resolve JDK/SDK/PATH independently of ~/.zshrc so agents and CI behave the
# same in non-interactive shells.
# shellcheck source=tools/dev/android-env.sh
source "$repo_root/tools/dev/android-env.sh"
api_base="http://localhost:8090"
bootstrap_path="/app/bootstrap"
tenant_id="${GOATOS_TENANT_ID:-00000000-0000-4000-8000-000000000001}"
# The Android dev build starts in the field-operator surface. The old CEO default could read the
# workflow but was correctly denied TaskExecute on proof upload, leaving camera files only in the
# tablet outbox. Callers can still override this for leadership/verifier testing.
user_id="${GOATOS_LOCAL_USER_ID:-90000000-0000-4000-8000-000000000201}"
ttl="${GOATOS_DEV_TOKEN_TTL:-23h}"
gradle_props="$HOME/.gradle/gradle.properties"
supervisor="$repo_root/tools/dev/run-local-stack-supervised.sh"
service="$repo_root/tools/dev/local-stack-service.sh"
android_api_log_dir="$repo_root/.codex-goatos-render/logs"
android_api_log="$android_api_log_dir/local-api-android-dev.log"
android_api_bin="$repo_root/.codex-goatos-render/bin/goatos-api-android-dev"
android_api_label="sg.mesha.goatos.android-dev-api"
android_api_plist="$HOME/Library/LaunchAgents/$android_api_label.plist"
launch_domain="gui/$(id -u)"

serial=""; do_clear=1; token_only=0
while [ $# -gt 0 ]; do
  case "$1" in
    -s|--serial) serial="$2"; shift 2 ;;
    --no-clear) do_clear=0; shift ;;
    --token-only) token_only=1; shift ;;
    *) echo "unknown arg: $1" >&2; exit 2 ;;
  esac
done

log() { printf '\033[1;32m[android-dev]\033[0m %s\n' "$*"; }
die() { printf '\033[1;31m[android-dev] ERROR:\033[0m %s\n' "$*" >&2; exit 1; }

# --- pick device (physical preferred, else emulator) --------------------------
pick_device() {
  [ -n "$serial" ] && { echo "$serial"; return; }
  local phys emu
  phys="$(adb devices | awk 'NR>1 && $2=="device" && $1 !~ /emulator/ {print $1; exit}')"
  [ -n "$phys" ] && { echo "$phys"; return; }
  emu="$(adb devices | awk 'NR>1 && $2=="device" && $1 ~ /emulator/ {print $1; exit}')"
  echo "$emu"
}

# --- resolve the backend HS256 secret, issuer, audience -----------------------
# Sources tried (first that yields a bootstrap-200 token wins): env override,
# the live :8080 process env, then the supervised-script defaults.
mint_with() { # $1=secret -> prints token on stdout (empty on failure)
  local secret="$1"
  [ -z "$secret" ] && return 0
  ( cd "$backend_dir" && \
    GOATOS_AUTH_HS256_SECRET="$secret" \
    GOATOS_AUTH_ISSUER="$iss" GOATOS_AUTH_AUDIENCE="$aud" \
    GOATOS_AUTH_MODE="bearer" GOATOS_AUTH_MAX_TOKEN_TTL="${maxttl:-24h}" \
    go run ./cmd/mint-dev-token -tenant-id "$tenant_id" -user-id "$user_id" -ttl "$ttl" 2>/dev/null )
}
validate() { # $1=token -> 0 if /app/bootstrap==200
  local code
  code="$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $1" "$api_base$bootstrap_path" || true)"
  [ "$code" = "200" ]
}

detect_android_database_url() {
  local port
  port="$(
    docker ps --format '{{.Names}} {{.Ports}}' 2>/dev/null \
      | awk '$1 == "goatos-local-current" && match($0, /127\.0\.0\.1:[0-9]+->5432\/tcp/) {
          print $0
        }' \
      | sed -nE 's/.*127\.0\.0\.1:([0-9]+)->5432\/tcp.*/\1/p' \
      | head -n 1
  )"
  if [ -n "$port" ]; then
    printf 'postgres://postgres:goatos@127.0.0.1:%s/goatos?sslmode=disable\n' "$port"
    return 0
  fi
  printf 'postgres://postgres:goatos@127.0.0.1:5433/goatos?sslmode=disable\n'
}

start_android_api_fallback() {
  local code pid
  code="$(curl -s -o /dev/null -w '%{http_code}' "$api_base/readyz" || true)"
  [ "$code" = "204" ] && return 0

  pid="$(lsof -ti tcp:8080 -sTCP:LISTEN 2>/dev/null | head -1 || true)"
  if [ -n "$pid" ]; then
    die "port 8080 is in use but /readyz is not healthy; stop pid $pid or free :8080"
  fi

  mkdir -p "$android_api_log_dir"
  log "shared service did not become ready; starting Android dev API fallback on canonical local DB..."
  mkdir -p "$(dirname "$android_api_bin")"
  ( cd "$backend_dir" && go build -buildvcs=false -o "$android_api_bin" ./cmd/api )
  mkdir -p "$(dirname "$android_api_plist")"
  cat >"$android_api_plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>$android_api_label</string>
  <key>ProgramArguments</key>
  <array>
    <string>$android_api_bin</string>
  </array>
  <key>WorkingDirectory</key>
  <string>$backend_dir</string>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>StandardOutPath</key>
  <string>$android_api_log</string>
  <key>StandardErrorPath</key>
  <string>$android_api_log</string>
  <key>EnvironmentVariables</key>
  <dict>
    <key>DATABASE_URL</key>
    <string>${DATABASE_URL:-$(detect_android_database_url)}</string>
    <key>GOATOS_ENV</key>
    <string>${GOATOS_ENV:-local}</string>
    <key>GOATOS_TENANT_ID</key>
    <string>$tenant_id</string>
    <key>GOATOS_AUTH_MODE</key>
    <string>${GOATOS_AUTH_MODE:-bearer}</string>
    <key>GOATOS_AUTH_ISSUER</key>
    <string>$iss</string>
    <key>GOATOS_AUTH_AUDIENCE</key>
    <string>$aud</string>
    <key>GOATOS_AUTH_HS256_SECRET</key>
    <string>${GOATOS_AUTH_HS256_SECRET:-goatos-local-dev-secret-32-bytes-min}</string>
    <key>GOATOS_AUTH_MAX_TOKEN_TTL</key>
    <string>${maxttl:-24h}</string>
    <key>GOATOS_HTTP_ADDR</key>
    <string>127.0.0.1:8080</string>
    <key>GOATOS_ALLOW_STALE_LOCAL_STACK</key>
    <string>1</string>
    <key>GOATOS_LOCAL_MEDIA_SIGNING_SECRET</key>
    <string>${GOATOS_LOCAL_MEDIA_SIGNING_SECRET:-goatos-local-media-secret-32-bytes-min}</string>
  </dict>
</dict>
</plist>
EOF
  launchctl bootout "$launch_domain/$android_api_label" >/dev/null 2>&1 || true
  launchctl bootstrap "$launch_domain" "$android_api_plist" >/dev/null
  launchctl enable "$launch_domain/$android_api_label" >/dev/null 2>&1 || true
  launchctl kickstart -k "$launch_domain/$android_api_label" >/dev/null 2>&1 || true

  for _ in $(seq 1 60); do
    code="$(curl -s -o /dev/null -w '%{http_code}' "$api_base/readyz" || true)"
    [ "$code" = "204" ] && return 0
    sleep 1
  done
  cat "$android_api_log" >&2 || true
  die "Android dev API fallback did not become ready on :8080"
}

ensure_backend() {
  local code
  code="$(curl -s -o /dev/null -w '%{http_code}' "$api_base/readyz" || true)"
  if [ "$code" = "204" ]; then
    return 0
  fi
  log "backend not ready on :8080 (readyz=$code); starting local backend service..."
  bash "$service" start >/dev/null
  for _ in $(seq 1 45); do
    code="$(curl -s -o /dev/null -w '%{http_code}' "$api_base/readyz" || true)"
    [ "$code" = "204" ] && return 0
    sleep 1
  done
  log "backend still not ready; restarting local backend service..."
  bash "$service" restart >/dev/null
  for _ in $(seq 1 60); do
    code="$(curl -s -o /dev/null -w '%{http_code}' "$api_base/readyz" || true)"
    [ "$code" = "204" ] && return 0
    sleep 1
  done
  bash "$service" logs >&2 || true
  start_android_api_fallback
}

# issuer/audience/max-ttl from the supervised script (with sane fallbacks)
eval "$(grep -E '^export GOATOS_AUTH_(ISSUER|AUDIENCE|MAX_TOKEN_TTL)=' "$supervisor" 2>/dev/null || true)"
iss="${GOATOS_AUTH_ISSUER:-goatos-local}"
aud="${GOATOS_AUTH_AUDIENCE:-goatos-api}"
maxttl="${GOATOS_AUTH_MAX_TOKEN_TTL:-24h}"

ensure_backend
log "backend health: $(curl -s -o /dev/null -w '%{http_code}' "$api_base/readyz" || echo unreachable) (expect 204); minting a dev token that /app/bootstrap accepts..."

token=""
# 1) explicit env secret
if [ -n "${GOATOS_AUTH_HS256_SECRET:-}" ]; then
  t="$(mint_with "$GOATOS_AUTH_HS256_SECRET")"; if [ -n "$t" ] && validate "$t"; then token="$t"; log "secret source: \$GOATOS_AUTH_HS256_SECRET"; fi
fi
# 2) live :8080 process env
if [ -z "$token" ]; then
  apipid="$(lsof -ti tcp:8080 -sTCP:LISTEN 2>/dev/null | head -1 || true)"
  if [ -n "$apipid" ]; then
    s="$(ps eww "$apipid" 2>/dev/null | tr ' ' '\n' | grep '^GOATOS_AUTH_HS256_SECRET=' | head -1 | cut -d= -f2- || true)"
    t="$(mint_with "$s")"; if [ -n "$t" ] && validate "$t"; then token="$t"; log "secret source: live :8080 process env"; fi
  fi
fi
# 3) supervised-script default
if [ -z "$token" ]; then
  s="$(eval "$(grep -E '^export GOATOS_AUTH_HS256_SECRET=' "$supervisor" 2>/dev/null)"; printf '%s' "${GOATOS_AUTH_HS256_SECRET:-}")"
  t="$(mint_with "$s")"; if [ -n "$t" ] && validate "$t"; then token="$t"; log "secret source: run-local-stack-supervised.sh default"; fi
fi
[ -n "$token" ] || die "could not mint a token the backend accepts (is the stack up on :8080? secret mismatch?). Start it with: make dev-local-service-start"

# bake into ~/.gradle/gradle.properties (token value never printed)
mkdir -p "$(dirname "$gradle_props")"; touch "$gradle_props"
grep -Ev '^(goatosDevBearerToken|goatosDevApiBaseUrl)=' "$gradle_props" > "$gradle_props.tmp" 2>/dev/null || true
printf 'goatosDevApiBaseUrl=http://localhost:8080/\n' >> "$gradle_props.tmp"
printf 'goatosDevBearerToken=%s\n' "$token" >> "$gradle_props.tmp"
mv "$gradle_props.tmp" "$gradle_props"
log "dev API URL + fresh token baked into ~/.gradle/gradle.properties (validated: /app/bootstrap 200, ttl $ttl)"
[ "$token_only" = "1" ] && { log "token-only: done."; exit 0; }

# --- build + install + tunnel + launch ---------------------------------------
dev="$(pick_device)"
if [ -z "$dev" ]; then
  log "no authorized USB device or booted emulator; starting the configured AVD fallback..."
  dev="$(bash "$repo_root/tools/dev/android-emulator-ensure.sh")" || \
    die "no usable Android device and emulator fallback failed"
fi
log "device: $dev"
log "JAVA_HOME=$JAVA_HOME"

log "building :app:assembleDevDebug ..."
( cd "$android_dir" && ./gradlew :app:assembleDevDebug --console=plain -q )
apk="$android_dir/app/build/outputs/apk/dev/debug/app-dev-debug.apk"
[ -f "$apk" ] || die "APK not found at $apk"

log "installing on $dev ..."
adb -s "$dev" install -r "$apk" >/dev/null
device_user="$(adb -s "$dev" shell am get-current-user 2>/dev/null | tr -d '\r' | head -1)"
[[ "$device_user" =~ ^[0-9]+$ ]] || die "could not resolve the foreground Android user on $dev"
[ "$do_clear" = "1" ] && {
  adb -s "$dev" shell pm clear --user "$device_user" sg.mesha.goatos.dev >/dev/null 2>&1 || true
  log "cleared app data for foreground Android user $device_user (fresh token will be used)"
}
adb -s "$dev" reverse tcp:8080 tcp:8090 >/dev/null
log "adb reverse tcp:8080 -> laptop:8080 (device localhost now reaches the laptop backend)"
adb -s "$dev" shell am start --user "$device_user" -n sg.mesha.goatos.dev/sg.mesha.goatos.MainActivity >/dev/null 2>&1 || true
log "launched. If it shows 'Couldn't load your workspace', the backend isn't reachable — re-run this script (it re-mints + re-tunnels)."
