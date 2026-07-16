#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

export PATH="${GOATOS_LOCAL_SERVICE_PATH:-/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin:$PATH}"

host="${GOATOS_LOCAL_HOST:-127.0.0.1}"
api_port="${GOATOS_LOCAL_API_PORT:-8080}"
web_port="${GOATOS_LOCAL_WEB_PORT:-3300}"
api_base_url="${GOATOS_API_BASE_URL:-http://$host:$api_port}"

log_dir="$repo_root/.codex-goatos-render/logs"
api_log="$log_dir/local-api.log"
web_log="$log_dir/local-admin-web.log"
projection_log="$log_dir/local-projections.log"
supervisor_log="$log_dir/local-stack-supervisor.log"

api_pid=""
web_pid=""
projection_pid=""
stop_requested="0"

timestamp() {
  date -u +"%Y-%m-%dT%H:%M:%SZ"
}

log() {
  mkdir -p "$log_dir"
  printf '%s %s\n' "$(timestamp)" "$*" | tee -a "$supervisor_log"
}

detect_docker_database_url() {
  local candidates=""

  if command -v docker >/dev/null 2>&1; then
    candidates="$(
      docker ps --format '{{.Names}} {{.Ports}}' 2>/dev/null \
        | awk '
            $1 == "goatos-local-current" {
              if (match($0, /127\.0\.0\.1:[0-9]+->5432\/tcp/)) {
                print $0
              }
            }
          '
    )"
  fi

  local count
  count="$(printf '%s\n' "$candidates" | sed '/^$/d' | wc -l | tr -d ' ')"
  if [ "$count" -gt 1 ]; then
    echo "Multiple Goat OS local app Postgres containers are running; refusing to guess DATABASE_URL." >&2
    printf '%s\n' "$candidates" >&2
    echo "Stop the extra container or set DATABASE_URL explicitly." >&2
    exit 2
  fi

  local port
  port="$(printf '%s\n' "$candidates" | sed -nE 's/.*127\.0\.0\.1:([0-9]+)->5432\/tcp.*/\1/p' | head -n 1)"
  if [ -n "$port" ]; then
    printf 'postgres://postgres:goatos@127.0.0.1:%s/goatos?sslmode=disable\n' "$port"
  fi
}

export GOATOS_ENV="${GOATOS_ENV:-local}"
export GOATOS_AUTH_MODE="${GOATOS_AUTH_MODE:-bearer}"
export GOATOS_AUTH_ISSUER="${GOATOS_AUTH_ISSUER:-goatos-local}"
export GOATOS_AUTH_AUDIENCE="${GOATOS_AUTH_AUDIENCE:-goatos-api}"
export GOATOS_AUTH_HS256_SECRET="${GOATOS_AUTH_HS256_SECRET:-goatos-local-dev-secret-32-bytes-min}"
export GOATOS_AUTH_MAX_TOKEN_TTL="${GOATOS_AUTH_MAX_TOKEN_TTL:-24h}"
export GOATOS_HTTP_ADDR="${GOATOS_HTTP_ADDR:-$host:$api_port}"
# DRV-R3 local-DB mutation trust decision + guard (shared, tested: tools/dev/test-db-mutation-guard.sh).
# shellcheck source=tools/dev/lib/db-mutation-guard.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib/db-mutation-guard.sh"
resolve_database_url detect_docker_database_url
export GOATOS_API_BASE_URL="$api_base_url"
export GOATOS_TENANT_ID="${GOATOS_TENANT_ID:-${GOATOS_LOCAL_TENANT_ID:-00000000-0000-4000-8000-000000000001}}"

local_user_id="${GOATOS_LOCAL_USER_ID:-90000000-0000-4000-8000-000000000101}"
local_role="${GOATOS_LOCAL_ROLE:-ceo_internal}"

port_busy() {
  local target_host="$1"
  local target_port="$2"
  nc -z "$target_host" "$target_port" >/dev/null 2>&1
}

port_listener_pids() {
  local target_port="$1"
  lsof -nP -tiTCP:"$target_port" -sTCP:LISTEN 2>/dev/null || true
}

log_port_listener_context() {
  local target_port="$1"
  local pid
  for pid in $(port_listener_pids "$target_port"); do
    local cwd
    cwd="$(lsof -a -p "$pid" -d cwd -Fn 2>/dev/null | sed -n 's/^n//p' | head -n 1)"
    log "Port $target_port listener pid=$pid cwd=${cwd:-unknown}."
  done
}

