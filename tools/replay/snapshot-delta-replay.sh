#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source "$repo_root/tools/postgres-ci.sh"

tenant_id="00000000-0000-4000-8000-000000000001"
actor_id="90000000-0000-4000-8000-000000000901"
timestamp="$(date -u +%Y%m%dT%H%M%SZ)"
report_root="$repo_root/.codex-goatos-render/replay-delta/$timestamp"
old_root="${GOATOS_DELTA_OLD_ROOT:-}"
live_root="${GOATOS_DELTA_LIVE_ROOT:-}"
legacy_bq_project="${GOATOS_LEGACY_BQ_PROJECT:-goatos-sheets}"
legacy_bq_location="${GOATOS_LEGACY_BQ_LOCATION:-US}"
old_legacy_date="${GOATOS_DELTA_OLD_LEGACY_DATE:-2026-06-15}"
old_legacy_count_json="${GOATOS_DELTA_OLD_LEGACY_COUNT_JSON:-}"
container_name="goatos-replay-delta-${USER:-user}-$$"
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
    'needs_review', count(*) FILTER (WHERE identity_state = 'needs_review'),
    'open_conflicts', (
      SELECT count(*)
      FROM identity_conflicts c
      WHERE c.tenant_id = '$tenant_id'
        AND c.state = 'open'
    )
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

apply_snapshot() {
  local label="$1"
  local rfid_xlsx="$2"
  local events_json="$3"
  local locations_json="$4"
  local candidates_csv="$5"
  local out_dir="$6"
  local backfill_locations_json="${7-$locations_json}"

  mkdir -p "$out_dir"
  assert_file "$rfid_xlsx"
  assert_file "$events_json"
  assert_file "$locations_json"
  assert_file "$candidates_csv"

  log "[$label] Discovering RFID workbook"
  local discover_out="$out_dir/rfid-discover.txt"
  run_backend_text "$discover_out" go run ./cmd/rfid-import --discover --source-type local_xlsx --input "$rfid_xlsx" --sheet Combined
  grep -q "importable=true" "$discover_out" || fail "$label RFID discovery was not importable"

  log "[$label] Staging RFID source"
  local rfid_import_out="$out_dir/rfid-import.txt"
  run_backend_text "$rfid_import_out" go run ./cmd/rfid-import \
    --input "$rfid_xlsx" \
    --sheet Combined \
    --tenant-id "$tenant_id" \
    --source-name "$label replay RFID source"

  local import_run_id
  import_run_id="$(kv_value "$rfid_import_out" "import_run_id")"
  [[ -n "$import_run_id" ]] || fail "$label rfid-import did not print import_run_id"
  echo "$import_run_id" >"$out_dir/import-run-id.txt"

  log "[$label] Applying RFID clean rows"
  run_backend_text "$out_dir/rfid-apply-normal.txt" go run ./cmd/rfid-apply \
    --tenant-id "$tenant_id" \
    --import-run-id "$import_run_id" \
    --actor-id "$actor_id"

  log "[$label] Applying BQ reconcile pass 1"
  run_backend_json "$out_dir/bq-pass1.json" go run ./cmd/bq-reconcile \
    --tenant-id "$tenant_id" \
    --import-run-id "$import_run_id" \
    --events-json "$events_json" \
    --locations-json "$locations_json" \
    --report-dir "$out_dir/bq-pass1-reports" \
    --execute \
    --timeout 20m \
    --trace-id "snapshot-delta-$label-bq-pass1-$timestamp"

  log "[$label] Applying RFID blank old-tag suffix rows"
  run_backend_text "$out_dir/rfid-apply-blank-suffix.txt" go run ./cmd/rfid-apply \
    --tenant-id "$tenant_id" \
    --import-run-id "$import_run_id" \
    --actor-id "$actor_id" \
    --allow-rfid-only-blank-suffix

  log "[$label] Applying BQ reconcile pass 2"
  run_backend_json "$out_dir/bq-pass2.json" go run ./cmd/bq-reconcile \
    --tenant-id "$tenant_id" \
    --import-run-id "$import_run_id" \
    --events-json "$events_json" \
    --locations-json "$locations_json" \
    --report-dir "$out_dir/bq-pass2-reports" \
    --execute \
    --timeout 20m \
    --trace-id "snapshot-delta-$label-bq-pass2-$timestamp"

  log "[$label] Verifying BQ settle dry run"
  run_backend_json "$out_dir/bq-final-dry-run.json" go run ./cmd/bq-reconcile \
    --tenant-id "$tenant_id" \
    --import-run-id "$import_run_id" \
    --events-json "$events_json" \
    --locations-json "$locations_json" \
    --report-dir "$out_dir/bq-final-dry-run-reports" \
    --timeout 20m \
    --trace-id "snapshot-delta-$label-bq-final-dry-$timestamp"
  assert_eq "$label.bq_final_patches_planned" "$(json_number "$out_dir/bq-final-dry-run.json" ".patches_planned")" "0"

  log "[$label] Applying old-tag passport backfill"
  local backfill_args=(
    go run ./cmd/bq-reconcile
    --tenant-id "$tenant_id" \
    --backfill-candidates-csv "$candidates_csv" \
    --report-dir "$out_dir/old-tag-backfill-reports" \
    --execute \
    --timeout 20m \
    --trace-id "snapshot-delta-$label-old-tag-backfill-$timestamp"
  )
  if [[ -n "$backfill_locations_json" ]]; then
    backfill_args+=(--locations-json "$backfill_locations_json")
  fi
  run_backend_json "$out_dir/old-tag-backfill.json" "${backfill_args[@]}"

  log "[$label] Rebuilding counters"
  run_backend_text "$out_dir/rebuild-identity-counters.txt" go run ./cmd/rebuild-identity-counters \
    --tenant-id "$tenant_id" \
    --source-import-run-id "$import_run_id"

  write_db_counts_json "$out_dir/counts.json"
}

