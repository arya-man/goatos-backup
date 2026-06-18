#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source "$repo_root/tools/postgres-ci.sh"

tenant_id="00000000-0000-4000-8000-000000000001"
actor_id="90000000-0000-4000-8000-000000000901"
timestamp="$(date -u +%Y%m%dT%H%M%SZ)"
report_root="$repo_root/.codex-goatos-render/replay-live/$timestamp"
input_dir="$report_root/inputs"
generated_dir="$report_root/generated"

rfid_sheet_id="${GOATOS_LIVE_RFID_SHEET_ID:-1FulMrlb8_AGwL5nFwoORACnbDstSFMg-GCPaKIZnnF8}"
census_sheet_id="${GOATOS_LIVE_CENSUS_SHEET_ID:-1tye3uknlVMoPIiYk2pdkwy9PLQwo5m8wdFIsc7yI5Ho}"
goats_db_sheet_id="${GOATOS_LIVE_GOATS_DB_SHEET_ID:-1R648AutCSXS247DZb7dc07dDgyue6_R4mZDd93oW3M8}"

legacy_bq_project="${GOATOS_LEGACY_BQ_PROJECT:-goatos-sheets}"
legacy_bq_location="${GOATOS_LEGACY_BQ_LOCATION:-US}"
container_name="goatos-replay-live-${USER:-user}-$$"
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
  echo "report_root=$report_root" >&2
  exit 1
}

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || fail "$1 is required"
}

