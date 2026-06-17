#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source "$repo_root/tools/postgres-ci.sh"

tenant_id="00000000-0000-4000-8000-000000000001"
actor_id="90000000-0000-4000-8000-000000000901"
source_dir="${GOATOS_REPLAY_SOURCE_DIR:-/Users/ravi/mesha/source-material/goatos-dev-data-fill-20260617}"
timestamp="$(date -u +%Y%m%dT%H%M%SZ)"
report_root="$repo_root/.codex-goatos-render/replay-frozen/$timestamp"
input_dir="$report_root/inputs"

rfid_src="$source_dir/rfid/RFID source of truth.xlsx"
bq_events_src="$source_dir/bq-latest/bq_events.json"
bq_locations_src="$source_dir/bq-latest/bq_latest_goat_locations.json"
old_tag_csv_src="$source_dir/drive-sheet-inventory-20260615T174444Z/safe-old-tag-passport-backfill-candidates.csv"

container_name="goatos-replay-frozen-${USER:-user}-$$"
image="${GOATOS_POSTGRES_IMAGE:-postgres:16.9-alpine}"
db_name="goatos"
db_user="postgres"

cleanup() {
  if [[ "${GOATOS_REPLAY_KEEP_DB:-0}" == "1" ]]; then
    echo "Keeping replay DB container: $container_name"
    echo "DATABASE_URL=$DATABASE_URL"
    return
  fi
  docker rm -f "$container_name" >/dev/null 2>&1 || true
}
trap cleanup EXIT

log() {
  printf '\n==> %s\n' "$*"
}

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || fail "$1 is required"
}

assert_file_sha() {
  local path="$1"
  local want_size="$2"
  local want_sha="$3"

  [[ -f "$path" ]] || fail "missing input: $path"

  local got_size
  got_size="$(wc -c <"$path" | tr -d '[:space:]')"
  [[ "$got_size" == "$want_size" ]] || fail "$(basename "$path") size=$got_size want=$want_size"

  local got_sha
  got_sha="$(shasum -a 256 "$path" | awk '{print $1}')"
  [[ "$got_sha" == "$want_sha" ]] || fail "$(basename "$path") sha=$got_sha want=$want_sha"

  echo "verified $(basename "$path") size=$got_size sha=$got_sha"
}

assert_eq() {
  local label="$1"
  local got="$2"
  local want="$3"

  if [[ "$got" != "$want" ]]; then
    fail "$label=$got want=$want"
  fi
  echo "ok $label=$got"
}

json_number() {
  local file="$1"
  local expr="$2"

  jq -er "$expr" "$file"
}

assert_json_number() {
  local label="$1"
  local file="$2"
  local expr="$3"
  local want="$4"

  local got
  got="$(json_number "$file" "$expr")"
  assert_eq "$label" "$got" "$want"
}

kv_value() {
  local file="$1"
  local key="$2"

  tr ' ' '\n' <"$file" | sed -n "s/^${key}=//p" | tail -1
}

assert_kv() {
  local label="$1"
  local file="$2"
  local key="$3"
  local want="$4"

  local got
  got="$(kv_value "$file" "$key")"
  assert_eq "$label" "$got" "$want"
}

run_psql() {
  postgres_ci_psql "$container_name" "$db_user" "$db_name" "$@"
}

query_scalar() {
  run_psql -Atq
}

apply_goose_up() {
  local migration="$1"
  awk '
    /^-- \+goose Up/ { in_up = 1; next }
    /^-- \+goose Down/ { in_up = 0 }
    in_up { print }
  ' "$migration" | run_psql >/dev/null
}

run_backend_text() {
  local out="$1"
  shift

  (
    cd "$repo_root/backend"
    "$@"
  ) | tee "$out"
}

run_backend_json() {
  local out="$1"
  shift

  run_backend_text "$out" "$@"
  jq -e . "$out" >/dev/null
}

