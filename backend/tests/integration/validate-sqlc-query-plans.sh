#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
container_name="goatos-sqlc-plan-validation-$$"
image="${GOATOS_SQLC_POSTGRES_IMAGE:-${GOATOS_POSTGRES_IMAGE:-postgres:16.9-alpine}}"
db_name="goatos"
db_user="postgres"
query_files=(
  "$repo_root/backend/internal/identity/adapters/postgres/sqlc/query.sql"
  "$repo_root/backend/internal/legacy_import/adapters/postgres/sqlc/query.sql"
  "$repo_root/backend/internal/reporting/adapters/postgres/sqlc/query.sql"
)

cleanup() {
  docker rm -f "$container_name" >/dev/null 2>&1 || true
}
trap cleanup EXIT

run_psql() {
  docker exec -i "$container_name" psql -v ON_ERROR_STOP=1 -U "$db_user" -d "$db_name" "$@"
}

apply_goose_up() {
  local migration="$1"
  awk '
    /^-- \+goose Up/ { in_up = 1; next }
    /^-- \+goose Down/ { in_up = 0 }
    in_up { print }
  ' "$migration" | run_psql
}

explain_must_use_index() {
  local label="$1"
  local forbidden="$2"
  local sql="$3"
  local plan

  plan="$(
    {
      printf '%s\n' 'SET enable_seqscan = off;'
      printf '%s\n' "$sql"
    } | run_psql
  )"

  if grep -E "$forbidden" <<<"$plan" >/dev/null; then
    echo "$plan"
    echo "Unexpected sequential scan in $label" >&2
    exit 1
  fi
  if ! grep -E '(Index Scan|Index Only Scan|Bitmap Index Scan)' <<<"$plan" >/dev/null; then
    echo "$plan"
    echo "Expected indexed plan in $label" >&2
    exit 1
  fi
  echo "Indexed plan observed: $label"
}

extract_query() {
  local query_file="$1"
  local query_name="$2"
  awk -v query_name="$query_name" '
    /^-- name: / {
      in_query = ($3 == query_name)
      next
    }
    in_query { print }
  ' "$query_file"
}

bind_query_params() {
  sed \
    -e "s/@tenant_id/'00000000-0000-4000-8000-000000000001'::uuid/g" \
    -e "s/@goat_id/'10000000-0000-4000-8000-000000000001'::uuid/g" \
    -e "s/@display_id/'G-000001'/g" \
    -e "s/@identifier_type/'old_tag'/g" \
    -e "s/@identifier_value/'1900'/g" \
    -e "s/@scope_key/'park:CBE'/g" \
    -e "s/@conflict_id/'20000000-0000-4000-8000-000000000001'::uuid/g" \
    -e "s/@policy_version/'phase1-rfid-db-import-v1'/g" \
    -e "s/@import_run_id/'30000000-0000-4000-8000-000000000001'::uuid/g" \
    -e "s/@source_system/'legacy_rfid_db'/g" \
    -e "s/@source_dataset/'rfid_db_first_import'/g" \
    -e "s/@source_row_key/'source_system=legacy_rfid_db|source_dataset=rfid_db_first_import|normalized_old_tag=1900|normalized_park_code=CBE|rfid=RFID_SYNTHETIC_0001'/g" \
    -e "s/@source_row_version_hash/'sha256:0000000000000000000000000000000000000000000000000000000000000000'/g" \
    -e "s/@counter_grain/'tenant_lifecycle'/g" \
    -e "s/sqlc.narg('custodian_party_id')::uuid/NULL::uuid/g" \
    -e "s/sqlc.narg('farm_id')::uuid/NULL::uuid/g" \
    -e "s/sqlc.narg('park_id')::uuid/NULL::uuid/g" \
    -e "s/sqlc.narg('shed_id')::uuid/NULL::uuid/g" \
    -e "s/sqlc.narg('cohort_id')::uuid/NULL::uuid/g" \
    -e "s/sqlc.narg('lifecycle_status')::text/NULL::text/g" \
    -e "s/sqlc.narg('reproductive_status')::text/NULL::text/g" \
    -e "s/sqlc.narg('growth_cohort_tag')::text/NULL::text/g" \
    -e "s/sqlc.narg('management_stage')::text/NULL::text/g" \
    -e "s/sqlc.narg('health_status')::text/NULL::text/g" \
    -e "s/sqlc.narg('identity_state')::text/NULL::text/g" \
    -e "s/sqlc.narg('breed_id')::uuid/NULL::uuid/g" \
    -e "s/sqlc.narg('sex')::text/NULL::text/g" \
    -e "s/sqlc.narg('cursor_count_value')::bigint/NULL::bigint/g" \
    -e "s/sqlc.narg('cursor_counter_id')::uuid/NULL::uuid/g" \
    -e "s/sqlc.narg('cursor_created_at')::timestamptz/NULL::timestamptz/g" \
    -e "s/sqlc.narg('cursor_candidate_id')::uuid/NULL::uuid/g" \
    -e "s/sqlc.narg('cursor_row_number')::int/NULL::int/g" \
    -e "s/sqlc.narg('cursor_legacy_row_id')::uuid/NULL::uuid/g" \
    -e "s/@limit_count/10/g"
}

