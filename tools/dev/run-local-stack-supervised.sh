#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

assert_not_temp_checkout() {
  if [ "${GOATOS_ALLOW_TEMP_WORKTREE_LOCAL_STACK:-0}" = "1" ]; then
    return
  fi
  local top_level
  top_level="$(git -C "$repo_root" rev-parse --show-toplevel 2>/dev/null || printf '%s' "$repo_root")"
  case "$top_level" in
    /tmp/*|/private/tmp/*|/var/folders/*)
      cat >&2 <<EOF
Refusing to start Goat OS local stack from a temporary worktree:
  $top_level

Start the local stack from the canonical checkout, or set
GOATOS_ALLOW_TEMP_WORKTREE_LOCAL_STACK=1 for an explicit throwaway experiment.
EOF
      exit 2
      ;;
  esac
}

shared_stack="${GOATOS_SHARED_LOCAL_STACK:-1}"
if [ "$shared_stack" = "1" ]; then
  unset GOATOS_ALLOW_STALE_LOCAL_STACK GOATOS_ALLOW_TEMP_WORKTREE_LOCAL_STACK
fi
assert_not_temp_checkout

export PATH="${GOATOS_LOCAL_SERVICE_PATH:-/opt/homebrew/opt/libpq/bin:/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin:$PATH}"

if [ "$shared_stack" = "1" ]; then
  # The browser-visible stack is a fixed contract. Ambient variables from an
  # agent shell must never redirect it to an E2E DB, port, or backend.
  host="127.0.0.1"
  api_port="8080"
  web_port="3300"
  api_base_url="http://127.0.0.1:8080"
  unset DATABASE_URL GOATOS_E2E_DATABASE_URL GOATOS_LOCAL_STACK_DATABASE_URL GOATOS_ALLOW_CUSTOM_LOCAL_STACK_DB
  # The canonical vaccination calendar read is intentionally bounded but can
  # cross the production-oriented 3s query deadline while a developer machine
  # is compiling/tests are running. Pin shared-local headroom so valid rows do
  # not become a misleading `calendar request failed` empty screen under load.
  # Isolated E2E stacks keep their own explicit timeout contract.
  export GOATOS_PG_QUERY_TIMEOUT="15s"
else
  host="${GOATOS_LOCAL_HOST:-127.0.0.1}"
  api_port="${GOATOS_LOCAL_API_PORT:?isolated stack requires GOATOS_LOCAL_API_PORT}"
  web_port="${GOATOS_LOCAL_WEB_PORT:?isolated stack requires GOATOS_LOCAL_WEB_PORT}"
  api_base_url="${GOATOS_API_BASE_URL:-http://$host:$api_port}"
  if [ "$api_port" = "8080" ] || [ "$web_port" = "3300" ]; then
    echo "Isolated local stacks must not claim shared ports 8080 or 3300." >&2
    exit 2
  fi
fi

log_dir="$repo_root/.codex-goatos-render/logs"
api_log="$log_dir/local-api.log"
web_log="$log_dir/local-admin-web.log"
supervisor_log="$log_dir/local-stack-supervisor.log"

api_pid=""
web_pid=""
stop_requested="0"
origin_main_restart_requested="0"

timestamp() {
  date -u +"%Y-%m-%dT%H:%M:%SZ"
}

log() {
  mkdir -p "$log_dir"
  printf '%s %s\n' "$(timestamp)" "$*" | tee -a "$supervisor_log"
}

fetch_origin_main_with_gh() {
  local credential_helper
  # This is a literal Git credential-helper shell function.
  # shellcheck disable=SC2016
  credential_helper='!f() { test "$1" = get || exit 0; echo username=x-access-token; printf "password="; gh auth token; }; f'
  if command -v gh >/dev/null 2>&1; then
    git -C "$repo_root" \
      -c credential.helper= \
      -c "credential.helper=$credential_helper" \
      fetch --quiet origin main
    return
  fi
  git -C "$repo_root" fetch --quiet origin main
}

assert_clean_tracked_checkout() {
  local dirty
  dirty="$(git -C "$repo_root" status --porcelain --untracked-files=no)"
  if [ -n "$dirty" ]; then
    log "Refusing shared local stack: tracked files are modified in $repo_root."
    return 1
  fi
}

sync_exact_origin_main() {
  if [ "$shared_stack" != "1" ]; then
    return 0
  fi

  local remote head origin_main
  remote="$(git -C "$repo_root" remote get-url origin 2>/dev/null || true)"
  case "$remote" in
    *github.com/vgoats/goatos*) ;;
    *)
      log "Refusing shared local stack: origin is ${remote:-missing}, expected vgoats/goatos."
      return 1
      ;;
  esac
  assert_clean_tracked_checkout || return 1
  if ! fetch_origin_main_with_gh; then
    log "Refusing shared local stack: could not authenticate and fetch origin/main."
    return 1
  fi

  head="$(git -C "$repo_root" rev-parse HEAD)"
  origin_main="$(git -C "$repo_root" rev-parse refs/remotes/origin/main)"
  if [ "$head" != "$origin_main" ]; then
    if ! git -C "$repo_root" merge-base --is-ancestor "$head" "$origin_main"; then
      log "Refusing shared local stack: HEAD ${head:0:12} is not a clean fast-forward ancestor of origin/main ${origin_main:0:12}."
      return 1
    fi
    log "Fast-forwarding shared local stack ${head:0:12} -> origin/main ${origin_main:0:12}."
    git -C "$repo_root" merge --ff-only --quiet refs/remotes/origin/main
    log "Re-executing the supervisor from exact origin/main before database preparation."
    exec /bin/bash "$repo_root/tools/dev/run-local-stack-supervised.sh"
  fi

  assert_clean_tracked_checkout || return 1
  export GOATOS_ORIGIN_MAIN_PREVERIFIED=1
}

origin_main_drifted() {
  if [ "$shared_stack" != "1" ]; then
    return 1
  fi
  if ! fetch_origin_main_with_gh; then
    log "Lost authenticated origin/main verification; stopping the shared stack fail-closed."
    return 0
  fi
  [ "$(git -C "$repo_root" rev-parse HEAD)" != "$(git -C "$repo_root" rev-parse refs/remotes/origin/main)" ]
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
export GOATOS_LOCAL_MEDIA_SIGNING_SECRET="${GOATOS_LOCAL_MEDIA_SIGNING_SECRET:-goatos-local-media-secret-32-bytes-min}"
if [ "$shared_stack" = "1" ]; then
  export GOATOS_HTTP_ADDR="$host:$api_port"
else
  export GOATOS_HTTP_ADDR="${GOATOS_HTTP_ADDR:-$host:$api_port}"
fi
# DRV-R3 local-DB mutation trust decision + guard (shared, tested: tools/dev/test-db-mutation-guard.sh).
# shellcheck source=tools/dev/lib/db-mutation-guard.sh
# shellcheck disable=SC1091
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

calendar_data_plane_ready() {
  if [ "$shared_stack" != "1" ]; then
    return 0
  fi
  local token today
  token="$(
    (
      cd "$repo_root/backend"
      go run ./cmd/mint-dev-token \
        -tenant-id "$GOATOS_TENANT_ID" \
        -user-id "$local_user_id" \
        -ttl 10m
    ) 2>>"$supervisor_log"
  )"
  today="$(date +%F)"
  curl --max-time 15 -fsS \
    -H "Authorization: Bearer $token" \
    -H "X-Tenant-ID: $GOATOS_TENANT_ID" \
    "$api_base_url/calendar/vaccination/events?date_from=$today&limit=1" \
    >/dev/null 2>&1
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

kill_process_tree() {
  local root_pid="$1"
  [ -n "$root_pid" ] || return 0
  local child
  for child in $(pgrep -P "$root_pid" 2>/dev/null || true); do
    kill_process_tree "$child"
  done
  kill_pid "$root_pid"
}

kill_owned_port_listeners() {
  local target_port="$1"
  local pid cwd
  for pid in $(port_listener_pids "$target_port"); do
    cwd="$(lsof -a -p "$pid" -d cwd -Fn 2>/dev/null | sed -n 's/^n//p' | head -n 1)"
    case "$cwd" in
      "$repo_root"|"$repo_root"/*) kill_process_tree "$pid" ;;
    esac
  done
}

cleanup() {
  kill_process_tree "$web_pid"
  kill_process_tree "$api_pid"
  # `go run` and npm/Next can leave compiled or worker descendants listening
  # after their immediate parent exits. Reap only listeners owned by this repo;
  # never scan or terminate isolated E2E stacks on other ports/checkouts.
  kill_owned_port_listeners "$web_port"
  kill_owned_port_listeners "$api_port"
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
  wait_for_api || return 1
  if ! calendar_data_plane_ready; then
    tail -n 120 "$api_log" >&2 || true
    log "Goat OS API readiness passed but the authenticated vaccination calendar data plane failed."
    return 1
  fi
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

monitor_stack() {
  local api_failures=0
  local web_failures=0
  local origin_elapsed=0
  local health_interval="${GOATOS_LOCAL_SERVICE_HEALTH_INTERVAL_SECONDS:-5}"
  local origin_interval="${GOATOS_LOCAL_ORIGIN_CHECK_INTERVAL_SECONDS:-30}"

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

    sleep "$health_interval"
    origin_elapsed=$((origin_elapsed + health_interval))
    if [ "$shared_stack" = "1" ] && [ "$origin_elapsed" -ge "$origin_interval" ]; then
      origin_elapsed=0
      if origin_main_drifted; then
        origin_main_restart_requested="1"
        log "origin/main advanced or could not be verified; stopping both shared services for an exact-main restart."
        return 1
      fi
    fi
  done
}

restart_delay="${GOATOS_LOCAL_SERVICE_RESTART_DELAY_SECONDS:-5}"
mkdir -p "$log_dir"
touch "$api_log" "$web_log" "$supervisor_log"

log "Starting durable Goat OS local stack supervisor."
log "Database URL target: $DATABASE_URL"

if ! sync_exact_origin_main; then
  log "Shared origin/main synchronization failed; not starting FE, BE, or touching the database."
  exit 1
fi

# DRV-R3 single-owner prep: migrate + seed + closeout run EXACTLY ONCE here (guarded), never inside the
# restart loop below — a monitor-triggered restart must only relaunch the servers, not re-mutate the DB.
assert_mutable_local_db
if ! (migrate_local_database && seed_dev_grant && seed_closeout_if_present); then
  log "Local database preparation failed; not starting the stack."
  exit 1
fi

while [ "$stop_requested" = "0" ]; do
  cleanup
  if start_api && start_web && monitor_stack; then
    break
  fi
  cleanup
  if [ "$origin_main_restart_requested" = "1" ]; then
    log "Exiting supervisor so the persistent service can fast-forward and restart atomically."
    exit 75
  fi
  if [ "$stop_requested" = "1" ]; then
    break
  fi
  log "Restarting local stack in ${restart_delay}s."
  sleep "$restart_delay"
done
