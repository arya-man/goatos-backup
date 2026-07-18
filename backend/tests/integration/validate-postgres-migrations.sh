#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
source "$repo_root/tools/postgres-ci.sh"

bash "$repo_root/backend/tests/integration/validate-hot-index-migrations.sh"

container_name="goatos-migration-validation-$$"
image="${GOATOS_POSTGRES_IMAGE:-postgres:16.9-alpine}"
db_name="goatos"
db_user="postgres"

cleanup() {
  docker rm -f "$container_name" >/dev/null 2>&1 || true
}
trap cleanup EXIT

run_psql() {
  postgres_ci_psql "$container_name" "$db_user" "$db_name" "$@"
}

validate_goose_structure() {
  local duplicate_versions
  duplicate_versions="$(find "$repo_root/backend/migrations/postgres" -maxdepth 1 -type f -name '*.sql' \
    | awk -F/ '{print $NF}' \
    | sed -E 's/^([0-9]+)_.*/\1/' \
    | sort \
    | uniq -d)"
  if [[ -n "$duplicate_versions" ]]; then
    echo "Duplicate migration version(s) detected:" >&2
    while IFS= read -r version; do
      [[ -z "$version" ]] && continue
      find "$repo_root/backend/migrations/postgres" -maxdepth 1 -type f -name "${version}_*.sql" \
        -exec basename {} \; | sort >&2
    done <<<"$duplicate_versions"
    exit 1
  fi

  local migration up_count down_count up_line down_line
  while IFS= read -r migration; do
    up_count="$(grep -cE '^-- \+goose Up[[:space:]]*$' "$migration" || true)"
    down_count="$(grep -cE '^-- \+goose Down[[:space:]]*$' "$migration" || true)"
    if [[ "$up_count" -ne 1 || "$down_count" -ne 1 ]]; then
      echo "$(basename "$migration"): expected exactly one -- +goose Up and one -- +goose Down marker" >&2
      exit 1
    fi
    up_line="$(grep -nE '^-- \+goose Up[[:space:]]*$' "$migration" | cut -d: -f1)"
    down_line="$(grep -nE '^-- \+goose Down[[:space:]]*$' "$migration" | cut -d: -f1)"
    if (( up_line >= down_line )); then
      echo "$(basename "$migration"): -- +goose Up must appear before -- +goose Down" >&2
      exit 1
    fi
  done < <(find "$repo_root/backend/migrations/postgres" -maxdepth 1 -type f -name '*.sql' | sort)
}

validate_clean_slate_baseline() {
  local count
  count="$(find "$repo_root/backend/migrations/postgres" -maxdepth 1 -type f -name '*.sql' | wc -l | tr -d ' ')"
  if [[ "$count" != "1" ]]; then
    echo "clean-slate baseline expects exactly one Postgres migration, found $count" >&2
    exit 1
  fi
  if [[ ! -f "$repo_root/backend/migrations/postgres/000001_goatos_clean_slate_baseline.sql" ]]; then
    echo "missing clean-slate baseline migration 000001_goatos_clean_slate_baseline.sql" >&2
    exit 1
  fi
}

apply_goose_up() {
  local migration="$1"
  awk '
    /^-- \+goose Up/ { in_up = 1; next }
    /^-- \+goose Down/ { in_up = 0 }
    in_up { print }
  ' "$migration" | run_psql
}

expect_failure() {
  local label="$1"
  local sql="$2"

  if printf '%s\n' "$sql" | run_psql >/tmp/goatos-migration-expected-failure.log 2>&1; then
    cat /tmp/goatos-migration-expected-failure.log
    echo "Expected failure did not occur: $label" >&2
    exit 1
  fi

  echo "Expected failure observed: $label"
}

validate_goose_structure
validate_clean_slate_baseline

docker run --rm --name "$container_name" \
  -e POSTGRES_PASSWORD=goatos \
  -e POSTGRES_DB="$db_name" \
  -d "$image" >/dev/null

postgres_ci_wait_ready "$container_name" "$db_user" "$db_name"

while IFS= read -r migration; do
  echo "Applying $(basename "$migration")"
  apply_goose_up "$migration"
done < <(find "$repo_root/backend/migrations/postgres" -maxdepth 1 -type f -name '*.sql' | sort)

run_psql <<'SQL'
\echo 'Running clean-slate GoatOS invariant checks'

SELECT tenant_id
FROM tenants
WHERE name = 'Mesha'
  AND status = 'active';

DO $$
DECLARE
  tenant uuid := '00000000-0000-4000-8000-000000000001';
  mesha_party uuid := '00000000-0000-4000-8000-000000001001';
  cbe uuid := '00000000-0000-4000-8000-000000003001';
  goat_animal uuid := '10000000-0000-4000-8000-000000000001';
  sheep_animal uuid := '10000000-0000-4000-8000-000000000002';
  survivor uuid := '10000000-0000-4000-8000-000000000003';
  merged uuid := '10000000-0000-4000-8000-000000000004';
  decision uuid := '20000000-0000-4000-8000-000000000001';
  event_table text;
  audit_table text;