count_sql() {
  local sql="$1"
  printf '%s\n' "$sql" | query_scalar
}

assert_sql_scalar() {
  local label="$1"
  local sql="$2"
  local want="$3"
  local got

  got="$(count_sql "$sql")"
  assert_eq "$label" "$got" "$want"
}

assert_old_tag_lifecycle() {
  local old_tag="$1"
  local want="$2"
  local got

  got="$(count_sql "
SELECT COALESCE(g.lifecycle_status, '')
FROM goat_identifiers gi
JOIN goats g ON g.goat_id = gi.goat_id AND g.tenant_id = gi.tenant_id
WHERE gi.tenant_id = '$tenant_id'
  AND gi.identifier_type = 'old_tag'
  AND gi.normalized_value = '$old_tag'
  AND gi.status = 'active'
ORDER BY gi.created_at DESC
LIMIT 1;
")"
  assert_eq "old_tag_${old_tag}_lifecycle" "$got" "$want"
}

need_cmd docker
need_cmd go
need_cmd jq
need_cmd shasum

mkdir -p "$input_dir"

log "Verifying frozen replay inputs"
assert_file_sha "$rfid_src" 49148 "79b1de33483cce3a19bc206a9be4c033c69ddd6fd346d05a66245d65de50369c"
assert_file_sha "$bq_events_src" 2519116 "c3547b461b6aca234d9d9ab4f0fa4ae35c3fcc5747bfffbb387d4c8d269ab4f0"
assert_file_sha "$bq_locations_src" 457547 "eb9fbc13b4d27c83533b65f7b5f5ef79d9eb7f5f48f5d7c56c4514f7c4fb6313"
assert_file_sha "$old_tag_csv_src" 297465 "4a6e16eebdac5dc87c7f8459a61e93cebcae1c46eeb721990b02db6f564482ff"

rfid_xlsx="$input_dir/RFID-source-of-truth.xlsx"
bq_events="$input_dir/bq_events.json"
bq_locations="$input_dir/bq_latest_goat_locations.json"
old_tag_csv="$input_dir/safe-old-tag-passport-backfill-candidates.csv"

cp "$rfid_src" "$rfid_xlsx"
cp "$bq_events_src" "$bq_events"
cp "$bq_locations_src" "$bq_locations"
cp "$old_tag_csv_src" "$old_tag_csv"

assert_eq "bq_events_rows" "$(jq 'length' "$bq_events")" "12906"
assert_eq "bq_latest_location_rows" "$(jq 'length' "$bq_locations")" "2706"
assert_eq "old_tag_candidate_lines" "$(wc -l <"$old_tag_csv" | tr -d '[:space:]')" "1450"

log "Starting throwaway Postgres"
docker run --rm --name "$container_name" \
  -e POSTGRES_PASSWORD=goatos \
  -e POSTGRES_DB="$db_name" \
  -p 127.0.0.1::5432 \
  -d "$image" >/dev/null

postgres_ci_wait_ready "$container_name" "$db_user" "$db_name"
port="$(docker inspect -f '{{(index (index .NetworkSettings.Ports "5432/tcp") 0).HostPort}}' "$container_name")"
export DATABASE_URL="postgres://postgres:goatos@127.0.0.1:${port}/${db_name}?sslmode=disable"
export GOATOS_ENV="local"

log "Applying Postgres migrations"
while IFS= read -r migration; do
  echo "Applying $(basename "$migration")"
  apply_goose_up "$migration"
done < <(find "$repo_root/backend/migrations/postgres" -maxdepth 1 -type f -name '*.sql' | sort)

assert_sql_scalar "migration_table_count" "SELECT count(*) FROM information_schema.tables WHERE table_schema = 'public';" "55"

log "Discovering RFID workbook"
discover_out="$report_root/rfid-discover.txt"
run_backend_text "$discover_out" go run ./cmd/rfid-import --discover --source-type local_xlsx --input "$rfid_xlsx" --sheet Combined
grep -q "classification=importable_shape_2" "$discover_out" || fail "RFID discovery did not classify shape 2"
grep -q "importable=true" "$discover_out" || fail "RFID discovery was not importable"

