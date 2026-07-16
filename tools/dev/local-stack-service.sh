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
api_port="${GOATOS_LOCAL_API_PORT:-8080}"
web_port="${GOATOS_LOCAL_WEB_PORT:-3300}"

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

plist_points_to_repo() {
  [ -f "$plist_path" ] || return 1
  grep -Fq "<string>$repo_root/tools/dev/run-local-stack-supervised.sh</string>" "$plist_path" \
    && grep -Fq "<string>$repo_root</string>" "$plist_path"
}

bootstrap() {
  launchctl bootstrap "$domain" "$plist_path"
  launchctl enable "$domain/$label" >/dev/null 2>&1 || true
  launchctl kickstart -k "$domain/$label"
}

listener_pids() {
  local port="$1"
  lsof -nP -tiTCP:"$port" -sTCP:LISTEN 2>/dev/null || true
}

pid_cwd() {
  local pid="$1"
  lsof -a -p "$pid" -d cwd -Fn 2>/dev/null | sed -n 's/^n//p' | head -n 1
}

kill_port_listeners() {
  local port="$1"
  local pids pid
  pids="$(listener_pids "$port" | tr '\n' ' ')"
  if [ -z "${pids// }" ]; then
    return 0
  fi

  echo "freeing port $port: killing listener pid(s): $pids"
  # shellcheck disable=SC2086
  kill $pids >/dev/null 2>&1 || true
  for _ in $(seq 1 20); do
    if [ -z "$(listener_pids "$port" | tr '\n' ' ')" ]; then
      return 0
    fi
    sleep 0.25
  done
  for pid in $(listener_pids "$port"); do
    kill -9 "$pid" >/dev/null 2>&1 || true
  done
}

supervisor_pids() {
  ps -axo pid=,command= \
    | awk -v me="$$" '/run-local-stack-supervised[.]sh/ && $1 != me { print $1 }'
}

kill_orphan_supervisors() {
  local pids pid
  pids="$(supervisor_pids | tr '\n' ' ')"
  if [ -z "${pids// }" ]; then
    return 0
  fi

  echo "stopping local stack supervisor pid(s): $pids"
  # shellcheck disable=SC2086
  kill $pids >/dev/null 2>&1 || true
  for _ in $(seq 1 20); do
    if [ -z "$(supervisor_pids | tr '\n' ' ')" ]; then
      return 0
    fi
    sleep 0.25
  done
  for pid in $(supervisor_pids); do
    kill -9 "$pid" >/dev/null 2>&1 || true
  done
}

show_port_owner() {
  local port="$1"
  local pids pid cwd
  pids="$(listener_pids "$port" | tr '\n' ' ')"
  if [ -z "${pids// }" ]; then
    echo "port $port: free"
    return 0
  fi

  for pid in $pids; do
    cwd="$(pid_cwd "$pid")"
    if [[ "$cwd" == "$repo_root"* ]]; then
      echo "port $port: pid $pid cwd=$cwd (current repo)"
    else
      echo "port $port: pid $pid cwd=${cwd:-unknown} (not current repo)"
    fi
  done
}

status() {
  echo "LaunchAgent: $plist_path"
  echo "repo: $repo_root"
  if command -v git >/dev/null 2>&1 && git -C "$repo_root" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    echo "repo commit: $(git -C "$repo_root" rev-parse --short HEAD)"
  fi
  if [ -f "$plist_path" ]; then
    if plist_points_to_repo; then
      echo "plist: current repo"
    else
      echo "plist: stale or points at a different repo"
    fi
  else
    echo "plist: missing"
  fi
  if is_loaded; then
    echo "launchd: loaded"
    launchctl print "$domain/$label" | sed -n '1,35p'
  else
    echo "launchd: not loaded"
  fi
  echo
  echo "Ports:"
  show_port_owner "$api_port"
  show_port_owner "$web_port"
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
    kill_orphan_supervisors
    kill_port_listeners "$api_port"
    kill_port_listeners "$web_port"
    bootstrap
    status
    ;;
  start)
    if ! plist_points_to_repo; then
      write_plist
      bootout_if_loaded
      kill_orphan_supervisors
      kill_port_listeners "$api_port"
      kill_port_listeners "$web_port"
      bootstrap
    else
      if is_loaded; then
        launchctl kickstart "$domain/$label" >/dev/null 2>&1 || true
      else
        kill_orphan_supervisors
        kill_port_listeners "$api_port"
        kill_port_listeners "$web_port"
        bootstrap
      fi
    fi
    status
    ;;
  stop)
    bootout_if_loaded
    kill_orphan_supervisors
    kill_port_listeners "$api_port"
    kill_port_listeners "$web_port"
    status
    ;;
  restart)
    if [ ! -f "$plist_path" ]; then
      write_plist
    fi
    bootout_if_loaded
    kill_orphan_supervisors
    kill_port_listeners "$api_port"
    kill_port_listeners "$web_port"
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
    kill_orphan_supervisors
    kill_port_listeners "$api_port"
    kill_port_listeners "$web_port"
    rm -f "$plist_path"
    status
    ;;
  *)
    usage
    exit 2
    ;;
esac
