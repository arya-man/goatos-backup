#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

host="${GOATOS_LOCAL_HOST:-127.0.0.1}"
api_port="${GOATOS_LOCAL_API_PORT:-8080}"
api_base_url="${GOATOS_API_BASE_URL:-http://$host:$api_port}"
backend_started=""
api_log_dir="$repo_root/.codex-goatos-render/logs"
api_log="$api_log_dir/local-api.log"

export GOATOS_ENV="${GOATOS_ENV:-local}"
export GOATOS_AUTH_MODE="${GOATOS_AUTH_MODE:-bearer}"
export GOATOS_AUTH_ISSUER="${GOATOS_AUTH_ISSUER:-goatos-local}"
export GOATOS_AUTH_AUDIENCE="${GOATOS_AUTH_AUDIENCE:-goatos-api}"
export GOATOS_AUTH_HS256_SECRET="${GOATOS_AUTH_HS256_SECRET:-goatos-local-dev-secret-32-bytes-min}"
export GOATOS_AUTH_MAX_TOKEN_TTL="${GOATOS_AUTH_MAX_TOKEN_TTL:-24h}"
export GOATOS_HTTP_ADDR="${GOATOS_HTTP_ADDR:-$host:$api_port}"
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

# DRV-R3 local-DB mutation trust decision + guard (shared, tested: tools/dev/test-db-mutation-guard.sh).
# shellcheck source=tools/dev/lib/db-mutation-guard.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib/db-mutation-guard.sh"
resolve_database_url detect_docker_database_url
export GOATOS_API_BASE_URL="$api_base_url"
export GOATOS_TENANT_ID="${GOATOS_TENANT_ID:-${GOATOS_LOCAL_TENANT_ID:-00000000-0000-4000-8000-000000000001}}"

local_user_id="${GOATOS_LOCAL_USER_ID:-90000000-0000-4000-8000-000000000101}"
local_role="${GOATOS_LOCAL_ROLE:-ceo_internal}"

api_ready() {
  curl -fsS "$api_base_url/readyz" >/dev/null 2>&1
}

wait_for_api() {
  for _ in $(seq 1 60); do
    if api_ready; then
      return 0
    fi
    if ! kill -0 "$backend_started" >/dev/null 2>&1; then
      cat "$api_log" >&2 || true
      echo "Goat OS API exited before readiness." >&2
      exit 1
    fi
    sleep 1
  done
  cat "$api_log" >&2 || true
  echo "Timed out waiting for Goat OS API at $api_base_url/readyz." >&2
  exit 1
}

port_busy() {
  local target_host="$1"
  local target_port="$2"
  nc -z "$target_host" "$target_port" >/dev/null 2>&1
}

cleanup() {
  if [ -n "$backend_started" ]; then
    kill "$backend_started" >/dev/null 2>&1 || true
    wait "$backend_started" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

mkdir -p "$api_log_dir"

migrate_local_database() {
  echo "Applying Goat OS local migrations to $DATABASE_URL"
  (
    cd "$repo_root/backend"
    go run ./cmd/migrate -timeout=10m
  )
}

seed_closeout_if_present() {
  if [ ! -f "$repo_root/tools/dev/seed-closeout.sh" ]; then
    return 0
  fi
  echo "Running Goat OS local seed closeout for tenant $GOATOS_TENANT_ID"
  (
    cd "$repo_root"
    bash tools/dev/seed-closeout.sh
  )
}

assert_mutable_local_db
migrate_local_database
(
  cd "$repo_root/backend"
  go run ./cmd/seed-dev-grant \
    -tenant-id "$GOATOS_TENANT_ID" \
    -user-id "$local_user_id" \
    -role "$local_role" \
    -department "${GOATOS_LOCAL_DEPARTMENT:-leadership}"
)
seed_closeout_if_present

if api_ready; then
  echo "Using existing Goat OS API at $api_base_url"
else
  if port_busy "$host" "$api_port"; then
    echo "Port $api_port is already in use, but $api_base_url/readyz is not healthy." >&2
    echo "Stop the existing process on $host:$api_port, or set GOATOS_LOCAL_API_PORT." >&2
    lsof -nP "-iTCP:$api_port" -sTCP:LISTEN >&2 || true
    exit 1
  fi

  echo "Starting Goat OS API at $GOATOS_HTTP_ADDR"
  (
    cd "$repo_root/backend"
    go run ./cmd/api
  ) >"$api_log" 2>&1 &
  backend_started="$!"
  wait_for_api
fi

echo "Starting Mesha admin-web at http://127.0.0.1:3300/"
echo "API log: $api_log"
# Single-owner prep (DRV-R3): this script already migrated + seeded + ran closeout above, so tell the
# dev:local wrapper NOT to repeat those DB mutations (it still mints its own dev token).
GOATOS_LOCAL_DB_PREPARED=1 npm --prefix "$repo_root/apps/admin-web" run dev:local