api_ready() {
  curl -fsS "$api_base_url/readyz" >/dev/null 2>&1
}

web_ready() {
  curl -fsS "http://$host:$web_port/login" >/dev/null 2>&1
}

kill_pid() {
  local pid="$1"
  if [ -z "$pid" ]; then
    return
  fi
  if kill -0 "$pid" >/dev/null 2>&1; then
    kill "$pid" >/dev/null 2>&1 || true
    for _ in $(seq 1 20); do
      if ! kill -0 "$pid" >/dev/null 2>&1; then
        return
      fi
      sleep 0.25
    done
    kill -9 "$pid" >/dev/null 2>&1 || true
  fi
}

cleanup() {
  kill_pid "$projection_pid"
  kill_pid "$web_pid"
  kill_pid "$api_pid"
  projection_pid=""
  web_pid=""
  api_pid=""
}

on_signal() {
  stop_requested="1"
  log "Stop requested; shutting down local stack children."
  cleanup
  exit 0
}

trap on_signal INT TERM

wait_for_api() {
  for _ in $(seq 1 90); do
    if api_ready; then
      return 0
    fi
    if [ -n "$api_pid" ] && ! kill -0 "$api_pid" >/dev/null 2>&1; then
      tail -n 120 "$api_log" >&2 || true
      log "Goat OS API exited before readiness."
      return 1
    fi
    sleep 1
  done
  tail -n 120 "$api_log" >&2 || true
  log "Timed out waiting for Goat OS API at $api_base_url/readyz."
  return 1
}

wait_for_web() {
  for _ in $(seq 1 90); do
    if web_ready; then
      return 0
    fi
    if [ -n "$web_pid" ] && ! kill -0 "$web_pid" >/dev/null 2>&1; then
      tail -n 120 "$web_log" >&2 || true
      log "Mesha admin-web exited before readiness."
      return 1
    fi
    sleep 1
  done
  tail -n 120 "$web_log" >&2 || true
  log "Timed out waiting for Mesha admin-web at http://$host:$web_port/login."
  return 1
}

seed_dev_grant() {
  (
    cd "$repo_root/backend"
    go run ./cmd/seed-dev-grant \
      -tenant-id "$GOATOS_TENANT_ID" \
      -user-id "$local_user_id" \
      -role "$local_role"
  ) >>"$supervisor_log" 2>&1
}

migrate_local_database() {
  log "Applying Goat OS local migrations to $DATABASE_URL."
  (
    cd "$repo_root/backend"
    go run ./cmd/migrate -timeout=10m
  ) >>"$supervisor_log" 2>&1
}

seed_closeout_if_present() {
  if [ ! -f "$repo_root/tools/dev/seed-closeout.sh" ]; then
    return 0
  fi
  log "Running Goat OS local seed closeout for tenant $GOATOS_TENANT_ID."
  (
    cd "$repo_root"
    bash tools/dev/seed-closeout.sh
  ) >>"$supervisor_log" 2>&1
}

start_api() {
  if api_ready; then
    log "Refusing to reuse an existing Goat OS API at $api_base_url; restart the local service so the current checkout owns the port."
    log_port_listener_context "$api_port"
    return 1
  fi

  if port_busy "$host" "$api_port"; then
    log "Port $api_port is in use but $api_base_url/readyz is not healthy."
    lsof -nP "-iTCP:$api_port" -sTCP:LISTEN >>"$supervisor_log" 2>&1 || true
    return 1
  fi

  log "Starting Goat OS API at $GOATOS_HTTP_ADDR."
  (
    cd "$repo_root/backend"
    go run ./cmd/api
  ) >>"$api_log" 2>&1 &
  api_pid="$!"
  wait_for_api
}

start_web() {
  if web_ready; then
    log "Refusing to reuse an existing Mesha admin-web at http://$host:$web_port; restart the local service so the current checkout owns the port."
    log_port_listener_context "$web_port"
    return 1
  fi

  if port_busy "$host" "$web_port"; then
    log "Port $web_port is in use but http://$host:$web_port/login is not healthy."
    lsof -nP "-iTCP:$web_port" -sTCP:LISTEN >>"$supervisor_log" 2>&1 || true
    return 1
  fi

  log "Starting Mesha admin-web at http://$host:$web_port/."
  # Single-owner prep (DRV-R3): DB migrate/seed/closeout already ran once before the supervise loop, so
  # the dev:local wrapper must NOT repeat them on any (re)start; it still mints its own dev token.
  GOATOS_LOCAL_DB_PREPARED=1 npm --prefix "$repo_root/apps/admin-web" run dev:local >>"$web_log" 2>&1 &
  web_pid="$!"
  wait_for_web
}

