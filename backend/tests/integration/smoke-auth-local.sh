#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
source "$repo_root/tools/postgres-ci.sh"

container_name="goatos-auth-smoke-$$"
image="${GOATOS_POSTGRES_IMAGE:-postgres:16.9-alpine}"
db_name="goatos"
db_user="postgres"
api_port="${GOATOS_AUTH_SMOKE_API_PORT:-18080}"
api_log="$(mktemp "${TMPDIR:-/tmp}/goatos-auth-smoke-api.XXXXXX.log")"
api_pid=""

tenant_id="00000000-0000-4000-8000-000000000001"
granted_user_id="90000000-0000-4000-8000-000000000101"
denied_user_id="90000000-0000-4000-8000-000000000102"

cleanup() {
  if [ -n "$api_pid" ]; then
    kill "$api_pid" >/dev/null 2>&1 || true
    wait "$api_pid" >/dev/null 2>&1 || true
  fi
  docker rm -f "$container_name" >/dev/null 2>&1 || true
  rm -f "$api_log"
}
trap cleanup EXIT

run_psql() {
  postgres_ci_psql "$container_name" "$db_user" "$db_name" "$@"
}

apply_goose_up() {
  local migration="$1"
  awk '
    /^-- \+goose Up/ { in_up = 1; next }
    /^-- \+goose Down/ { in_up = 0 }
    in_up { print }
  ' "$migration" | run_psql >/dev/null
}

expect_status() {
  local label="$1"
  local token="$2"
  local want="$3"
  local status

  status="$(
    curl -sS -o /tmp/goatos-auth-smoke-response.json -w '%{http_code}' \
      -H "Authorization: Bearer $token" \
      "http://127.0.0.1:$api_port/goats/search?limit=10"
  )"
  if [ "$status" != "$want" ]; then
    cat /tmp/goatos-auth-smoke-response.json >&2 || true
    echo "$label: expected HTTP $want, got $status" >&2
    exit 1
  fi
}

docker run --rm --name "$container_name" \
  -e POSTGRES_PASSWORD=goatos \
  -e POSTGRES_DB="$db_name" \
  -p 127.0.0.1::5432 \
  -d "$image" >/dev/null

postgres_ci_wait_ready "$container_name" "$db_user" "$db_name"
db_port="$(docker port "$container_name" 5432/tcp | sed -E 's/.*:([0-9]+)$/\1/')"

while IFS= read -r migration; do
  apply_goose_up "$migration"
done < <(find "$repo_root/backend/migrations/postgres" -maxdepth 1 -type f -name '*.sql' | sort)

export GOATOS_ENV="local"
export GOATOS_AUTH_MODE="bearer"
export GOATOS_AUTH_ISSUER="goatos-local-smoke"
export GOATOS_AUTH_AUDIENCE="goatos-api"
export GOATOS_AUTH_HS256_SECRET="goatos-local-smoke-secret-32-bytes-min"
export GOATOS_AUTH_MAX_TOKEN_TTL="24h"
export GOATOS_HTTP_ADDR="127.0.0.1:$api_port"
export DATABASE_URL="postgres://postgres:goatos@127.0.0.1:$db_port/$db_name?sslmode=disable"

(
  cd "$repo_root/backend"
  go run ./cmd/api
) >"$api_log" 2>&1 &
api_pid="$!"

for _ in $(seq 1 60); do
  if curl -fsS "http://127.0.0.1:$api_port/readyz" >/dev/null 2>&1; then
    break
  fi
  if ! kill -0 "$api_pid" >/dev/null 2>&1; then
    cat "$api_log" >&2
    echo "API exited before readiness" >&2
    exit 1
  fi
  sleep 1
done
curl -fsS "http://127.0.0.1:$api_port/readyz" >/dev/null

(
  cd "$repo_root/backend"
  go run ./cmd/seed-dev-grant -tenant-id "$tenant_id" -user-id "$granted_user_id" -role operator
) >/dev/null

granted_token="$(
  cd "$repo_root/backend"
  go run ./cmd/mint-dev-token -tenant-id "$tenant_id" -user-id "$granted_user_id" -ttl 30m
)"
denied_token="$(
  cd "$repo_root/backend"
  go run ./cmd/mint-dev-token -tenant-id "$tenant_id" -user-id "$denied_user_id" -ttl 30m
)"

expect_status "granted operator search" "$granted_token" "200"
expect_status "valid token without grant" "$denied_token" "403"

GOATOS_API_BASE_URL="http://127.0.0.1:$api_port" GOATOS_BEARER_TOKEN="$granted_token" \
  npm --prefix "$repo_root/apps/admin-web" run typecheck >/dev/null

echo "Auth smoke passed: bearer token, local tenant grant, and admin-web client import are wired to one shared GOATOS_AUTH_* config."