assert_file() {
  local path="$1"
  [[ -f "$path" ]] || fail "missing input file: $path"
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

kv_value() {
  local file="$1"
  local key="$2"

  tr ' ' '\n' <"$file" | sed -n "s/^${key}=//p" | tail -1
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

bq_query() {
  local out="$1"
  local format="$2"
  local sql="$3"
  local sql_file="$report_root/$(basename "$out").sql"

  printf '%s\n' "$sql" >"$sql_file"
  bq --quiet \
    --project_id="$legacy_bq_project" \
    --location="$legacy_bq_location" \
    query \
    --use_legacy_sql=false \
    --format="$format" \
    --max_rows=10000000 \
    <"$sql_file" >"$out"
}

export_drive_sheet_xlsx() {
  local file_id="$1"
  local label="$2"
  local out_xlsx="$3"
  local out_metadata="$4"
  local token="$5"

  curl -fsS \
    -H "Authorization: Bearer $token" \
    "https://www.googleapis.com/drive/v3/files/$file_id?fields=id,name,mimeType,modifiedTime,webViewLink" \
    -o "$out_metadata"
  curl -fL \
    -H "Authorization: Bearer $token" \
    "https://www.googleapis.com/drive/v3/files/$file_id/export?mimeType=application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" \
    -o "$out_xlsx"
  jq -r "\"${label}_drive_name=\\(.name) modified=\\(.modifiedTime) url=\\(.webViewLink)\"" "$out_metadata"
}

xlsx_sheet_to_csv() {
  local in_xlsx="$1"
  local sheet_name="$2"
  local out_csv="$3"

  python3 - "$in_xlsx" "$sheet_name" "$out_csv" <<'PY'
import csv
import sys

from openpyxl import load_workbook

in_xlsx, sheet_name, out_csv = sys.argv[1:4]
wb = load_workbook(in_xlsx, data_only=True, read_only=True)
if sheet_name not in wb.sheetnames:
    raise SystemExit(f"missing sheet {sheet_name!r}; available={wb.sheetnames!r}")
ws = wb[sheet_name]
with open(out_csv, "w", newline="") as f:
    writer = csv.writer(f)
    for row in ws.iter_rows(values_only=True):
        writer.writerow(["" if value is None else value for value in row])
PY
}

sheets_range_to_csv() {
  local file_id="$1"
  local range_name="$2"
  local out_csv="$3"
  local token="$4"
  local encoded_range
  local values_json

  encoded_range="$(python3 - "$range_name" <<'PY'
import sys
from urllib.parse import quote

print(quote(sys.argv[1], safe=""))
PY
)"
  values_json="${out_csv%.csv}.values.json"
  curl -fsS \
    -H "Authorization: Bearer $token" \
    "https://sheets.googleapis.com/v4/spreadsheets/$file_id/values/$encoded_range?valueRenderOption=UNFORMATTED_VALUE&dateTimeRenderOption=FORMATTED_STRING" \
    -o "$values_json"
  python3 - "$values_json" "$out_csv" <<'PY'
import csv
import json
import sys

values_json, out_csv = sys.argv[1:3]
with open(values_json) as f:
    values = json.load(f).get("values", [])
width = max((len(row) for row in values), default=0)
with open(out_csv, "w", newline="") as f:
    writer = csv.writer(f)
    for row in values:
        writer.writerow(row + [""] * (width - len(row)))
PY
}

write_db_counts_json() {
  local out="$1"

  printf "%s\n" "
COPY (
  SELECT json_build_object(
    'total', count(*) FILTER (WHERE identity_state <> 'merged'),
    'alive', count(*) FILTER (WHERE identity_state <> 'merged' AND lifecycle_status = 'alive'),
    'sold', count(*) FILTER (WHERE identity_state <> 'merged' AND lifecycle_status = 'sold'),
    'dead', count(*) FILTER (WHERE identity_state <> 'merged' AND lifecycle_status = 'dead'),
    'inactive', count(*) FILTER (WHERE identity_state <> 'merged' AND lifecycle_status = 'inactive'),
    'clean', count(*) FILTER (WHERE identity_state = 'clean'),
    'needs_review', count(*) FILTER (WHERE identity_state = 'needs_review')
  )
  FROM goats
  WHERE tenant_id = '$tenant_id'
) TO STDOUT;
" | run_psql >"$out"
}

export_identity_snapshot_from_container() {
  local container="$1"
  local out="$2"

  docker exec "$container" psql -U postgres -d "$db_name" -AtF $'\t' -c "
SELECT
  g.display_id,
  COALESCE((
    SELECT i.normalized_value
    FROM goat_identifiers i
    WHERE i.goat_id = g.goat_id
      AND i.identifier_type = 'rfid'
      AND i.status = 'active'
    ORDER BY i.normalized_value
    LIMIT 1
  ), '') AS rfid,
  COALESCE((
    SELECT i.scope_key || ':' || i.normalized_value
    FROM goat_identifiers i
    WHERE i.goat_id = g.goat_id
      AND i.identifier_type = 'old_tag'
      AND i.status = 'active'
    ORDER BY i.scope_key, i.normalized_value
    LIMIT 1
  ), '') AS old_tag,
  g.lifecycle_status,
  COALESCE(p.name, '') AS park,
  COALESCE(g.breed, '') AS breed,
  COALESCE(g.sex, '') AS sex,
  COALESCE(g.age_band, '') AS age_band,
  g.identity_state
FROM goats g
LEFT JOIN locations p
  ON p.location_id = g.park_id
WHERE g.tenant_id = '$tenant_id'
  AND g.identity_state <> 'merged'
ORDER BY rfid, old_tag, g.display_id;
" >"$out"
}

export_existing_identifiers() {
  local out="$1"

  printf "%s\n" "
COPY (
  SELECT gi.identifier_type, gi.scope_key, gi.normalized_value, g.display_id
  FROM goat_identifiers gi
  JOIN goats g
    ON g.tenant_id = gi.tenant_id
   AND g.goat_id = gi.goat_id
  WHERE gi.tenant_id = '$tenant_id'
    AND gi.identifier_type IN ('old_tag', 'rfid')
    AND gi.status = 'active'
    AND g.identity_state <> 'merged'
  ORDER BY gi.identifier_type, gi.scope_key, gi.normalized_value
) TO STDOUT WITH CSV HEADER;
" | run_psql >"$out"
}

need_cmd bq
need_cmd curl
need_cmd docker
need_cmd gcloud
need_cmd go
need_cmd jq
need_cmd python3
need_cmd shasum

mkdir -p "$input_dir" "$generated_dir"

log "Checking read-only Google context"
active_account="$(gcloud auth list --filter=status:ACTIVE --format='value(account)' 2>/dev/null | head -1 || true)"
[[ "$active_account" == "ravi@mesha.sg" ]] || fail "active gcloud account is $active_account, expected ravi@mesha.sg"
echo "active_gcloud_account=$active_account"
echo "legacy_bq_project=$legacy_bq_project"
echo "legacy_bq_location=$legacy_bq_location"

log "Exporting live RFID workbook from Google Drive"
rfid_xlsx="$input_dir/RFID-source-of-truth.live.xlsx"
rfid_metadata="$input_dir/RFID-source-of-truth.live.metadata.json"
drive_token=""
drive_token="$(gcloud auth print-access-token)"
export_drive_sheet_xlsx "$rfid_sheet_id" "rfid" "$rfid_xlsx" "$rfid_metadata" "$drive_token"
echo "rfid_source=drive:$rfid_sheet_id"
rfid_size="$(wc -c <"$rfid_xlsx" | tr -d '[:space:]')"
rfid_sha="$(shasum -a 256 "$rfid_xlsx" | awk '{print $1}')"
echo "rfid_size=$rfid_size"
echo "rfid_sha256=$rfid_sha"

log "Exporting live Census DB workbook from Google Drive"
census_xlsx="$input_dir/Census-DB.live.xlsx"
census_metadata="$input_dir/Census-DB.live.metadata.json"
census_csv="$input_dir/Census_DB__DB.live.csv"
if [[ -z "$drive_token" ]]; then
  drive_token="$(gcloud auth print-access-token)"
fi
export_drive_sheet_xlsx "$census_sheet_id" "census" "$census_xlsx" "$census_metadata" "$drive_token"
xlsx_sheet_to_csv "$census_xlsx" "DB" "$census_csv"
echo "census_csv_lines=$(wc -l <"$census_csv" | tr -d '[:space:]')"

log "Exporting live Goats DB sheets through Sheets API"
goats_db_metadata="$input_dir/Goats-DB.live.metadata.json"
curl -fsS \
  -H "Authorization: Bearer $drive_token" \
  "https://sheets.googleapis.com/v4/spreadsheets/$goats_db_sheet_id?fields=spreadsheetId,properties.title,sheets.properties(title,sheetId,index,gridProperties.rowCount,gridProperties.columnCount)" \
  -o "$goats_db_metadata"
goats_db_db_csv="$input_dir/Goats_DB__DB.live.csv"
goats_db_livelist_csv="$input_dir/Goats_DB__LiveList.live.csv"
sheets_range_to_csv "$goats_db_sheet_id" "DB!A:T" "$goats_db_db_csv" "$drive_token"
sheets_range_to_csv "$goats_db_sheet_id" "LiveList!A:Z" "$goats_db_livelist_csv" "$drive_token"
echo "goats_db_db_lines=$(wc -l <"$goats_db_db_csv" | tr -d '[:space:]')"
echo "goats_db_livelist_lines=$(wc -l <"$goats_db_livelist_csv" | tr -d '[:space:]')"

log "Exporting live BQ event rows"
bq_events="$input_dir/bq_events.live.json"
bq_query "$bq_events" "json" "
SELECT
  COALESCE(CAST(age AS STRING), '') AS age,
  COALESCE(CAST(breed AS STRING), '') AS breed,
  COALESCE(CAST(date AS STRING), '') AS date,
  COALESCE(CAST(dst_shed AS STRING), '') AS dst_shed,
  COALESCE(CAST(event AS STRING), '') AS event,
  COALESCE(CAST(farm AS STRING), '') AS farm,
  COALESCE(CAST(farm_goat_id AS STRING), '') AS farm_goat_id,
  COALESCE(CAST(gender AS STRING), '') AS gender,
  COALESCE(CAST(goat_id AS STRING), '') AS goat_id,
  COALESCE(CAST(src_shed AS STRING), '') AS src_shed
FROM \`$legacy_bq_project.goatsDB.goats_db_clean\`
WHERE goat_id IS NOT NULL
  AND TRIM(CAST(goat_id AS STRING)) != ''
ORDER BY
  SAFE_CAST(date AS DATE),
  farm,
  goat_id,
  event
"
jq -e . "$bq_events" >/dev/null
echo "bq_events_rows=$(jq 'length' "$bq_events")"

log "Exporting live BQ current identity rows"
bq_current_identities="$input_dir/bq_current_identities.live.csv"
bq_query "$bq_current_identities" "csv" "
WITH base AS (
  SELECT
    COALESCE(CAST(farm AS STRING), '') AS farm,
    TRIM(CAST(goat_id AS STRING)) AS goat_id,
    CASE
      WHEN REGEXP_CONTAINS(TRIM(CAST(goat_id AS STRING)), r'^[0-9]+\\.0+$')
        THEN REGEXP_EXTRACT(TRIM(CAST(goat_id AS STRING)), r'^([0-9]+)\\.0+$')
      ELSE REGEXP_REPLACE(UPPER(REGEXP_REPLACE(TRIM(CAST(goat_id AS STRING)), r'[,[:space:]]', '')), r'[^A-Z0-9]+', '')
    END AS goat_key,
    COALESCE(NULLIF(TRIM(CAST(breed AS STRING)), ''), '-') AS breed,
    COALESCE(NULLIF(TRIM(CAST(gender AS STRING)), ''), '-') AS gender,
    COALESCE(TRIM(CAST(current_status AS STRING)), '') AS current_status,
    COALESCE(TRIM(CAST(last_event AS STRING)), '') AS last_event,
    COALESCE(CAST(last_event_date AS STRING), '') AS last_event_date,
    COALESCE(NULLIF(TRIM(CAST(last_shed AS STRING)), ''), '-') AS last_shed,
    COALESCE(CAST(event_date AS STRING), '') AS event_date
  FROM \`$legacy_bq_project.goatsDB.goat_activity_timeline\`
  WHERE goat_id IS NOT NULL
    AND TRIM(CAST(goat_id AS STRING)) != ''
),
ranked AS (
  SELECT
    *,
    COUNT(*) OVER (PARTITION BY farm, goat_key) AS source_event_count,
    ROW_NUMBER() OVER (
      PARTITION BY farm, goat_key
      ORDER BY
        IFNULL(SAFE_CAST(last_event_date AS DATE), DATE '0001-01-01') DESC,
        IFNULL(SAFE_CAST(event_date AS DATE), DATE '0001-01-01') DESC,
        CASE LOWER(last_event)
          WHEN 'death' THEN 3
          WHEN 'sale' THEN 2
          WHEN 'shifting' THEN 1
          WHEN 'birth' THEN 1
          WHEN 'purchase' THEN 1
          WHEN 'abortion' THEN 1
          ELSE 0
        END DESC,
        CASE WHEN breed NOT IN ('', '-') THEN 1 ELSE 0 END DESC,
        CASE WHEN gender NOT IN ('', '-') THEN 1 ELSE 0 END DESC,
        goat_id
    ) AS rn
  FROM base
  WHERE farm != ''
    AND goat_key != ''
)
SELECT
  farm,
  goat_id,
  goat_key,
  breed,
  gender,
  current_status,
  last_event,
  last_event_date,
  last_shed,
  source_event_count
FROM ranked
WHERE rn = 1
ORDER BY farm, goat_key
"
echo "bq_current_identity_lines=$(wc -l <"$bq_current_identities" | tr -d '[:space:]')"

log "Diagnosing live RFID evidence against live BQ and Goats DB"
rfid_diagnostics_csv="$report_root/live-rfid-source-diagnostics.csv"
rfid_diagnostics_json="$report_root/live-rfid-source-diagnostics.json"
diagnostic_args=(
  --rfid-xlsx "$rfid_xlsx"
  --rfid-sheet Combined
  --bq-current-csv "$bq_current_identities"
  --goats-db-db-csv "$goats_db_db_csv"
  --diagnostics-csv-out "$rfid_diagnostics_csv"
  --summary-json-out "$rfid_diagnostics_json"
)
python3 "$repo_root/tools/replay/diagnose-live-replay-sources.py" "${diagnostic_args[@]}" | tee "$report_root/live-rfid-source-diagnostics.pretty.json"

log "Filtering live RFID workbook to canonical farm blocks; reopened append blocks are review-only"
rfid_import_xlsx="$generated_dir/RFID-source-of-truth.live.seed.xlsx"
rfid_delta_csv="$generated_dir/RFID-source-of-truth.live.excluded-reopened-farm-review.csv"
rfid_filter_summary="$generated_dir/rfid-seed-filter-summary.json"
python3 "$repo_root/tools/replay/filter-rfid-workbook.py" \
  --input-xlsx "$rfid_xlsx" \
  --sheet Combined \
  --output-xlsx "$rfid_import_xlsx" \
  --excluded-csv "$rfid_delta_csv" \
  --summary-json "$rfid_filter_summary" | tee "$report_root/rfid-seed-filter-summary.pretty.json"

log "Exporting live legacy dashboard count"
legacy_count_json="$input_dir/legacy_dashboard_count.live.json"
bq_query "$legacy_count_json" "json" "
SELECT
  CAST(date AS STRING) AS date,
  farm_total_count,
  cbe_summary_count,
  cpt_summary_count,
  procurement_summary_count
FROM \`$legacy_bq_project.farm.daily_summary_dev\`
WHERE farm_total_count IS NOT NULL
ORDER BY date DESC
LIMIT 1
"
jq -e 'length == 1' "$legacy_count_json" >/dev/null
legacy_date="$(jq -r '.[0].date' "$legacy_count_json")"
legacy_active="$(jq -r '.[0].farm_total_count' "$legacy_count_json")"
echo "legacy_dashboard_date=$legacy_date"
echo "legacy_dashboard_active=$legacy_active"

log "Generating latest-location JSON from live BQ current identities"
bq_locations="$generated_dir/bq_latest_goat_locations.live.json"
pre_candidate_csv="$generated_dir/pre_db_candidates.ignore.csv"
pre_skipped_csv="$generated_dir/pre_db_skipped.csv"
pre_summary_json="$generated_dir/pre_db_generator_summary.json"
python3 "$repo_root/tools/replay/generate-live-replay-inputs.py" \
  --current-identities-csv "$bq_current_identities" \
  --census-csv "$census_csv" \
  --goats-db-events-csv "$goats_db_db_csv" \
  --locations-json-out "$bq_locations" \
  --candidates-csv-out "$pre_candidate_csv" \
  --skipped-csv-out "$pre_skipped_csv" \
  --summary-json-out "$pre_summary_json" | tee "$report_root/pre-db-generator-summary.pretty.json"

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

log "Discovering live RFID workbook"
discover_out="$report_root/rfid-discover.txt"
run_backend_text "$discover_out" go run ./cmd/rfid-import --discover --source-type local_xlsx --input "$rfid_import_xlsx" --sheet Combined
grep -q "importable=true" "$discover_out" || fail "RFID discovery was not importable"

log "Staging live RFID source"
rfid_import_out="$report_root/rfid-import.txt"
run_backend_text "$rfid_import_out" go run ./cmd/rfid-import \
  --input "$rfid_import_xlsx" \
  --sheet Combined \
  --tenant-id "$tenant_id" \
  --source-name "Live replay RFID source"

import_run_id="$(kv_value "$rfid_import_out" "import_run_id")"
[[ -n "$import_run_id" ]] || fail "rfid-import did not print import_run_id"
echo "import_run_id=$import_run_id"

log "Applying RFID clean rows"
rfid_apply_normal_out="$report_root/rfid-apply-normal.txt"
run_backend_text "$rfid_apply_normal_out" go run ./cmd/rfid-apply \
  --tenant-id "$tenant_id" \
  --import-run-id "$import_run_id" \
  --actor-id "$actor_id"

log "Applying first live BQ reconcile pass"
bq_pass1_json="$report_root/bq-pass1.json"
run_backend_json "$bq_pass1_json" go run ./cmd/bq-reconcile \
  --tenant-id "$tenant_id" \
  --import-run-id "$import_run_id" \
  --events-json "$bq_events" \
  --locations-json "$bq_locations" \
  --report-dir "$report_root/bq-pass1-reports" \
  --execute \
  --timeout 20m \
  --trace-id "live-replay-bq-pass1-$timestamp"

log "Applying RFID blank old-tag suffix rows"
rfid_apply_blank_out="$report_root/rfid-apply-blank-suffix.txt"
run_backend_text "$rfid_apply_blank_out" go run ./cmd/rfid-apply \
  --tenant-id "$tenant_id" \
  --import-run-id "$import_run_id" \
  --actor-id "$actor_id" \
  --allow-rfid-only-blank-suffix

log "Applying second live BQ reconcile pass"
bq_pass2_json="$report_root/bq-pass2.json"
run_backend_json "$bq_pass2_json" go run ./cmd/bq-reconcile \
  --tenant-id "$tenant_id" \
  --import-run-id "$import_run_id" \
  --events-json "$bq_events" \
  --locations-json "$bq_locations" \
  --report-dir "$report_root/bq-pass2-reports" \
  --execute \
  --timeout 20m \
  --trace-id "live-replay-bq-pass2-$timestamp"

log "Verifying live BQ reconcile is settled before old-tag candidate generation"
bq_final_dry_json="$report_root/bq-final-dry-run.json"
run_backend_json "$bq_final_dry_json" go run ./cmd/bq-reconcile \
  --tenant-id "$tenant_id" \
  --import-run-id "$import_run_id" \
  --events-json "$bq_events" \
  --locations-json "$bq_locations" \
  --report-dir "$report_root/bq-final-dry-run-reports" \
  --timeout 20m \
  --trace-id "live-replay-bq-final-dry-$timestamp"
assert_eq "bq_final_patches_planned" "$(json_number "$bq_final_dry_json" ".patches_planned")" "0"

pre_backfill_counts="$report_root/pre-backfill-db-counts.json"
write_db_counts_json "$pre_backfill_counts"
echo "pre_backfill_counts=$(cat "$pre_backfill_counts")"

log "Exporting existing identifier keys from throwaway DB"
existing_identifiers_csv="$generated_dir/existing_identifiers_after_rfid_bq.csv"
export_existing_identifiers "$existing_identifiers_csv"
echo "existing_identifier_lines=$(wc -l <"$existing_identifiers_csv" | tr -d '[:space:]')"

log "Regenerating old-tag candidates from live BQ current identities"
old_tag_csv="$generated_dir/safe-old-tag-passport-backfill-candidates.live.csv"
skipped_csv="$generated_dir/safe-old-tag-passport-backfill-skipped.live.csv"
summary_json="$generated_dir/live-generator-summary.json"
python3 "$repo_root/tools/replay/generate-live-replay-inputs.py" \
  --current-identities-csv "$bq_current_identities" \
  --census-csv "$census_csv" \
  --goats-db-events-csv "$goats_db_db_csv" \
  --existing-identifiers-csv "$existing_identifiers_csv" \
  --locations-json-out "$bq_locations" \
  --candidates-csv-out "$old_tag_csv" \
  --skipped-csv-out "$skipped_csv" \
  --summary-json-out "$summary_json" | tee "$report_root/live-generator-summary.pretty.json"

candidate_rows="$(($(wc -l <"$old_tag_csv" | tr -d '[:space:]') - 1))"
[[ "$candidate_rows" -gt 0 ]] || fail "live generator produced zero old-tag candidates"
echo "old_tag_candidate_rows=$candidate_rows"

log "Applying regenerated old-tag passport backfill"
old_tag_json="$report_root/old-tag-backfill.json"
run_backend_json "$old_tag_json" go run ./cmd/bq-reconcile \
  --tenant-id "$tenant_id" \
  --backfill-candidates-csv "$old_tag_csv" \
  --locations-json "$bq_locations" \
  --report-dir "$report_root/old-tag-backfill-reports" \
  --execute \
  --timeout 20m \
  --trace-id "live-replay-old-tag-backfill-$timestamp"

log "Rebuilding counters after full live replay"
counter_out="$report_root/rebuild-identity-counters.txt"
run_backend_text "$counter_out" go run ./cmd/rebuild-identity-counters \
  --tenant-id "$tenant_id" \
  --source-import-run-id "$import_run_id"

log "Checking live replay parity against legacy dashboard active count"
final_counts="$report_root/final-db-counts.json"
write_db_counts_json "$final_counts"
final_alive="$(jq -r '.alive' "$final_counts")"
final_total="$(jq -r '.total' "$final_counts")"
final_sold="$(jq -r '.sold' "$final_counts")"
final_dead="$(jq -r '.dead' "$final_counts")"
final_inactive="$(jq -r '.inactive' "$final_counts")"

oracle_compare_json="$report_root/oracle-compare/summary.json"
oracle_compare_status="skipped"
mkdir -p "$(dirname "$oracle_compare_json")"
printf 'null\n' >"$oracle_compare_json"
if [[ -n "${GOATOS_REPLAY_ORACLE_CONTAINER:-goatos-local-current-persist}" ]] && \
   docker ps --format '{{.Names}}' | grep -Fxq "${GOATOS_REPLAY_ORACLE_CONTAINER:-goatos-local-current-persist}"; then
  oracle_container="${GOATOS_REPLAY_ORACLE_CONTAINER:-goatos-local-current-persist}"
  oracle_compare_status="checked"
  log "Comparing live replay against local oracle container $oracle_container"
  oracle_dir="$report_root/oracle-compare"
  mkdir -p "$oracle_dir"
  export_identity_snapshot_from_container "$oracle_container" "$oracle_dir/oracle.tsv"
  export_identity_snapshot_from_container "$container_name" "$oracle_dir/replay.tsv"
  python3 "$repo_root/tools/replay/compare-replay-to-oracle.py" \
    --oracle-tsv "$oracle_dir/oracle.tsv" \
    --replay-tsv "$oracle_dir/replay.tsv" \
    --out-dir "$oracle_dir" \
    --summary-json "$oracle_compare_json" | tee "$oracle_dir/summary.pretty.json"
else
  echo "oracle_compare=skipped (set GOATOS_REPLAY_ORACLE_CONTAINER or run goatos-local-current-persist)"
fi

jq -n \
  --arg report_root "$report_root" \
  --arg legacy_date "$legacy_date" \
  --argjson legacy_active "$legacy_active" \
  --argjson final_total "$final_total" \
  --argjson final_alive "$final_alive" \
  --argjson final_sold "$final_sold" \
  --argjson final_dead "$final_dead" \
  --argjson final_inactive "$final_inactive" \
  --arg oracle_compare_status "$oracle_compare_status" \
  --slurpfile generator "$summary_json" \
  --slurpfile pre_counts "$pre_backfill_counts" \
  --slurpfile final_counts "$final_counts" \
  --slurpfile oracle_compare "$oracle_compare_json" \
  '{
    report_root: $report_root,
    legacy_dashboard: {date: $legacy_date, active: $legacy_active},
    final: {
      total: $final_total,
      alive: $final_alive,
      sold: $final_sold,
      dead: $final_dead,
      inactive: $final_inactive
    },
    generator: $generator[0],
    pre_backfill_counts: $pre_counts[0],
    final_counts: $final_counts[0],
    oracle_compare_status: $oracle_compare_status,
    oracle_compare: ($oracle_compare[0] // null),
    active_count_parity: ($final_alive == $legacy_active),
    goat_level_parity: (
      $oracle_compare_status != "checked" or (
        (($oracle_compare[0] // {}) | .missing_in_replay // 0) == 0 and
        (($oracle_compare[0] // {}) | .extra_in_replay // 0) == 0 and
        (($oracle_compare[0] // {}) | .changed_common_keys // 0) == 0
      )
    ),
    parity: (
      ($final_alive == $legacy_active) and (
        $oracle_compare_status != "checked" or (
          (($oracle_compare[0] // {}) | .missing_in_replay // 0) == 0 and
          (($oracle_compare[0] // {}) | .extra_in_replay // 0) == 0 and
          (($oracle_compare[0] // {}) | .changed_common_keys // 0) == 0
        )
      )
    )
  }' >"$report_root/live-replay-summary.json"

cat "$report_root/live-replay-summary.json"

if [[ "$final_alive" != "$legacy_active" ]]; then
  fail "live replay active goats $final_alive does not match legacy dashboard $legacy_active for $legacy_date"
fi
if [[ "$oracle_compare_status" == "checked" ]]; then
  missing="$(jq -r '.missing_in_replay' "$oracle_compare_json")"
  extra="$(jq -r '.extra_in_replay' "$oracle_compare_json")"
  changed="$(jq -r '.changed_common_keys' "$oracle_compare_json")"
  if [[ "$missing" != "0" || "$extra" != "0" || "$changed" != "0" ]]; then
    fail "live replay goat-level parity failed against $oracle_container: missing=$missing extra=$extra changed=$changed"
  fi
fi

log "Live replay passed"
echo "import_run_id=$import_run_id"
echo "report_root=$report_root"
