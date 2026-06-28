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
export PATH="/opt/homebrew/opt/postgresql@15/bin:$PATH"

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

jq_py() {
  python3 -c "import sys,json;d=json.load(sys.stdin);print($1)" 2>/dev/null || true
}

seed_open_vaccination_goat() {
  local stamp="$1"
  local version="00000000-0000-4000-8000-00000000b011"
  local sop_version="b0000000-0000-4000-8000-000000000002"
  local vaccine_item="00000000-0000-4000-8000-00000000b001"
  local rule="00000000-0000-4000-8000-00000000b012"
  local entry_date
  local dob_day21
  entry_date="$(date -u +%F)"
  if dob_day21="$(date -u -v-21d +%F 2>/dev/null)"; then
    :
  else
    dob_day21="$(date -u -d "$entry_date - 21 days" +%F)"
  fi

  local create
  create="$(
    curl -sS \
      -H "Authorization: Bearer $GOATOS_BEARER_TOKEN" \
      -H "Idempotency-Key: smoke-open-$stamp" \
      -H "Content-Type: application/json" \
      -X POST "$GOATOS_API_BASE_URL/admin/goats" \
      -d @- <<JSON
{"rfid":"SMOKE-OPEN-$stamp","park_code":"CBE","shed_code":"CBE_SHED_MANDELA_1_PART_1","sex":"female","dob":"$dob_day21","dob_estimated":true,"origin_type":"procured","entry_date":"$entry_date","management_stage":"K1","evidence_refs":[{"evidence_type":"source_record","evidence_id":"smoke-open-$stamp"}]}
JSON
  )"
  local goat_id
  goat_id="$(echo "$create" | jq_py 'd["goat"]["goat_id"]')"
  if [ -z "$goat_id" ]; then
    echo "open visual seed failed: $create" >&2
    return 1
  fi

  (
    cd "$backend_dir"
    GOATOS_OUTBOX_ALLOW_NONDURABLE=1 GOATOS_OUTBOX_PUBLISHER=eventbus \
      go run ./cmd/outbox-relay -limit 50 >/dev/null
  )
  (
    cd "$backend_dir"
    GOATOS_TENANT_ID="$GOATOS_TENANT_ID" \
      go run ./cmd/obligation-sweeper \
        -tenant-id "$GOATOS_TENANT_ID" \
        -version-id "$version" \
        -sop-version-id "$sop_version" \
        -vaccine-item-id "$vaccine_item" \
        -actor-id "$GOATOS_LOCAL_USER_ID" >/dev/null
  )

  local obligation_count
  obligation_count="$(
    psql "$DATABASE_URL" -tAc \
      "select count(*) from obligation_instances where tenant_id='$GOATOS_TENANT_ID' and target_id='$goat_id' and protocol_version_id='$version' and rule_id='$rule'"
  )"
  if [ "$obligation_count" != "1" ]; then
    echo "open visual seed did not create exactly one obligation for goat=$goat_id; count=$obligation_count" >&2
    return 1
  fi
  export GOATOS_SMOKE_GOAT_ID="$goat_id"
  echo "open visual goat=$goat_id"
}

seed_procurement_warmup_load() {
  local stamp="$1"
  local source_party_id="00000000-0000-4000-8000-000000001101"
  local purchase_date
  purchase_date="$(date -u +%F)"

  local create
  create="$(
    curl -sS \
      -H "Authorization: Bearer $GOATOS_BEARER_TOKEN" \
      -H "Idempotency-Key: smoke-proc-load-$stamp" \
      -H "Content-Type: application/json" \
      -X POST "$GOATOS_API_BASE_URL/procurement/source-entry/loads" \
      -d @- <<JSON
{"source_party_id":"$source_party_id","source_location_id":null,"expected_count":1,"purchase_date":"$purchase_date","planned_dispatch_at":null,"notes":"admin-web smoke supplier warmup seed","context":{"seed":"admin-web-e2e-smoke","run_id":"$run_id"}}
JSON
  )"
  local load_id
  load_id="$(echo "$create" | jq_py 'd["load"]["load_id"]')"
  if [ -z "$load_id" ]; then
    echo "procurement visual load seed failed: $create" >&2
    return 1
  fi

  local add
  add="$(
    curl -sS \
      -H "Authorization: Bearer $GOATOS_BEARER_TOKEN" \
      -H "Idempotency-Key: smoke-proc-goat-$stamp" \
      -H "Content-Type: application/json" \
      -X POST "$GOATOS_API_BASE_URL/procurement/source-entry/loads/$load_id/goats" \
      -d @- <<JSON
{"source_tag":"SMOKE-PROC-$stamp","source_rfid":"SMOKE-PROC-RFID-$stamp","temporary_id":"SMOKE-PROC-TEMP-$stamp","selection_state":"candidate","selection_reason":"admin-web smoke supplier warmup seed","purpose":"breeding","current_state":"source_warmup","identity_review_state":"clean","ownership_state":"pending","health_state":"pending","warmup_days":45,"holding_location_id":null,"proof_refs":[],"metadata":{"seed":"admin-web-e2e-smoke","run_id":"$run_id"}}
JSON
  )"
  local goat_id
  goat_id="$(echo "$add" | jq_py 'd["goat"]["goat_id"]')"
  if [ -z "$goat_id" ]; then
    echo "procurement visual goat seed failed: $add" >&2
    return 1
  fi

  echo "procurement visual load=$load_id goat=$goat_id"
}

echo "### data-plane: vaccination chain proof"
bash "$repo_root/tools/dev/vaccination-chain-proof.sh" | tee "$report_dir/vaccination-chain-proof.log"

echo "### seed: open vaccination work for browser click coverage"
seed_open_vaccination_goat "$(date +%s)" | tee "$report_dir/open-visual-goat.log"

echo "### seed: procurement supplier warmup row for browser click coverage"
seed_procurement_warmup_load "$(date +%s)" | tee "$report_dir/procurement-warmup.log"

echo "### frontend: live visual/click smoke"
npm --prefix "$repo_root/apps/admin-web" run smoke:visual:live | tee "$report_dir/smoke-visual-live.log"

echo "## admin-web-e2e-smoke closed run_id=$run_id"
