#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
container_name="goatos-migration-validation-$$"
image="${GOATOS_POSTGRES_IMAGE:-postgres:16-alpine}"
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
  echo "Applying $(basename "$migration")"
  apply_goose_up "$migration"
done < <(find "$repo_root/backend/migrations/postgres" -maxdepth 1 -type f -name '*.sql' | sort)

run_psql <<'SQL'
\echo 'Running positive invariant checks'

SELECT plan_seed.tenant_id
FROM tenants plan_seed
WHERE plan_seed.name = 'Mesha'
  AND plan_seed.status = 'active';

DO $$
DECLARE
  tenant uuid := '00000000-0000-4000-8000-000000000001';
  mesha_party uuid := '00000000-0000-4000-8000-000000001001';
  cbe uuid := '00000000-0000-4000-8000-000000003001';
  goat_a uuid := '10000000-0000-4000-8000-000000000001';
  goat_b uuid := '10000000-0000-4000-8000-000000000002';
  goat_c uuid := '10000000-0000-4000-8000-000000000003';
  survivor uuid := '10000000-0000-4000-8000-000000000004';
  merged uuid := '10000000-0000-4000-8000-000000000005';
  decision uuid := '20000000-0000-4000-8000-000000000001';
  event_partition text;
  audit_partition text;
BEGIN
  INSERT INTO goats (goat_id, tenant_id, lifecycle_status, identity_state, custodian_party_id, current_location_id, park_id)
  VALUES
    (goat_a, tenant, 'alive', 'clean', mesha_party, cbe, cbe),
    (goat_b, tenant, 'alive', 'clean', mesha_party, cbe, cbe),
    (goat_c, tenant, 'alive', 'clean', mesha_party, cbe, cbe),
    (survivor, tenant, 'alive', 'clean', mesha_party, cbe, cbe),
    (merged, tenant, 'alive', 'needs_review', mesha_party, cbe, cbe);

  INSERT INTO goat_identifiers (identifier_id, tenant_id, goat_id, identifier_type, identifier_value, normalized_value, scope_key, status, valid_from, normalizer_version)
  VALUES
    ('30000000-0000-4000-8000-000000000001', tenant, goat_a, 'rfid', 'RFID_EXAMPLE_A', 'RFID_EXAMPLE_A', 'global', 'active', now(), 'test_v1'),
    ('30000000-0000-4000-8000-000000000002', tenant, goat_a, 'old_tag', '1900', '1900', 'park:CBE', 'active', now(), 'test_v1'),
    ('30000000-0000-4000-8000-000000000003', tenant, goat_b, 'old_tag', '1900', '1900', 'park:CPT', 'active', now(), 'test_v1');

  INSERT INTO identity_decisions (decision_id, tenant_id, decision_type, decision_result, decision_state, decided_by_type, policy_version, evidence)
  VALUES (decision, tenant, 'merge_goats', 'same_goat_merge', 'approved', 'human', 'phase1-identifier-v1', '{"evidence_refs":[{"evidence_type":"source_record","evidence_id":"synthetic-validation"}]}'::jsonb);

  UPDATE goats
  SET identity_state = 'merged',
      lifecycle_status = 'merged',
      merged_into_goat_id = survivor,
      row_version = row_version + 1
  WHERE goat_id = merged;

  INSERT INTO goat_merge_links (merge_link_id, tenant_id, survivor_goat_id, merged_goat_id, decision_id, reason, created_by)
  VALUES ('40000000-0000-4000-8000-000000000001', tenant, survivor, merged, decision, 'Synthetic merge validation.', '50000000-0000-4000-8000-000000000001');

  INSERT INTO goat_identity_events (
    identity_event_id,
    tenant_id,
    goat_id,
    event_type,
    event_version,
    occurred_at,
    recorded_at,
    payload,
    decision_id,
    idempotency_key
  )
  VALUES (
    '60000000-0000-4000-8000-000000000001',
    tenant,
    goat_a,
    'goat.created',
    1,
    '2026-06-15 08:00:00+00',
    '2026-06-15 08:00:01+00',
    '{"synthetic":true}'::jsonb,
    decision,
    'validation-event-june'
  )
  RETURNING tableoid::regclass::text INTO event_partition;

  IF event_partition <> 'goat_identity_events_2026_06' THEN
    RAISE EXCEPTION 'expected June event partition, got %', event_partition;
  END IF;

  INSERT INTO goat_identity_events (
    identity_event_id,
    tenant_id,
    goat_id,
    event_type,
    event_version,
    occurred_at,
    recorded_at,
    payload,
    decision_id,
    idempotency_key
  )
  VALUES (
    '60000000-0000-4000-8000-000000000002',
    tenant,
    goat_a,
    'goat.identity.updated',
    1,
    '2035-01-01 00:00:00+00',
    '2035-01-01 00:00:01+00',
    '{"synthetic":true}'::jsonb,
    decision,
    'validation-event-default'
  )
  RETURNING tableoid::regclass::text INTO event_partition;

  IF event_partition <> 'goat_identity_events_default' THEN
    RAISE EXCEPTION 'expected default event partition, got %', event_partition;
  END IF;

  INSERT INTO audit_log (audit_id, tenant_id, actor_type, action, resource_type, resource_id, metadata, recorded_at)
  VALUES (
    '70000000-0000-4000-8000-000000000001',
    tenant,
    'system_rule',
    'validation.audit',
    'goat',
    goat_a,
    '{"synthetic":true}'::jsonb,
    '2026-06-15 08:00:02+00'
  )
  RETURNING tableoid::regclass::text INTO audit_partition;

  IF audit_partition <> 'audit_log_2026_06' THEN
    RAISE EXCEPTION 'expected June audit partition, got %', audit_partition;
  END IF;

  INSERT INTO audit_log (audit_id, tenant_id, actor_type, action, resource_type, resource_id, metadata, recorded_at)
  VALUES (
    '70000000-0000-4000-8000-000000000002',
    tenant,
    'system_rule',
    'validation.audit.default',
    'goat',
    goat_a,
    '{"synthetic":true}'::jsonb,
    '2035-01-01 00:00:02+00'
  )
  RETURNING tableoid::regclass::text INTO audit_partition;

  IF audit_partition <> 'audit_log_default' THEN
    RAISE EXCEPTION 'expected default audit partition, got %', audit_partition;
  END IF;