BEGIN
  INSERT INTO goats (
    goat_id, tenant_id, species, breed, sex, approx_dob, lifecycle_status,
    reproductive_status, management_stage, health_status, custodian_party_id,
    current_location_id, park_id, shed_id, origin_type
  )
  VALUES
    (goat_animal, tenant, 'goat', 'osmanabadi', 'female', DATE '2026-06-01', 'alive', 'open', 'K1', 'healthy', mesha_party, cbe, cbe, cbe, 'birth'),
    (sheep_animal, tenant, 'sheep', 'anantapur', 'male', DATE '2026-05-01', 'alive', 'open', 'K2', 'healthy', mesha_party, cbe, cbe, cbe, 'procured'),
    (survivor, tenant, 'goat', 'beetal', 'female', DATE '2025-01-01', 'alive', 'open', 'adult', 'healthy', mesha_party, cbe, cbe, cbe, 'imported'),
    (merged, tenant, 'goat', 'beetal', 'female', DATE '2025-01-01', 'alive', 'open', 'adult', 'healthy', mesha_party, cbe, cbe, cbe, 'imported');

  INSERT INTO goat_identifiers (
    identifier_id, tenant_id, goat_id, identifier_type, identifier_value,
    normalized_value, scope_key, is_primary_for_goat, status, valid_from,
    source_system, source_record_id, normalizer_version
  )
  VALUES
    ('30000000-0000-4000-8000-000000000001', tenant, goat_animal, 'animal_identifier_1', 'ANIMAL-A1', 'ANIMAL-A1', 'global', true, 'active', now(), 'validation', 'goat-a-1', 'test_v1'),
    ('30000000-0000-4000-8000-000000000002', tenant, goat_animal, 'animal_identifier_2', 'ANIMAL-A2', 'ANIMAL-A2', 'global', false, 'active', now(), 'validation', 'goat-a-2', 'test_v1'),
    ('30000000-0000-4000-8000-000000000003', tenant, sheep_animal, 'animal_identifier_1', 'ANIMAL-B1', 'ANIMAL-B1', 'global', true, 'active', now(), 'validation', 'sheep-b-1', 'test_v1'),
    ('30000000-0000-4000-8000-000000000004', tenant, sheep_animal, 'animal_identifier_2', 'ANIMAL-B2', 'ANIMAL-B2', 'global', false, 'active', now(), 'validation', 'sheep-b-2', 'test_v1'),
    ('30000000-0000-4000-8000-000000000005', tenant, survivor, 'animal_identifier_1', 'ANIMAL-C1', 'ANIMAL-C1', 'global', true, 'active', now(), 'validation', 'survivor-c-1', 'test_v1'),
    ('30000000-0000-4000-8000-000000000006', tenant, survivor, 'animal_identifier_2', 'ANIMAL-C2', 'ANIMAL-C2', 'global', false, 'active', now(), 'validation', 'survivor-c-2', 'test_v1'),
    ('30000000-0000-4000-8000-000000000007', tenant, merged, 'animal_identifier_1', 'ANIMAL-D1', 'ANIMAL-D1', 'global', true, 'active', now(), 'validation', 'merged-d-1', 'test_v1'),
    ('30000000-0000-4000-8000-000000000008', tenant, merged, 'animal_identifier_2', 'ANIMAL-D2', 'ANIMAL-D2', 'global', false, 'active', now(), 'validation', 'merged-d-2', 'test_v1');

  INSERT INTO identity_decisions (
    decision_id, tenant_id, decision_type, decision_result, decision_state,
    decided_by_type, policy_version, evidence
  )
  VALUES (
    decision, tenant, 'merge_goats', 'same_goat_merge', 'approved',
    'human', 'clean-slate-v1', '{"evidence_refs":[{"evidence_type":"source_record","evidence_id":"synthetic-validation"}]}'::jsonb
  );

  UPDATE goats
  SET lifecycle_status = 'merged',
      merged_into_goat_id = survivor,
      row_version = row_version + 1
  WHERE goat_id = merged;

  INSERT INTO goat_merge_links (
    merge_link_id, tenant_id, survivor_goat_id, merged_goat_id, decision_id,
    reason, created_by
  )
  VALUES (
    '40000000-0000-4000-8000-000000000001', tenant, survivor, merged, decision,
    'Synthetic merge validation.', '50000000-0000-4000-8000-000000000001'
  );

  INSERT INTO goat_identity_events (
    identity_event_id, tenant_id, goat_id, event_type, event_version,
    occurred_at, recorded_at, payload, decision_id, idempotency_key
  )
  VALUES (
    '60000000-0000-4000-8000-000000000001', tenant, goat_animal,
    'goat.created', 1, '2026-06-15 08:00:00+00',
    '2026-06-15 08:00:01+00', '{"synthetic":true}'::jsonb,
    decision, 'validation-event-june'
  )
  RETURNING tableoid::regclass::text INTO event_table;

  IF event_table <> 'goat_identity_events' THEN
    RAISE EXCEPTION 'expected ordinary goat_identity_events table, got %', event_table;
  END IF;

  INSERT INTO audit_log (
    audit_id, tenant_id, actor_type, action, resource_type, resource_id,
    metadata, recorded_at
  )
  VALUES (
    '70000000-0000-4000-8000-000000000001', tenant, 'system_rule',
    'validation.audit', 'goat', goat_animal,
    '{"synthetic":true}'::jsonb, '2026-06-15 08:00:02+00'
  )
  RETURNING tableoid::regclass::text INTO audit_table;

  IF audit_table <> 'audit_log' THEN
    RAISE EXCEPTION 'expected ordinary audit_log table, got %', audit_table;
  END IF;
