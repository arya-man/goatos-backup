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
  local port=""

  if command -v docker >/dev/null 2>&1; then
    port="$(
      docker ps --filter "name=goatos-local-current" --format '{{.Ports}}' 2>/dev/null \
        | sed -nE 's/.*127\.0\.0\.1:([0-9]+)->5432\/tcp.*/\1/p' \
        | head -n 1
    )"

    if [ -z "$port" ]; then
      port="$(
        docker ps --format '{{.Names}} {{.Ports}}' 2>/dev/null \
          | grep 'goatos' \
          | sed -nE 's/.*127\.0\.0\.1:([0-9]+)->5432\/tcp.*/\1/p' \
          | head -n 1
      )"
    fi
  fi

  if [ -n "$port" ]; then
    printf 'postgres://postgres:goatos@127.0.0.1:%s/goatos?sslmode=disable\n' "$port"
  fi
}

export DATABASE_URL="${DATABASE_URL:-$(detect_docker_database_url)}"
export DATABASE_URL="${DATABASE_URL:-postgres://postgres:goatos@127.0.0.1:5432/goatos?sslmode=disable}"
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

(
  cd "$repo_root/backend"
  go run ./cmd/seed-dev-grant \
    -tenant-id "$GOATOS_TENANT_ID" \
    -user-id "$local_user_id" \
    -role "$local_role"
)

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

echo "Starting Mesha admin-web at http://127.0.0.1:3300"
echo "API log: $api_log"
npm --prefix "$repo_root/apps/admin-web" run dev:local
