#!/usr/bin/env bash
# Start the laptop API on the throwaway phone-QA DB and install the Android dev
# app with a token for one QA user.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
backend_dir="$repo_root/backend"
api_bin="$repo_root/.codex-goatos-render/bin/goatos-api-phone-qa"
relay_bin="$repo_root/.codex-goatos-render/bin/goatos-outbox-relay-phone-qa"
relay_label="sg.mesha.goatos.phone-qa-outbox-relay"
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
# The API alone CANNOT process a domain event. Verdicts, shifting, feed and counts all publish to
# outbox_messages and a SEPARATE relay drains them onto the event bus. Without it every message
# sits `pending` forever: a verifier REJECT is written but never applied, so the operator's screen
# keeps showing the animal as done and no rework appears. That cost a full QA session on
# 2026-08-08 and read as "the accept/reject fix regressed" when the code was fine.
(cd "$backend_dir" && go build -buildvcs=false -o "$relay_bin" ./cmd/outbox-relay)

log "stopping existing local phone/api launch agents"
launchctl bootout "$launch_domain/sg.mesha.goatos.android-dev-api" >/dev/null 2>&1 || true
launchctl bootout "$launch_domain/$label" >/dev/null 2>&1 || true

# Phone QA must NEVER take a default port. 3300 (admin-web), 8080 (API) and 5433
# (database) carry the maintainer's LOCAL REPLICA OF STG DATA, and parallel agent
# sessions share this laptop -- freeing one by killing its holder kills another
# session's stack. This script used to hardcode 8080 and even killed-checked it,
# so following the sanctioned path WAS the violation (2026-08-07). The device
# always calls its own localhost:8080, so only the host side moves: no APK
# rebuild and no token re-mint are needed.
host_port="${GOATOS_PHONE_QA_PORT:-8081}"
case "$host_port" in
  3300|8080|5433) die "refusing default port $host_port for phone QA; those carry the maintainer's stg replica (see AGENTS.md)" ;;
esac
pid="$(lsof -ti tcp:"$host_port" -sTCP:LISTEN 2>/dev/null | head -1 || true)"
if [ -n "$pid" ]; then
  # Only ever reclaim OUR OWN phone-qa API. Any other holder is someone else's work.
  if ps -o command= -p "$pid" 2>/dev/null | grep -q 'goatos-api-phone-qa'; then
    log "reclaiming port $host_port from a previous phone-qa API (pid $pid)"
  else
    die "port $host_port is in use by pid $pid, which is NOT a phone-qa API; pick another GOATOS_PHONE_QA_PORT"
  fi
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
    <string>127.0.0.1:$host_port</string>
    <key>GOATOS_ALLOW_STALE_LOCAL_STACK</key>
    <string>1</string>
    <key>GOATOS_LOCAL_MEDIA_SIGNING_SECRET</key>
    <string>${GOATOS_LOCAL_MEDIA_SIGNING_SECRET:-goatos-local-media-secret-32-bytes-min}</string>
  </dict>
</dict>
</plist>
EOF

relay_plist="$HOME/Library/LaunchAgents/$relay_label.plist"
relay_log="$api_log_dir/outbox-relay.log"
launchctl bootout "$launch_domain/$relay_label" >/dev/null 2>&1 || true
cat >"$relay_plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>$relay_label</string>
  <key>ProgramArguments</key>
  <array>
    <string>$relay_bin</string>
  </array>
  <key>WorkingDirectory</key>
  <string>$backend_dir</string>
  <key>RunAtLoad</key>
  <true/>
  <key>StartInterval</key>
  <integer>10</integer>
  <key>StandardOutPath</key>
  <string>$relay_log</string>
  <key>StandardErrorPath</key>
  <string>$relay_log</string>
  <key>EnvironmentVariables</key>
  <dict>
    <key>DATABASE_URL</key>
    <string>$DATABASE_URL</string>
    <key>GOATOS_ENV</key>
    <string>${GOATOS_ENV:-local}</string>
    <key>GOATOS_TENANT_ID</key>
    <string>$tenant_id</string>
    <key>GOATOS_OUTBOX_PUBLISHER</key>
    <string>eventbus</string>
    <key>GOATOS_OUTBOX_ALLOW_NONDURABLE</key>
    <string>1</string>
    <key>GOATOS_ALLOW_STALE_LOCAL_STACK</key>
    <string>1</string>
  </dict>
</dict>
</plist>
EOF
launchctl bootstrap "$launch_domain" "$relay_plist" >/dev/null 2>&1 || true
launchctl enable "$launch_domain/$relay_label" >/dev/null 2>&1 || true

launchctl bootstrap "$launch_domain" "$plist" >/dev/null
launchctl enable "$launch_domain/$label" >/dev/null 2>&1 || true
launchctl kickstart -k "$launch_domain/$label" >/dev/null 2>&1 || true

for _ in $(seq 1 60); do
  code="$(curl -s -o /dev/null -w '%{http_code}' "http://localhost:$host_port/readyz" || true)"
  [ "$code" = "204" ] && break
  sleep 0.5
done
[ "${code:-}" = "204" ] || die "API did not become ready on :$host_port; see $api_log"

# Device-side stays 8080 (the APK's baked base URL); host side is the QA port.
adb reverse tcp:8080 tcp:"$host_port" >/dev/null
log "API is on :$host_port using ${DATABASE_URL%%\?*}; device localhost:8080 -> laptop:$host_port; installing Android as $user_id"

GOATOS_LOCAL_USER_ID="$user_id" \
GOATOS_TENANT_ID="$tenant_id" \
DATABASE_URL="$DATABASE_URL" \
GOATOS_ENV="${GOATOS_ENV:-local}" \
"$repo_root/tools/dev/android-dev-run.sh" "$@"
