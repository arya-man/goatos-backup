#!/usr/bin/env bash
# Start the laptop API on the throwaway phone-QA DB and install the Android dev
# app with a token for one QA user.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
backend_dir="$repo_root/backend"
api_bin="$repo_root/.codex-goatos-render/bin/goatos-api-phone-qa"
api_log_dir="$repo_root/.codex-goatos-render/logs"
api_log="$api_log_dir/phone-qa-api.log"
label="sg.mesha.goatos.phone-qa-api"
plist="$HOME/Library/LaunchAgents/$label.plist"
launch_domain="gui/$(id -u)"
tenant_id="${GOATOS_TENANT_ID:-00000000-0000-4000-8000-000000000001}"
user_id="${GOATOS_LOCAL_USER_ID:-90000000-0000-4000-8000-000000000102}"

die() { echo "phone-qa-throwaway-run: $*" >&2; exit 1; }
log() { printf '[phone-qa] %s\n' "$*"; }

[ -n "${DATABASE_URL:-}" ] || die "DATABASE_URL is required"
case "$DATABASE_URL" in
  *127.0.0.1:15544/*|*localhost:15544/*) ;;
  *) die "refusing DATABASE_URL outside throwaway port 15544: ${DATABASE_URL%%\?*}" ;;
esac

mkdir -p "$(dirname "$api_bin")" "$api_log_dir" "$(dirname "$plist")"

log "building backend API"
(cd "$backend_dir" && go build -buildvcs=false -o "$api_bin" ./cmd/api)

log "stopping existing local phone/api launch agents"
launchctl bootout "$launch_domain/sg.mesha.goatos.android-dev-api" >/dev/null 2>&1 || true
launchctl bootout "$launch_domain/$label" >/dev/null 2>&1 || true

pid="$(lsof -ti tcp:8080 -sTCP:LISTEN 2>/dev/null | head -1 || true)"
if [ -n "$pid" ]; then
  die "port 8080 is already in use by pid $pid; stop it before running phone QA"
fi

cat >"$plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>$label</string>
  <key>ProgramArguments</key>
  <array>
    <string>$api_bin</string>
  </array>
  <key>WorkingDirectory</key>
  <string>$backend_dir</string>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>StandardOutPath</key>
  <string>$api_log</string>
  <key>StandardErrorPath</key>
  <string>$api_log</string>
  <key>EnvironmentVariables</key>
  <dict>
    <key>DATABASE_URL</key>
    <string>$DATABASE_URL</string>
    <key>GOATOS_ENV</key>
    <string>${GOATOS_ENV:-local}</string>
    <key>GOATOS_TENANT_ID</key>
    <string>$tenant_id</string>
    <key>GOATOS_AUTH_MODE</key>
    <string>${GOATOS_AUTH_MODE:-bearer}</string>
    <key>GOATOS_AUTH_ISSUER</key>
    <string>${GOATOS_AUTH_ISSUER:-goatos-local}</string>
    <key>GOATOS_AUTH_AUDIENCE</key>
    <string>${GOATOS_AUTH_AUDIENCE:-goatos-api}</string>
    <key>GOATOS_AUTH_HS256_SECRET</key>
    <string>${GOATOS_AUTH_HS256_SECRET:-goatos-local-dev-secret-32-bytes-min}</string>
    <key>GOATOS_AUTH_MAX_TOKEN_TTL</key>
    <string>${GOATOS_AUTH_MAX_TOKEN_TTL:-24h}</string>
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

launchctl bootstrap "$launch_domain" "$plist" >/dev/null
launchctl enable "$launch_domain/$label" >/dev/null 2>&1 || true
launchctl kickstart -k "$launch_domain/$label" >/dev/null 2>&1 || true

for _ in $(seq 1 60); do
  code="$(curl -s -o /dev/null -w '%{http_code}' http://localhost:8080/readyz || true)"
  [ "$code" = "204" ] && break
  sleep 0.5
done
[ "${code:-}" = "204" ] || die "API did not become ready on :8080; see $api_log"

adb reverse tcp:8080 tcp:8080 >/dev/null
log "API is on :8080 using ${DATABASE_URL%%\?*}; installing Android as $user_id"

GOATOS_LOCAL_USER_ID="$user_id" \
GOATOS_TENANT_ID="$tenant_id" \
DATABASE_URL="$DATABASE_URL" \
GOATOS_ENV="${GOATOS_ENV:-local}" \
"$repo_root/tools/dev/android-dev-run.sh" "$@"