need_cmd bq
need_cmd docker
need_cmd go
need_cmd jq
need_cmd python3

mkdir -p "$report_root"
[[ -n "$old_root" ]] || fail "GOATOS_DELTA_OLD_ROOT is required for snapshot-delta replay"

if [[ -z "$live_root" ]]; then
  live_root="$(
    find "$repo_root/.codex-goatos-render/replay-live" -type f \
      -path '*/generated/RFID-source-of-truth.live.seed.xlsx' \
      -print |
      sed 's#/generated/RFID-source-of-truth\.live\.seed\.xlsx$##' |
      sort |
      tail -1
  )"
fi
[[ -n "$live_root" && -d "$live_root" ]] || fail "missing GOATOS_DELTA_LIVE_ROOT and no replay-live directory found"

old_rfid="$old_root/rfid/RFID source of truth.xlsx"
old_events="$old_root/bq-latest/bq_events.json"
old_locations="$old_root/bq-latest/bq_latest_goat_locations.json"
old_candidates="$old_root/drive-sheet-inventory-20260615T174444Z/safe-old-tag-passport-backfill-candidates.csv"

live_rfid="$live_root/generated/RFID-source-of-truth.live.seed.xlsx"
live_events="$live_root/inputs/bq_events.live.json"
live_current_identities="$live_root/inputs/bq_current_identities.live.csv"
live_census="$live_root/inputs/Census_DB__DB.live.csv"
live_goats_db="$live_root/inputs/Goats_DB__DB.live.csv"
live_legacy_count="$live_root/inputs/legacy_dashboard_count.live.json"

assert_file "$old_rfid"
assert_file "$old_events"
assert_file "$old_locations"
assert_file "$old_candidates"
assert_file "$live_rfid"
assert_file "$live_events"
assert_file "$live_current_identities"
assert_file "$live_census"
assert_file "$live_goats_db"
assert_file "$live_legacy_count"

log "Context"
echo "old_root=$old_root"
echo "live_root=$live_root"
echo "report_root=$report_root"
echo "old_legacy_date=$old_legacy_date"

old_legacy_json="$report_root/old-legacy-count.json"
if [[ -n "$old_legacy_count_json" ]]; then
  assert_file "$old_legacy_count_json"
  cp "$old_legacy_count_json" "$old_legacy_json"