start_projection_refresher() {
  if [ -n "$projection_pid" ] && kill -0 "$projection_pid" >/dev/null 2>&1; then
    return 0
  fi

  local interval="${GOATOS_LOCAL_PROJECTION_REFRESH_SECONDS:-120}"
  log "Starting local read-model projection refresher every ${interval}s."
  (
    cd "$repo_root/backend"
    run_projection_refresh() {
      local label="$1"
      shift
      if ! "$@"; then
        printf '%s projection refresh failed: %s\n' "$(timestamp)" "$label"
      fi
    }
    # DRV-R3b — sleep FIRST so neither the initial start nor a monitor-triggered restart of this
    # refresher fires an immediate projection recompute (a DB mutation). Prep already reprojected once
    # (seed closeout) before the supervise loop; periodic refresh then runs only after each interval.
    while true; do
      sleep "$interval"
      printf '%s refreshing local read-model projections\n' "$(timestamp)"
      run_projection_refresh vaccination_shed go run ./cmd/vaccination-shed-projection-recompute -tenant-id "$GOATOS_TENANT_ID"
      run_projection_refresh vaccination_execution go run ./cmd/vaccination-execution-projection-recompute -tenant-id "$GOATOS_TENANT_ID"
      run_projection_refresh vaccination_operations go run ./cmd/vaccination-operations-projection-recompute -tenant-id "$GOATOS_TENANT_ID"
      run_projection_refresh process_integrity go run ./cmd/process-integrity-projection-recompute -tenant-id "$GOATOS_TENANT_ID"
    done
  ) >>"$projection_log" 2>&1 &
  projection_pid="$!"
}

monitor_stack() {
  local api_failures=0
  local web_failures=0

  log "Local stack ready: API $api_base_url, admin-web http://$host:$web_port/."
  while [ "$stop_requested" = "0" ]; do
    if [ -n "$api_pid" ] && ! kill -0 "$api_pid" >/dev/null 2>&1; then
      log "Goat OS API process exited; restarting stack."
      return 1
    fi
    if [ -n "$web_pid" ] && ! kill -0 "$web_pid" >/dev/null 2>&1; then
      log "Mesha admin-web process exited; restarting stack."
      return 1
    fi
    if [ -n "$projection_pid" ] && ! kill -0 "$projection_pid" >/dev/null 2>&1; then
      log "Read-model projection refresher exited; restarting stack."
      return 1
    fi

    if api_ready; then
      api_failures=0
    else
      api_failures=$((api_failures + 1))
    fi
    if web_ready; then
      web_failures=0
    else
      web_failures=$((web_failures + 1))
    fi

    if [ "$api_failures" -ge 3 ]; then
      log "Goat OS API failed health checks; restarting stack."
      return 1
    fi
    if [ "$web_failures" -ge 3 ]; then
      log "Mesha admin-web failed health checks; restarting stack."
      return 1
    fi

    sleep "${GOATOS_LOCAL_SERVICE_HEALTH_INTERVAL_SECONDS:-5}"
  done
}

restart_delay="${GOATOS_LOCAL_SERVICE_RESTART_DELAY_SECONDS:-5}"
mkdir -p "$log_dir"
touch "$api_log" "$web_log" "$projection_log" "$supervisor_log"

log "Starting durable Goat OS local stack supervisor."
log "Database URL target: $DATABASE_URL"

# DRV-R3 single-owner prep: migrate + seed + closeout run EXACTLY ONCE here (guarded), never inside the
# restart loop below — a monitor-triggered restart must only relaunch the servers, not re-mutate the DB.
assert_mutable_local_db
if ! (migrate_local_database && seed_dev_grant && seed_closeout_if_present); then
  log "Local database preparation failed; not starting the stack."
  exit 1
fi

while [ "$stop_requested" = "0" ]; do
  cleanup
  if start_api && start_projection_refresher && start_web && monitor_stack; then
    break
  fi
  cleanup
  if [ "$stop_requested" = "1" ]; then
    break
  fi
  log "Restarting local stack in ${restart_delay}s."
  sleep "$restart_delay"
done