forbidden_seq_scan_pattern() {
  local query_name="$1"
  case "$query_name" in
    GetGoatByID|GetGoatByDisplayID)
      printf '%s\n' 'Seq Scan on (goats|goat_identifiers)'
      ;;
    ListIdentifiersForGoat)
      printf '%s\n' 'Seq Scan on goat_identifiers'
      ;;
    FindOpenConflictForIdentifier)
      printf '%s\n' 'Seq Scan on identity_conflicts'
      ;;
    GetConflictSummaryByID)
      printf '%s\n' 'Seq Scan on (identity_conflicts|identity_conflict_goats|identity_conflict_source_records)'
      ;;
    ListConflictGoatsByID)
      printf '%s\n' 'Seq Scan on identity_conflict_goats'
      ;;
    ListConflictSourceRecordsByID)
      printf '%s\n' 'Seq Scan on identity_conflict_source_records'
      ;;
    ListIdentityCandidates)
      printf '%s\n' 'Seq Scan on identity_match_candidates'
      ;;
    GetApprovedLegacyImportPolicy)
      printf '%s\n' 'Seq Scan on legacy_import_policies'
      ;;
    HasLegacyImportRowWithDifferentHash)
      printf '%s\n' 'Seq Scan on legacy_import_rows'
      ;;
    GetLegacyImportRunForApply)
      printf '%s\n' 'Seq Scan on legacy_import_runs'
      ;;
    ListPendingLegacyImportRowsForApply)
      printf '%s\n' 'Seq Scan on legacy_import_rows'
      ;;
    ListIdentityCounts)
      printf '%s\n' 'Seq Scan on goat_identity_counters'
      ;;
    *)
      echo "No sqlc plan expectation registered for generated query: $query_name" >&2
      exit 1
      ;;
  esac
}

validate_generated_query_plan() {
  local query_file="$1"
  local query_name="$2"
  local forbidden
  local sql

  forbidden="$(forbidden_seq_scan_pattern "$query_name")"
  sql="$(extract_query "$query_file" "$query_name" | bind_query_params)"
  if [ -z "$(tr -d '[:space:]' <<<"$sql")" ]; then
    echo "Generated query not found in $query_file: $query_name" >&2
    exit 1
  fi

  explain_must_use_index "$query_name" "$forbidden" "EXPLAIN (COSTS OFF)
$sql"
}

validate_outbox_claim_plan() {
  explain_must_use_index "ClaimPendingOutboxMessages" 'Seq Scan on outbox_messages' "EXPLAIN (COSTS OFF)
SELECT
  outbox_id::text,
  tenant_id::text,
  event_id::text,
  event_type,
  schema_version,
  aggregate_type,
  aggregate_id::text,
  topic,
  headers,
  payload,
  idempotency_key,
  trace_id,
  attempt_count,
  created_at,
  updated_at
FROM outbox_messages
WHERE status = 'pending'
  AND (next_attempt_at IS NULL OR next_attempt_at <= '2026-06-09T12:00:00Z'::timestamptz)
ORDER BY created_at, outbox_id
LIMIT 10
FOR UPDATE SKIP LOCKED"
}

docker run --rm --name "$container_name" \
  -e POSTGRES_PASSWORD=goatos \
  -e POSTGRES_DB="$db_name" \
  -d "$image" >/dev/null

for _ in $(seq 1 60); do
  if docker exec "$container_name" pg_isready -U "$db_user" -d "$db_name" >/dev/null 2>&1; then
    break
  fi
  sleep 1
done

docker exec "$container_name" pg_isready -U "$db_user" -d "$db_name" >/dev/null

while IFS= read -r migration; do
  apply_goose_up "$migration"
done < <(find "$repo_root/backend/migrations/postgres" -maxdepth 1 -type f -name '*.sql' | sort)

checked_count=0
declared_count=0
for query_file in "${query_files[@]}"; do
  while IFS= read -r query_name; do
    validate_generated_query_plan "$query_file" "$query_name"
    checked_count=$((checked_count + 1))
  done < <(awk '/^-- name: / { print $3 }' "$query_file")
  file_count="$(awk '/^-- name: / { count++ } END { print count + 0 }' "$query_file")"
  declared_count=$((declared_count + file_count))
done

if [ "$checked_count" -ne "$declared_count" ]; then
  echo "Validated $checked_count sqlc query plans, expected $declared_count" >&2
  exit 1
fi

validate_outbox_claim_plan

echo "Validated $checked_count generated sqlc query plans and 1 hand-written outbox query plan"
