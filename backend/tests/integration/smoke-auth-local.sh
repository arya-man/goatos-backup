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
report_dir="$(mktemp -d "${TMPDIR:-/tmp}/goatos-import-report.XXXXXX")"
fixture="$repo_root/backend/internal/legacy_import/testdata/synthetic_rfid_import.xlsx"

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
  rm -rf "$report_dir"
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

discover_output="$(
  cd "$repo_root/backend"
  go run ./cmd/rfid-import --discover --input "$fixture" --sheet Combined
)"
if [[ "$discover_output" != *"classification=importable_shape_2"* || "$discover_output" != *"importable=true"* ]]; then
  echo "source discovery did not classify synthetic Shape 2 fixture as importable: $discover_output" >&2
  exit 1
fi

google_skip_output="$(
  cd "$repo_root/backend"
  go run ./cmd/rfid-import --discover --source-type google_sheet
)"
if [[ "$google_skip_output" != *"classification=google_sheet_export_skipped"* || "$google_skip_output" != *"google_sheet_export_built=false"* ]]; then
  echo "google sheet discovery did not skip cleanly: $google_skip_output" >&2
  exit 1
fi

dry_run_output="$(
  cd "$repo_root/backend"
  go run ./cmd/rfid-import --input "$fixture" --sheet Combined --tenant-id "$tenant_id" --dry-run --source-name "Synthetic RFID smoke dry run"
)"
dry_run_id="$(sed -E 's/.*import_run_id=([^ ]+).*/\1/' <<<"$dry_run_output")"
if [ "$(run_psql -Atc "SELECT count(*) FROM legacy_import_rows WHERE import_run_id = '$dry_run_id'")" != "0" ]; then
  echo "dry-run import staged rows unexpectedly: $dry_run_output" >&2
  exit 1
fi

import_output="$(
  cd "$repo_root/backend"
  go run ./cmd/rfid-import --input "$fixture" --sheet Combined --tenant-id "$tenant_id" --source-name "Synthetic RFID smoke staging"
)"
import_run_id="$(sed -E 's/.*import_run_id=([^ ]+).*/\1/' <<<"$import_output")"
if [ -z "$import_run_id" ] || [ "$import_run_id" = "$import_output" ]; then
  echo "failed to parse import_run_id from: $import_output" >&2
  exit 1
fi

(
  cd "$repo_root/backend"
  go run ./cmd/rfid-apply --tenant-id "$tenant_id" --import-run-id "$import_run_id" --actor-id "$granted_user_id"
) >/dev/null

report_output="$(
  cd "$repo_root/backend"
  go run ./cmd/rfid-import --anomaly-report --tenant-id "$tenant_id" --import-run-id "$import_run_id" --sheet Combined --source-name "Synthetic RFID smoke final review" --output-dir "$report_dir"
)"
if [[ "$report_output" != *"anomaly_report_details="* || "$report_output" != *"anomaly_report_summary="* || "$report_output" != *"anomaly_report_groups="* ]]; then
  echo "final anomaly report did not write all report paths: $report_output" >&2
  exit 1
fi
details_path="$(sed -E 's/.*anomaly_report_details=([^ ]+).*/\1/' <<<"$report_output")"
summary_path="$(sed -E 's/.*anomaly_report_summary=([^ ]+).*/\1/' <<<"$report_output")"
groups_path="$(sed -E 's/.*anomaly_report_groups=([^ ]+).*/\1/' <<<"$report_output")"
for report_path in "$details_path" "$summary_path" "$groups_path"; do
  if [ ! -s "$report_path" ]; then
    echo "final anomaly report path missing or empty: $report_path" >&2
    exit 1
  fi
done
if rg -n "990000000000000000" "$details_path" "$summary_path" "$groups_path" >/dev/null 2>&1; then
  echo "final anomaly report leaked raw synthetic RFID prefix" >&2
  exit 1
fi
if ! rg -n "duplicate_rfid_in_workbook|missing_rfid|blank_gender" "$details_path" "$summary_path" "$groups_path" >/dev/null 2>&1; then
  echo "final anomaly report did not include expected staging reason codes" >&2
  exit 1
fi
if ! rg -n "unknown_status_mapping|species_or_breed_requires_review" "$details_path" "$summary_path" "$groups_path" >/dev/null 2>&1; then
  echo "final anomaly report did not include an apply-stage review reason" >&2
  exit 1
fi
if ! rg -n "source_status_label|source_breed_label" "$groups_path" >/dev/null 2>&1; then
  echo "final anomaly grouped summary did not include an apply-stage grouped bucket" >&2
  exit 1
fi

(
  cd "$repo_root/backend"
  go run ./cmd/rebuild-identity-counters --tenant-id "$tenant_id" --source-import-run-id "$import_run_id"
) >/dev/null

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

GOATOS_API_BASE_URL="http://127.0.0.1:$api_port" GOATOS_BEARER_TOKEN="$granted_token" \
  npm --prefix "$repo_root/apps/admin-web" run build >/dev/null

echo "Local full-stack smoke passed: source discovery, fixture import/apply, final masked anomaly report, counter rebuild, bearer auth, and admin-web build are wired through local Docker Postgres."
