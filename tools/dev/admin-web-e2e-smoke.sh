#!/usr/bin/env bash
# Non-destructive local admin-web E2E smoke.
#
# This is a runner for the currently implemented proof paths:
#   1. data-plane vaccination chain proof
#   2. live admin-web visual/click smoke
#
# It does NOT yet cover the full four-goat procurement negative matrix from
# context/execution/admin-web-e2e-checklist.md.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
backend_dir="$repo_root/backend"
run_id="${GOATOS_E2E_RUN_ID:-E2E-$(date +%Y%m%d-%H%M%S)}"
report_dir="${GOATOS_E2E_REPORT_DIR:-$repo_root/.codex-goatos-render/e2e-smoke/$run_id}"

export GOATOS_ENV="${GOATOS_ENV:-local}"
export GOATOS_AUTH_MODE="${GOATOS_AUTH_MODE:-bearer}"
export GOATOS_AUTH_ISSUER="${GOATOS_AUTH_ISSUER:-goatos-local}"
export GOATOS_AUTH_AUDIENCE="${GOATOS_AUTH_AUDIENCE:-goatos-api}"
export GOATOS_AUTH_HS256_SECRET="${GOATOS_AUTH_HS256_SECRET:-goatos-local-dev-secret-32-bytes-min}"
export GOATOS_AUTH_MAX_TOKEN_TTL="${GOATOS_AUTH_MAX_TOKEN_TTL:-24h}"
export GOATOS_TENANT_ID="${GOATOS_TENANT_ID:-00000000-0000-4000-8000-000000000001}"
export GOATOS_LOCAL_USER_ID="${GOATOS_LOCAL_USER_ID:-90000000-0000-4000-8000-000000000101}"
export GOATOS_API_BASE_URL="${GOATOS_API_BASE_URL:-http://127.0.0.1:8080}"
export GOATOS_ADMIN_WEB_BASE_URL="${GOATOS_ADMIN_WEB_BASE_URL:-http://127.0.0.1:3300}"
export DATABASE_URL="${DATABASE_URL:-postgres://postgres:goatos@127.0.0.1:55432/goatos?sslmode=disable}"

mkdir -p "$report_dir"

on_exit() {
  local status=$?
  {
    printf 'run_id=%s\n' "$run_id"
    printf 'status=%s\n' "$status"
    printf 'api=%s\n' "$GOATOS_API_BASE_URL"
    printf 'admin_web=%s\n' "$GOATOS_ADMIN_WEB_BASE_URL"
    printf 'tenant=%s\n' "$GOATOS_TENANT_ID"
    printf 'ended_at=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  } >"$report_dir/summary.env"

  if [ "$status" -ne 0 ]; then
    echo "admin-web E2E smoke failed; report_dir=$report_dir" >&2
    if [ -f "$repo_root/.codex-goatos-render/logs/local-api.log" ]; then
      tail -120 "$repo_root/.codex-goatos-render/logs/local-api.log" >"$report_dir/local-api-tail.log" || true
    fi
  else
    echo "admin-web E2E smoke passed; report_dir=$report_dir"
  fi
}
trap on_exit EXIT

echo "## admin-web-e2e-smoke run_id=$run_id"

echo "### readiness: API"
curl -fsS "$GOATOS_API_BASE_URL/readyz" >/dev/null

echo "### readiness: admin-web"
curl -fsS "$GOATOS_ADMIN_WEB_BASE_URL/" >/dev/null

echo "### seed: local grant"
(
  cd "$backend_dir"
  go run ./cmd/seed-dev-grant \
    -tenant-id "$GOATOS_TENANT_ID" \
    -user-id "$GOATOS_LOCAL_USER_ID" \
    -role ceo_internal
)

echo "### seed: vaccination trigger baseline"
(
  cd "$backend_dir"
  go run ./cmd/seed-vaccination-trigger
)

echo "### token: mint local bearer"
GOATOS_BEARER_TOKEN="$(
  cd "$backend_dir"
  go run ./cmd/mint-dev-token \
    -tenant-id "$GOATOS_TENANT_ID" \
    -user-id "$GOATOS_LOCAL_USER_ID" \
    -ttl 2h
)"
export GOATOS_BEARER_TOKEN

echo "### data-plane: vaccination chain proof"
bash "$repo_root/tools/dev/vaccination-chain-proof.sh" | tee "$report_dir/vaccination-chain-proof.log"

echo "### frontend: live visual/click smoke"
npm --prefix "$repo_root/apps/admin-web" run smoke:visual:live | tee "$report_dir/smoke-visual-live.log"

echo "## admin-web-e2e-smoke closed run_id=$run_id"
