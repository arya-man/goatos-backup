#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
container_name="goatos-sqlc-plan-validation-$$"
image="${GOATOS_SQLC_POSTGRES_IMAGE:-postgres:16.9-alpine}"
db_name="goatos"
db_user="postgres"
query_file="$repo_root/backend/internal/identity/adapters/postgres/sqlc/query.sql"

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
  local query_name="$1"
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
    -e "s/@conflict_id/'20000000-0000-4000-8000-000000000001'::uuid/g"
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
    *)
      echo "No sqlc plan expectation registered for generated query: $query_name" >&2
      exit 1
      ;;
  esac
}

validate_generated_query_plan() {
  local query_name="$1"
  local forbidden
  local sql

  forbidden="$(forbidden_seq_scan_pattern "$query_name")"
  sql="$(extract_query "$query_name" | bind_query_params)"
  if [ -z "$(tr -d '[:space:]' <<<"$sql")" ]; then
    echo "Generated query not found in $query_file: $query_name" >&2
    exit 1
  fi

  explain_must_use_index "$query_name" "$forbidden" "EXPLAIN (COSTS OFF)
$sql"
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
while IFS= read -r query_name; do
  validate_generated_query_plan "$query_name"
  checked_count=$((checked_count + 1))
done < <(awk '/^-- name: / { print $3 }' "$query_file")

declared_count="$(awk '/^-- name: / { count++ } END { print count + 0 }' "$query_file")"
if [ "$checked_count" -ne "$declared_count" ]; then
  echo "Validated $checked_count sqlc query plans, expected $declared_count" >&2
  exit 1
fi

echo "Validated $checked_count generated sqlc query plans"
