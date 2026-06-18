#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
label="sg.mesha.goatos.local-stack"
domain="gui/$(id -u)"
plist_dir="$HOME/Library/LaunchAgents"
plist_path="$plist_dir/$label.plist"
log_dir="$repo_root/.codex-goatos-render/logs"
launchd_stdout="$log_dir/local-stack-launchd.out.log"
launchd_stderr="$log_dir/local-stack-launchd.err.log"

usage() {
  cat <<EOF
Usage: $0 install|start|stop|restart|status|logs|uninstall

Manages the local Goat OS API/admin-web service on:
  API:       http://127.0.0.1:8080
  admin-web: http://127.0.0.1:3300/
EOF
}

write_plist() {
  mkdir -p "$plist_dir" "$log_dir"
  cat >"$plist_path" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>$label</string>
  <key>ProgramArguments</key>
  <array>
    <string>/bin/bash</string>
    <string>$repo_root/tools/dev/run-local-stack-supervised.sh</string>
  </array>
  <key>WorkingDirectory</key>
  <string>$repo_root</string>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>StandardOutPath</key>
  <string>$launchd_stdout</string>
  <key>StandardErrorPath</key>
  <string>$launchd_stderr</string>
  <key>EnvironmentVariables</key>
  <dict>
    <key>PATH</key>
    <string>/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin</string>
  </dict>
</dict>
</plist>
EOF
}

is_loaded() {
  launchctl print "$domain/$label" >/dev/null 2>&1
}

bootout_if_loaded() {
  if is_loaded; then
    launchctl bootout "$domain/$label" >/dev/null 2>&1 || true
  fi
}

bootstrap() {
  launchctl bootstrap "$domain" "$plist_path"
  launchctl enable "$domain/$label" >/dev/null 2>&1 || true
  launchctl kickstart -k "$domain/$label"
}

status() {
  echo "LaunchAgent: $plist_path"
  if is_loaded; then
    echo "launchd: loaded"
    launchctl print "$domain/$label" | sed -n '1,35p'
  else
    echo "launchd: not loaded"
  fi
  echo
  echo "Ports:"
  lsof -nP -iTCP:8080 -sTCP:LISTEN || true
  lsof -nP -iTCP:3300 -sTCP:LISTEN || true
  echo
  echo "Health:"
  curl -fsS http://127.0.0.1:8080/readyz >/dev/null 2>&1 && echo "api: ready" || echo "api: not ready"
  curl -fsS http://127.0.0.1:3300/login >/dev/null 2>&1 && echo "admin-web: ready" || echo "admin-web: not ready"
}

logs() {
  mkdir -p "$log_dir"
  echo "== supervisor =="
  tail -n 80 "$log_dir/local-stack-supervisor.log" 2>/dev/null || true
  echo
  echo "== api =="
  tail -n 80 "$log_dir/local-api.log" 2>/dev/null || true
  echo
  echo "== admin-web =="
  tail -n 80 "$log_dir/local-admin-web.log" 2>/dev/null || true
  echo
  echo "== launchd stderr =="
  tail -n 80 "$launchd_stderr" 2>/dev/null || true
}

cmd="${1:-}"
case "$cmd" in
  install)
    write_plist
    bootout_if_loaded
    bootstrap
    status
    ;;
  start)
    if [ ! -f "$plist_path" ]; then
      write_plist
    fi
    if is_loaded; then
      launchctl kickstart -k "$domain/$label"
    else
      bootstrap
    fi
    status
    ;;
  stop)
    bootout_if_loaded
    status
    ;;
  restart)
    if [ ! -f "$plist_path" ]; then
      write_plist
    fi
    bootout_if_loaded
    bootstrap
    status
    ;;
  status)
    status
    ;;
  logs)
    logs
    ;;
  uninstall)
    bootout_if_loaded
    rm -f "$plist_path"
    status
    ;;
  *)
    usage
    exit 2
    ;;
esac