log "Staging RFID source"
rfid_import_out="$report_root/rfid-import.txt"
run_backend_text "$rfid_import_out" go run ./cmd/rfid-import \
  --input "$rfid_xlsx" \
  --sheet Combined \
  --tenant-id "$tenant_id" \
  --source-name "Frozen replay RFID source"

import_run_id="$(kv_value "$rfid_import_out" "import_run_id")"
[[ -n "$import_run_id" ]] || fail "rfid-import did not print import_run_id"
assert_kv "rfid_rows" "$rfid_import_out" "rows" "1223"
assert_kv "rfid_inserted" "$rfid_import_out" "inserted" "1223"
assert_kv "rfid_import_errors" "$rfid_import_out" "errors" "0"

log "Applying RFID clean rows"
rfid_apply_normal_out="$report_root/rfid-apply-normal.txt"
run_backend_text "$rfid_apply_normal_out" go run ./cmd/rfid-apply \
  --tenant-id "$tenant_id" \
  --import-run-id "$import_run_id" \
  --actor-id "$actor_id"
assert_kv "rfid_normal_applied" "$rfid_apply_normal_out" "applied" "780"
assert_kv "rfid_normal_scanned" "$rfid_apply_normal_out" "scanned" "780"
assert_kv "rfid_normal_review" "$rfid_apply_normal_out" "review" "0"
assert_kv "rfid_normal_errors" "$rfid_apply_normal_out" "errors" "0"

log "Applying first BQ reconcile pass"
bq_pass1_json="$report_root/bq-pass1.json"
run_backend_json "$bq_pass1_json" go run ./cmd/bq-reconcile \
  --tenant-id "$tenant_id" \
  --import-run-id "$import_run_id" \
  --events-json "$bq_events" \
  --locations-json "$bq_locations" \
  --report-dir "$report_root/bq-pass1-reports" \
  --execute \
  --timeout 20m \
  --trace-id "frozen-replay-bq-pass1-$timestamp"
assert_json_number "bq_pass1_patches_applied" "$bq_pass1_json" ".patches_applied" "728"

log "Applying RFID blank old-tag suffix rows"
rfid_apply_blank_out="$report_root/rfid-apply-blank-suffix.txt"
run_backend_text "$rfid_apply_blank_out" go run ./cmd/rfid-apply \
  --tenant-id "$tenant_id" \
  --import-run-id "$import_run_id" \
  --actor-id "$actor_id" \
  --allow-rfid-only-blank-suffix
assert_kv "rfid_blank_applied" "$rfid_apply_blank_out" "applied" "439"
assert_kv "rfid_blank_scanned" "$rfid_apply_blank_out" "scanned" "439"
assert_kv "rfid_blank_review" "$rfid_apply_blank_out" "review" "0"
assert_kv "rfid_blank_errors" "$rfid_apply_blank_out" "errors" "0"

log "Applying second BQ reconcile pass"
bq_pass2_json="$report_root/bq-pass2.json"
run_backend_json "$bq_pass2_json" go run ./cmd/bq-reconcile \
  --tenant-id "$tenant_id" \
  --import-run-id "$import_run_id" \
  --events-json "$bq_events" \
  --locations-json "$bq_locations" \
  --report-dir "$report_root/bq-pass2-reports" \
  --execute \
  --timeout 20m \
  --trace-id "frozen-replay-bq-pass2-$timestamp"
assert_json_number "bq_pass2_patches_applied" "$bq_pass2_json" ".patches_applied" "163"