END $$;

DO $$
DECLARE
  table_name text;
  relation_kind "char";
BEGIN
  FOREACH table_name IN ARRAY ARRAY[
    'goat_identity_events',
    'audit_log',
    'obligation_status_events'
  ] LOOP
    SELECT relkind INTO relation_kind FROM pg_class WHERE oid = table_name::regclass;
    IF relation_kind <> 'r' THEN
      RAISE EXCEPTION 'expected % to be an ordinary table, relkind=%', table_name, relation_kind;
    END IF;
  END LOOP;

  IF EXISTS (SELECT 1 FROM pg_partitioned_table WHERE partrelid IN (
    'goat_identity_events'::regclass,
    'audit_log'::regclass,
    'obligation_status_events'::regclass
  )) THEN
    RAISE EXCEPTION 'operational history partition parent remains in clean-slate baseline';
  END IF;

  IF to_regprocedure('goatos_ensure_partition_coverage(timestamptz,integer)') IS NOT NULL THEN
    RAISE EXCEPTION 'partition maintenance functions remain in clean-slate baseline';
  END IF;
END $$;
SQL

expect_failure "import-port table is absent" "SELECT count(*) FROM legacy_import_runs;"
expect_failure "sync-port table is absent" "SELECT count(*) FROM legacy_sync_runs;"
expect_failure "candidate match table is absent" "SELECT count(*) FROM identity_match_candidates;"
expect_failure "identity counter table is absent" "SELECT count(*) FROM goat_identity_counters;"
expect_failure "snapshot mirror table is absent" "SELECT count(*) FROM counts_current_snapshot_rows;"
expect_failure "identity_state column is absent" "SELECT identity_state FROM goats LIMIT 1;"
expect_failure "source_confidence column is absent" "SELECT source_confidence FROM goats LIMIT 1;"
expect_failure "unknown sex is rejected" "
INSERT INTO goats (
  goat_id, tenant_id, species, sex, lifecycle_status, custodian_party_id, current_location_id, park_id, origin_type
) VALUES (
  '10000000-0000-4000-8000-000000000101',
  '00000000-0000-4000-8000-000000000001',
  'goat',
  'unknown',
  'alive',
  '00000000-0000-4000-8000-000000001001',
  '00000000-0000-4000-8000-000000003001',
  '00000000-0000-4000-8000-000000003001',
  'birth'
);
"
expect_failure "invalid identifier type is rejected" "
INSERT INTO goat_identifiers (
  tenant_id, goat_id, identifier_type, identifier_value, normalized_value,
  scope_key, is_primary_for_goat, status, valid_from, normalizer_version
) VALUES (
  '00000000-0000-4000-8000-000000000001',
  '10000000-0000-4000-8000-000000000001',
  'rfid',
  'RFID-OLD-NAME',
  'RFID-OLD-NAME',
  'global',
  false,
  'active',
  now(),
  'test_v1'
);
"
expect_failure "duplicate lifetime Animal ID is rejected" "
INSERT INTO goat_identifiers (
  tenant_id, goat_id, identifier_type, identifier_value, normalized_value,
  scope_key, is_primary_for_goat, status, valid_from, normalizer_version
) VALUES (
  '00000000-0000-4000-8000-000000000001',
  '10000000-0000-4000-8000-000000000002',
  'animal_identifier_2',
  'ANIMAL-A1',
  'ANIMAL-A1',
  'global',
  false,
  'active',
  now(),
  'test_v1'
);
"
expect_failure "normal update to merged animal is blocked" "
UPDATE goats
SET breed = 'blocked'
WHERE goat_id = '10000000-0000-4000-8000-000000000004';
"
expect_failure "child identifier write to merged animal is blocked" "
INSERT INTO goat_identifiers (
  tenant_id, goat_id, identifier_type, identifier_value, normalized_value,
  scope_key, is_primary_for_goat, status, valid_from, normalizer_version
) VALUES (
  '00000000-0000-4000-8000-000000000001',
  '10000000-0000-4000-8000-000000000004',
  'animal_identifier_2',
  'ANIMAL-D-REPLACEMENT',
  'ANIMAL-D-REPLACEMENT',
  'global',
  false,
  'active',
  now(),
  'test_v1'
);
"

echo "Migration validation passed"