END $$;
SQL

expect_failure "bad active ownership share total fails at commit" "
BEGIN;
INSERT INTO goat_ownership (tenant_id, goat_id, owner_party_id, share_bps, valid_from, status)
VALUES (
  '00000000-0000-4000-8000-000000000001',
  '10000000-0000-4000-8000-000000000001',
  '00000000-0000-4000-8000-000000001001',
  5000,
  now(),
  'active'
);
COMMIT;
"

run_psql <<'SQL'
BEGIN;
INSERT INTO goat_ownership (tenant_id, goat_id, owner_party_id, share_bps, valid_from, status)
VALUES (
  '00000000-0000-4000-8000-000000000001',
  '10000000-0000-4000-8000-000000000001',
  '00000000-0000-4000-8000-000000001001',
  10000,
  now(),
  'active'
);
COMMIT;
SQL

expect_failure "duplicate active RFID fails" "
INSERT INTO goat_identifiers (tenant_id, goat_id, identifier_type, identifier_value, normalized_value, scope_key, status, valid_from, normalizer_version)
VALUES (
  '00000000-0000-4000-8000-000000000001',
  '10000000-0000-4000-8000-000000000002',
  'rfid',
  'RFID_EXAMPLE_A',
  'RFID_EXAMPLE_A',
  'global',
  'active',
  now(),
  'test_v1'
);
"

expect_failure "duplicate active old_tag in same scope fails" "
INSERT INTO goat_identifiers (tenant_id, goat_id, identifier_type, identifier_value, normalized_value, scope_key, status, valid_from, normalizer_version)
VALUES (
  '00000000-0000-4000-8000-000000000001',
  '10000000-0000-4000-8000-000000000003',
  'old_tag',
  '1900',
  '1900',
  'park:CBE',
  'active',
  now(),
  'test_v1'
);
"

expect_failure "normal write to merged goat is blocked" "
UPDATE goats
SET breed = 'Synthetic invalid update'
WHERE goat_id = '10000000-0000-4000-8000-000000000005';
"

run_psql <<'SQL'
\echo 'Migration validation complete'
SQL