log "Verifying BQ reconcile is settled before backfill"
bq_final_dry_json="$report_root/bq-final-dry-run.json"
run_backend_json "$bq_final_dry_json" go run ./cmd/bq-reconcile \
  --tenant-id "$tenant_id" \
  --import-run-id "$import_run_id" \
  --events-json "$bq_events" \
  --locations-json "$bq_locations" \
  --report-dir "$report_root/bq-final-dry-run-reports" \
  --timeout 20m \
  --trace-id "frozen-replay-bq-final-dry-$timestamp"
assert_json_number "bq_final_patches_planned" "$bq_final_dry_json" ".patches_planned" "0"

assert_sql_scalar "pre_backfill_total" "SELECT count(*) FROM goats WHERE tenant_id = '$tenant_id' AND identity_state <> 'merged';" "1219"

log "Applying deterministic old-tag passport backfill"
old_tag_json="$report_root/old-tag-backfill.json"
run_backend_json "$old_tag_json" go run ./cmd/bq-reconcile \
  --tenant-id "$tenant_id" \
  --backfill-candidates-csv "$old_tag_csv" \
  --locations-json "$bq_locations" \
  --report-dir "$report_root/old-tag-backfill-reports" \
  --execute \
  --timeout 20m \
  --trace-id "frozen-replay-old-tag-backfill-$timestamp"
assert_json_number "old_tag_candidates_read" "$old_tag_json" ".candidates_read" "1449"
assert_json_number "old_tag_latest_location_overrides" "$old_tag_json" ".latest_location_lifecycle_overrides" "3"
assert_json_number "old_tag_applied_creates" "$old_tag_json" ".applied_creates" "1449"

log "Rebuilding counters after full replay"
counter_out="$report_root/rebuild-identity-counters.txt"
run_backend_text "$counter_out" go run ./cmd/rebuild-identity-counters \
  --tenant-id "$tenant_id" \
  --source-import-run-id "$import_run_id"

log "Asserting frozen legacy parity"
assert_sql_scalar "goats_total" "SELECT count(*) FROM goats WHERE tenant_id = '$tenant_id' AND identity_state <> 'merged';" "2668"
assert_sql_scalar "goats_alive" "SELECT count(*) FROM goats WHERE tenant_id = '$tenant_id' AND identity_state <> 'merged' AND lifecycle_status = 'alive';" "2088"
assert_sql_scalar "goats_sold" "SELECT count(*) FROM goats WHERE tenant_id = '$tenant_id' AND identity_state <> 'merged' AND lifecycle_status = 'sold';" "544"
assert_sql_scalar "goats_dead" "SELECT count(*) FROM goats WHERE tenant_id = '$tenant_id' AND identity_state <> 'merged' AND lifecycle_status = 'dead';" "36"
assert_sql_scalar "goats_inactive" "SELECT count(*) FROM goats WHERE tenant_id = '$tenant_id' AND identity_state <> 'merged' AND lifecycle_status = 'inactive';" "0"
assert_sql_scalar "identity_clean" "SELECT count(*) FROM goats WHERE tenant_id = '$tenant_id' AND identity_state = 'clean';" "2465"
assert_sql_scalar "identity_needs_review" "SELECT count(*) FROM goats WHERE tenant_id = '$tenant_id' AND identity_state = 'needs_review';" "203"
assert_sql_scalar "open_conflicts" "SELECT count(*) FROM identity_conflicts WHERE tenant_id = '$tenant_id' AND state IN ('open', 'needs_field_check');" "232"
assert_sql_scalar "import_rows_created_goat" "SELECT count(*) FROM legacy_import_rows WHERE tenant_id = '$tenant_id' AND processing_state = 'created_goat';" "1219"
assert_sql_scalar "import_rows_needs_review" "SELECT count(*) FROM legacy_import_rows WHERE tenant_id = '$tenant_id' AND processing_state = 'needs_review';" "4"

assert_old_tag_lifecycle "952" "alive"
assert_old_tag_lifecycle "998" "alive"
assert_old_tag_lifecycle "SA2328307" "dead"

log "Frozen replay passed"
echo "import_run_id=$import_run_id"
echo "report_root=$report_root"