else
  bq_query "$old_legacy_json" "json" "
  SELECT
    CAST(date AS STRING) AS date,
    farm_total_count,
    cbe_summary_count,
    cpt_summary_count,
    procurement_summary_count
  FROM \`$legacy_bq_project.farm.daily_summary_dev\`
  WHERE DATE(date) = DATE '$old_legacy_date'
  LIMIT 1
  "
fi
jq -e 'length == 1' "$old_legacy_json" >/dev/null
old_legacy_active="$(jq -r '.[0].farm_total_count' "$old_legacy_json")"
live_legacy_date="$(jq -r '.[0].date' "$live_legacy_count")"
live_legacy_active="$(jq -r '.[0].farm_total_count' "$live_legacy_count")"
echo "old_legacy_active=$old_legacy_active"
echo "live_legacy_date=$live_legacy_date"
echo "live_legacy_active=$live_legacy_active"

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

old_stage="$report_root/old-stage"
# The saved June-15 candidate CSV is the historical source of old-tag statuses.
# The companion "latest locations" export is not date-scoped, so feeding it into
# old-tag backfill would apply future lifecycle overrides and fake a current
# baseline before the delta stage. Keep it for BQ reconcile, but do not use it
# for historical old-tag backfill unless a date-scoped override file is supplied.
old_backfill_locations="${GOATOS_DELTA_OLD_BACKFILL_LOCATIONS:-}"
apply_snapshot "old" "$old_rfid" "$old_events" "$old_locations" "$old_candidates" "$old_stage" "$old_backfill_locations"
old_alive="$(jq -r '.alive' "$old_stage/counts.json")"
echo "old_stage_counts=$(cat "$old_stage/counts.json")"
assert_eq "old_stage_alive_vs_legacy_$old_legacy_date" "$old_alive" "$old_legacy_active"

log "[live-delta] Generating candidates from current live source against old-loaded DB"
live_delta="$report_root/live-delta-stage"
mkdir -p "$live_delta"
live_existing_identifiers="$live_delta/existing-identifiers-before-live-delta.csv"
live_locations="$live_delta/bq_latest_goat_locations.live-delta.json"
live_candidates="$live_delta/safe-old-tag-passport-backfill-candidates.live-delta.csv"
live_skipped="$live_delta/safe-old-tag-passport-backfill-skipped.live-delta.csv"
live_generator_summary="$live_delta/live-generator-summary.live-delta.json"
export_existing_identifiers "$live_existing_identifiers"
python3 "$repo_root/tools/replay/generate-live-replay-inputs.py" \
  --current-identities-csv "$live_current_identities" \
  --census-csv "$live_census" \
  --goats-db-events-csv "$live_goats_db" \
  --existing-identifiers-csv "$live_existing_identifiers" \
  --locations-json-out "$live_locations" \
  --candidates-csv-out "$live_candidates" \
  --skipped-csv-out "$live_skipped" \
  --summary-json-out "$live_generator_summary" | tee "$live_delta/live-generator-summary.live-delta.pretty.json"

apply_snapshot "live-delta" "$live_rfid" "$live_events" "$live_locations" "$live_candidates" "$live_delta"
live_alive="$(jq -r '.alive' "$live_delta/counts.json")"
echo "live_delta_counts=$(cat "$live_delta/counts.json")"
assert_eq "live_delta_alive_vs_legacy_$live_legacy_date" "$live_alive" "$live_legacy_active"

oracle_compare_json="$report_root/oracle-compare/summary.json"
oracle_compare_status="skipped"
mkdir -p "$(dirname "$oracle_compare_json")"
printf 'null\n' >"$oracle_compare_json"
if [[ -n "${GOATOS_REPLAY_ORACLE_CONTAINER:-goatos-local-current-persist}" ]] && \
   docker ps --format '{{.Names}}' | grep -Fxq "${GOATOS_REPLAY_ORACLE_CONTAINER:-goatos-local-current-persist}"; then
  oracle_container="${GOATOS_REPLAY_ORACLE_CONTAINER:-goatos-local-current-persist}"
  oracle_compare_status="checked"
  log "Comparing live-delta DB against local oracle container $oracle_container"
  export_identity_snapshot_from_container "$oracle_container" "$report_root/oracle-compare/oracle.tsv"
  export_identity_snapshot_from_container "$container_name" "$report_root/oracle-compare/replay.tsv"
  python3 "$repo_root/tools/replay/compare-replay-to-oracle.py" \
    --oracle-tsv "$report_root/oracle-compare/oracle.tsv" \
    --replay-tsv "$report_root/oracle-compare/replay.tsv" \
    --out-dir "$report_root/oracle-compare" \
    --summary-json "$oracle_compare_json" | tee "$report_root/oracle-compare/summary.pretty.json"
fi

if [[ "$oracle_compare_status" == "checked" ]]; then
  missing="$(jq -r '.missing_in_replay' "$oracle_compare_json")"
  extra="$(jq -r '.extra_in_replay' "$oracle_compare_json")"
  changed="$(jq -r '.changed_common_keys' "$oracle_compare_json")"
  if [[ "$missing" != "0" || "$extra" != "0" || "$changed" != "0" ]]; then
    fail "live-delta goat-level parity failed against local oracle: missing=$missing extra=$extra changed=$changed"
  fi
fi

jq -n \
  --arg report_root "$report_root" \
  --arg old_root "$old_root" \
  --arg live_root "$live_root" \
  --arg old_legacy_date "$old_legacy_date" \
  --arg live_legacy_date "$live_legacy_date" \
  --arg oracle_compare_status "$oracle_compare_status" \
  --slurpfile old_counts "$old_stage/counts.json" \
  --slurpfile live_counts "$live_delta/counts.json" \
  --slurpfile old_legacy "$old_legacy_json" \
  --slurpfile live_legacy "$live_legacy_count" \
  --slurpfile oracle_compare "$oracle_compare_json" \
  '{
    report_root: $report_root,
    old_root: $old_root,
    live_root: $live_root,
    old_legacy: {date: $old_legacy_date, active: ($old_legacy[0][0].farm_total_count | tonumber)},
    old_counts: $old_counts[0],
    live_legacy: {date: $live_legacy_date, active: ($live_legacy[0][0].farm_total_count | tonumber)},
    live_delta_counts: $live_counts[0],
    oracle_compare_status: $oracle_compare_status,
    oracle_compare: ($oracle_compare[0] // null),
    parity: (
      ($old_counts[0].alive == ($old_legacy[0][0].farm_total_count | tonumber)) and
      ($live_counts[0].alive == ($live_legacy[0][0].farm_total_count | tonumber)) and
      (
        $oracle_compare_status != "checked" or (
          (($oracle_compare[0] // {}) | .missing_in_replay // 0) == 0 and
          (($oracle_compare[0] // {}) | .extra_in_replay // 0) == 0 and
          (($oracle_compare[0] // {}) | .changed_common_keys // 0) == 0
        )
      )
    )
  }' >"$report_root/snapshot-delta-summary.json"
cat "$report_root/snapshot-delta-summary.json"

log "Snapshot delta replay passed"
echo "report_root=$report_root"
