#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
container_name="goatos-sqlc-plan-validation-$$"
image="${GOATOS_SQLC_POSTGRES_IMAGE:-postgres:16.9-alpine}"
db_name="goatos"
db_user="postgres"

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

explain_must_use_index "get goat by id" 'Seq Scan on goats' "
EXPLAIN (COSTS OFF)
SELECT g.goat_id::text
FROM goats g
LEFT JOIN locations loc ON loc.tenant_id = g.tenant_id AND loc.location_id = g.current_location_id
LEFT JOIN goat_identifiers old_tag ON old_tag.tenant_id = g.tenant_id
  AND old_tag.goat_id = g.goat_id
  AND old_tag.identifier_type = 'old_tag'
  AND old_tag.status = 'active'
  AND old_tag.is_primary_for_goat
LEFT JOIN goat_identifiers rfid ON rfid.tenant_id = g.tenant_id
  AND rfid.goat_id = g.goat_id
  AND rfid.identifier_type = 'rfid'
  AND rfid.status = 'active'
WHERE g.tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
  AND g.goat_id = '10000000-0000-4000-8000-000000000001'::uuid;
"

explain_must_use_index "get goat by display id" 'Seq Scan on goats' "
EXPLAIN (COSTS OFF)
SELECT g.goat_id::text
FROM goats g
WHERE g.tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
  AND g.display_id = 'G-000001';
"

explain_must_use_index "list identifiers for goat" 'Seq Scan on goat_identifiers' "
EXPLAIN (COSTS OFF)
SELECT identifier_id::text
FROM goat_identifiers
WHERE tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
  AND goat_id = '10000000-0000-4000-8000-000000000001'::uuid
ORDER BY is_primary_for_goat DESC, status ASC, valid_from DESC;
"

explain_must_use_index "find open conflict for scoped identifier" 'Seq Scan on identity_conflicts' "
EXPLAIN (COSTS OFF)
SELECT conflict_id::text
FROM identity_conflicts
WHERE tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
  AND identifier_type = 'old_tag'
  AND identifier_value = '1900'
  AND evidence->>'scope_key' = 'park:CBE'
  AND state IN ('open', 'needs_field_check')
ORDER BY created_at DESC
LIMIT 1;
"
