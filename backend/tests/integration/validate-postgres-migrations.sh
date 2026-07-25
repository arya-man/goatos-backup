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

validate_ordered_migrations() {
  # The baseline must be 000001 and forward migrations must be contiguous 000001..N so a
  # clean install and an old-baseline upgrade both apply the same ordered set.
  if [[ ! -f "$repo_root/backend/migrations/postgres/000001_goatos_clean_slate_baseline.sql" ]]; then
    echo "missing clean-slate baseline migration 000001_goatos_clean_slate_baseline.sql" >&2
    exit 1
  fi
  local expected=1 version
  while IFS= read -r migration; do
    version="$(basename "$migration" | sed -E 's/^([0-9]+)_.*/\1/')"
    if [[ "$((10#$version))" -ne "$expected" ]]; then
      echo "non-contiguous migration numbering: expected $(printf '%06d' "$expected"), found $(basename "$migration")" >&2
      exit 1
    fi
    expected=$((expected + 1))
  done < <(find "$repo_root/backend/migrations/postgres" -maxdepth 1 -type f -name '*.sql' | sort)
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
validate_ordered_migrations

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
\echo 'R50-015 P0: Verify no 42P10-window exists for ON CONFLICT targets'
DO $$
DECLARE
  msg text;
BEGIN
  -- Assert that outbox_messages has a unique index on (tenant_id, idempotency_key)
  -- covering at least the verification event types (needed for ON CONFLICT).
  IF NOT EXISTS (
    SELECT 1 FROM pg_indexes
    WHERE tablename = 'outbox_messages'
      AND indexname IN ('outbox_messages_verification_idempotency_idx', 'outbox_messages_verification_idempotency_idx_v2')
      AND indexdef LIKE '%UNIQUE%'
  ) THEN
    RAISE EXCEPTION 'R50-015: outbox_messages missing unique index on (tenant_id, idempotency_key) for ON CONFLICT';
  END IF;

  -- Assert that obligation_status_events has a unique index on (tenant_id, idempotency_key)
  -- needed for ON CONFLICT in InsertDeferredObligation.
  IF NOT EXISTS (
    SELECT 1 FROM pg_indexes
    WHERE tablename = 'obligation_status_events'
      AND indexname IN ('obligation_status_events_idempotency_idx', 'obligation_status_events_idempotency_idx_v2')
      AND indexdef LIKE '%UNIQUE%'
  ) THEN
    RAISE EXCEPTION 'R50-015: obligation_status_events missing unique index on (tenant_id, idempotency_key) for ON CONFLICT';
  END IF;

  -- Assert no stale/dangling plain (non-unique) index on obligation_status_events
  -- (old broken index must be gone after migration).
  IF (SELECT count(*) FROM pg_indexes
      WHERE tablename = 'obligation_status_events'
        AND indexname = 'obligation_status_events_idempotency_idx'
        AND indexdef NOT LIKE '%UNIQUE%') > 0 THEN
    RAISE EXCEPTION 'R50-015: obligation_status_events has dangling non-unique idempotency_idx (old broken index not removed)';
  END IF;

  RAISE INFO 'R50-015: 42P10-window assertion passed — unique indexes exist for ON CONFLICT targets';
END $$;

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

# ---------------------------------------------------------------------------
# R50 forward-compatibility assertions (000003_r50_forward_compatibility.sql):
# every delta that was previously only applied via editing 000001 in place must
# also be present after the ordered migration set runs here, and 000003 must be
# a no-op on a clean install of the current baseline. Shared with the
# old-baseline upgrade convergence check below.
# ---------------------------------------------------------------------------
r50_forward_compat_assertions_sql=$(cat <<'R50SQL'
DO $$
DECLARE
  def text;
BEGIN
  SELECT pg_get_constraintdef(oid) INTO def
  FROM pg_constraint
  WHERE conname = 'notification_requests_type_check';
  IF def IS NULL THEN
    RAISE EXCEPTION 'notification_requests_type_check constraint missing';
  END IF;
  IF def NOT LIKE '%verification_approved%' OR def NOT LIKE '%verification_closed%' THEN
    RAISE EXCEPTION 'notification_requests_type_check missing verification lifecycle values: %', def;
  END IF;
END $$;

DO $$
DECLARE
  def text;
BEGIN
  SELECT pg_get_constraintdef(oid) INTO def
  FROM pg_constraint
  WHERE conname = 'obligation_status_events_type_check';
  IF def IS NULL THEN
    RAISE EXCEPTION 'obligation_status_events_type_check constraint missing';
  END IF;
  IF def NOT LIKE '%in_progress%' THEN
    RAISE EXCEPTION 'obligation_status_events_type_check missing in_progress value: %', def;
  END IF;
END $$;

DO $$
BEGIN
  IF to_regclass('public.sop_task_scan_captures') IS NULL THEN
    RAISE EXCEPTION 'sop_task_scan_captures table missing';
  END IF;
  IF to_regclass('public.sop_task_scan_attempts') IS NULL THEN
    RAISE EXCEPTION 'sop_task_scan_attempts table missing';
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'sop_task_scan_captures_pkey') THEN
    RAISE EXCEPTION 'sop_task_scan_captures_pkey missing';
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'sop_task_scan_attempts_pkey') THEN
    RAISE EXCEPTION 'sop_task_scan_attempts_pkey missing';
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_indexes WHERE indexname = 'sop_task_scan_captures_idempotency_unique_idx') THEN
    RAISE EXCEPTION 'sop_task_scan_captures_idempotency_unique_idx missing';
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_indexes WHERE indexname = 'sop_task_scan_captures_task_field_tag_unique_idx') THEN
    RAISE EXCEPTION 'sop_task_scan_captures_task_field_tag_unique_idx missing';
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_indexes WHERE indexname = 'sop_task_scan_attempts_idempotency_unique_idx') THEN
    RAISE EXCEPTION 'sop_task_scan_attempts_idempotency_unique_idx missing';
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'sop_task_scan_captures_goat_id_fkey') THEN
    RAISE EXCEPTION 'sop_task_scan_captures_goat_id_fkey missing';
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'sop_task_scan_captures_task_id_fkey') THEN
    RAISE EXCEPTION 'sop_task_scan_captures_task_id_fkey missing';
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'sop_task_scan_captures_tenant_id_fkey') THEN
    RAISE EXCEPTION 'sop_task_scan_captures_tenant_id_fkey missing';
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'sop_task_scan_attempts_goat_id_fkey') THEN
    RAISE EXCEPTION 'sop_task_scan_attempts_goat_id_fkey missing';
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'sop_task_scan_attempts_task_id_fkey') THEN
    RAISE EXCEPTION 'sop_task_scan_attempts_task_id_fkey missing';
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'sop_task_scan_attempts_tenant_id_fkey') THEN
    RAISE EXCEPTION 'sop_task_scan_attempts_tenant_id_fkey missing';
  END IF;
END $$;

DO $$
DECLARE
  def text;
BEGIN
  IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'verification_items' AND column_name = 'subject_label') THEN
    RAISE EXCEPTION 'verification_items.subject_label column missing';
  END IF;
  IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'verification_items' AND column_name = 'closed_by') THEN
    RAISE EXCEPTION 'verification_items.closed_by column missing';
  END IF;
  IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'verification_items' AND column_name = 'closed_at') THEN
    RAISE EXCEPTION 'verification_items.closed_at column missing';
  END IF;

  SELECT pg_get_constraintdef(oid) INTO def FROM pg_constraint WHERE conname = 'verification_items_subject_label_check';
  IF def IS NULL THEN
    RAISE EXCEPTION 'verification_items_subject_label_check missing';
  END IF;

  SELECT pg_get_constraintdef(oid) INTO def FROM pg_constraint WHERE conname = 'verification_items_closed_pair_check';
  IF def IS NULL OR def NOT LIKE '%closed_by IS NULL%' THEN
    RAISE EXCEPTION 'verification_items_closed_pair_check missing or unexpected: %', def;
  END IF;

  SELECT pg_get_constraintdef(oid) INTO def FROM pg_constraint WHERE conname = 'verification_items_closed_approved_check';
  IF def IS NULL OR def NOT LIKE '%approved%' THEN
    RAISE EXCEPTION 'verification_items_closed_approved_check missing or unexpected: %', def;
  END IF;

  -- R50-015: Accept either the original index name or the _v2 variant (created to avoid DROP-before-CREATE window)
  SELECT pg_get_indexdef(indexrelid) INTO def
  FROM pg_index
  JOIN pg_class ON pg_class.oid = pg_index.indexrelid
  WHERE pg_class.relname IN ('outbox_messages_verification_idempotency_idx', 'outbox_messages_verification_idempotency_idx_v2');
  IF def IS NULL THEN
    RAISE EXCEPTION 'outbox_messages_verification_idempotency_idx (or _v2 variant) missing';
  END IF;
  IF def NOT LIKE '%verification.item.closed%' THEN
    RAISE EXCEPTION 'outbox_messages_verification_idempotency_idx missing verification.item.closed predicate: %', def;
  END IF;

  IF NOT EXISTS (SELECT 1 FROM pg_indexes WHERE indexname = 'verification_items_leadership_queue_idx') THEN
    RAISE EXCEPTION 'verification_items_leadership_queue_idx missing';
  END IF;
END $$;

DO $$
DECLARE
  bad_rows integer;
  total_rows integer;
BEGIN
  SELECT count(*) INTO total_rows
  FROM sop_versions sv
  JOIN sop_definitions sd ON sd.tenant_id = sv.tenant_id AND sd.sop_id = sv.sop_id
  WHERE sd.code IN ('vaccination.drive', 'vaccination.session');

  IF total_rows = 0 THEN
    RAISE EXCEPTION 'no vaccination.drive/vaccination.session sop_versions rows found to assert against';
  END IF;

  SELECT count(*) INTO bad_rows
  FROM sop_versions sv
  JOIN sop_definitions sd ON sd.tenant_id = sv.tenant_id AND sd.sop_id = sv.sop_id
  WHERE sd.code IN ('vaccination.drive', 'vaccination.session')
    AND (
      COALESCE(sv.proof_policy ->> 'proof_mode', '') <> 'shed_level_video'
      OR COALESCE(sv.proof_policy ->> 'subject_scope', '') <> 'shed'
      OR COALESCE(NULLIF(sv.proof_policy ->> 'minimum_count', '')::int, 0) < 1
      OR COALESCE(NULLIF(sv.proof_policy ->> 'maximum_count', '')::int, 0) > 5
    );

  IF bad_rows <> 0 THEN
    RAISE EXCEPTION '% vaccination sop_versions row(s) not on shed-level video proof contract', bad_rows;
  END IF;
END $$;
R50SQL
)

echo "Running R50 forward-compatibility assertions (clean install)"
printf '%s\n' "$r50_forward_compat_assertions_sql" | run_psql

delta_count="$(find "$repo_root/backend/migrations/postgres" -maxdepth 1 -type f -name '*.sql' ! -name '000001_goatos_clean_slate_baseline.sql' | wc -l | tr -d '[:space:]')"
if [[ "$delta_count" == "0" ]]; then
  echo "Single clean-slate baseline detected; skipping historical old-baseline upgrade validation."
  echo "Migration validation passed"
  exit 0
fi

# ---------------------------------------------------------------------------
# Old-baseline upgrade validation: a dev/stg database that applied the ORIGINAL
# 000001 baseline (old overflow_policy enum + old CHECK constraint) must upgrade
# cleanly through 000002 -- proving live that the migration drops the old
# constraint BEFORE rewriting rows (the VAXCAP-001 ordering fix).
# ---------------------------------------------------------------------------
upgrade_container="goatos-migration-upgrade-$$"

cleanup_upgrade() {
  docker rm -f "$upgrade_container" >/dev/null 2>&1 || true
}
trap 'cleanup; cleanup_upgrade' EXIT

run_upgrade_psql() {
  postgres_ci_psql "$upgrade_container" "$db_user" "$db_name" "$@"
}

apply_goose_up_upgrade() {
  local migration="$1"
  awk '
    /^-- \+goose Up/ { in_up = 1; next }
    /^-- \+goose Down/ { in_up = 0 }
    in_up { print }
  ' "$migration" | run_upgrade_psql
}

docker run --rm --name "$upgrade_container" \
  -e POSTGRES_PASSWORD=goatos \
  -e POSTGRES_DB="$db_name" \
  -d "$image" >/dev/null

postgres_ci_wait_ready "$upgrade_container" "$db_user" "$db_name"

echo "Applying 000001 baseline for upgrade validation"
apply_goose_up_upgrade "$repo_root/backend/migrations/postgres/000001_goatos_clean_slate_baseline.sql"

# Recreate the PRE-rename world: the historical baseline constrained overflow_policy to the
# retired enum value and existing rows carry it.
run_upgrade_psql <<'SQL'
ALTER TABLE vaccination_capacity_config
  DROP CONSTRAINT IF EXISTS vaccination_capacity_config_overflow_check;
UPDATE vaccination_capacity_config
SET overflow_policy = 'split_within_safe_window_then_mark_needs_review';
INSERT INTO vaccination_capacity_config (tenant_id, max_per_day, capacity_scope, max_buffer_days, overflow_policy)
VALUES ('00000000-0000-4000-8000-000000000001', 100, 'tenant', 7, 'split_within_safe_window_then_mark_needs_review')
ON CONFLICT (tenant_id) DO NOTHING;
ALTER TABLE vaccination_capacity_config
  ADD CONSTRAINT vaccination_capacity_config_overflow_check
  CHECK (overflow_policy = 'split_within_safe_window_then_mark_needs_review');

-- Recreate the PRE-000003 world for every R50 forward-compatibility delta: the historical
-- (old-baseline) database predates the in-place edits that later baseline commits folded into
-- 000001, so hand-revert each one before applying 000002/000003 below.

-- Delta 1: notification_requests_type_check lacked the verification lifecycle values.
ALTER TABLE notification_requests
  DROP CONSTRAINT IF EXISTS notification_requests_type_check;
ALTER TABLE notification_requests
  ADD CONSTRAINT notification_requests_type_check
  CHECK ((notification_type = ANY (ARRAY['reminder'::text, 'nudge'::text, 'escalation'::text, 'verification_pending'::text, 'rework'::text, 'advance_notice'::text, 'due_today'::text])));

-- Delta 2: the SOP RFID scan-capture tables did not exist yet.
DROP TABLE IF EXISTS sop_task_scan_attempts;
DROP TABLE IF EXISTS sop_task_scan_captures;

-- Delta 4: verification_items lacked the closure columns/checks.
ALTER TABLE verification_items DROP CONSTRAINT IF EXISTS verification_items_closed_approved_check;
ALTER TABLE verification_items DROP CONSTRAINT IF EXISTS verification_items_closed_pair_check;
ALTER TABLE verification_items DROP CONSTRAINT IF EXISTS verification_items_subject_label_check;
ALTER TABLE verification_items DROP COLUMN IF EXISTS closed_at;
ALTER TABLE verification_items DROP COLUMN IF EXISTS closed_by;
ALTER TABLE verification_items DROP COLUMN IF EXISTS subject_label;

-- Delta 5: the outbox verification idempotency index lacked the closure event type.
DROP INDEX IF EXISTS outbox_messages_verification_idempotency_idx;
CREATE UNIQUE INDEX outbox_messages_verification_idempotency_idx ON outbox_messages USING btree (tenant_id, idempotency_key) WHERE (event_type = ANY (ARRAY['verification.item.pending'::text, 'verification.verdict.approved'::text, 'verification.verdict.rework'::text]));

-- Delta 6: the leadership closure queue index did not exist.
DROP INDEX IF EXISTS verification_items_leadership_queue_idx;

-- Delta 7: vaccination SOP versions were still on the pre-goat-scan batch-level proof shape.
WITH vaccination_sops AS (
  SELECT sv.sop_version_id
  FROM sop_versions sv
  JOIN sop_definitions sd ON sd.tenant_id = sv.tenant_id AND sd.sop_id = sv.sop_id
  WHERE sd.code IN ('vaccination.drive', 'vaccination.session')
)
UPDATE sop_versions sv
SET form_dsl = sv.form_dsl - 'goat_row_proof',
    proof_policy = jsonb_build_object(
      'types', jsonb_build_array('video'),
      'required', true,
      'subject_scope', 'batch',
      'expected_subjects', jsonb_build_array('shed', 'vial_lot', 'administration'),
      'minimum_count', 3,
      'maximum_count', 5,
      'verify_capability', 'proof.verify',
      'verify_before_apply', true,
      'retention_policy', 'operational_90d'
    )
FROM vaccination_sops ids
WHERE sv.sop_version_id = ids.sop_version_id;

-- Delta 8: obligation_status_events.event_type lacked the 'in_progress' value.
ALTER TABLE obligation_status_events
  DROP CONSTRAINT IF EXISTS obligation_status_events_type_check;
ALTER TABLE obligation_status_events
  ADD CONSTRAINT obligation_status_events_type_check
  CHECK ((event_type = ANY (ARRAY[
    'scheduled'::text,
    'became_due'::text,
    'dispatched'::text,
    'completed'::text,
    'missed'::text,
    'waived'::text,
    'escalated'::text,
    'escalation_acknowledged'::text,
    'escalation_resolved'::text,
    'canceled'::text,
    'deferred'::text,
    'rescoped'::text
  ])));
SQL

while IFS= read -r migration; do
  [[ "$(basename "$migration")" == "000001_goatos_clean_slate_baseline.sql" ]] && continue
  echo "Applying $(basename "$migration") on old-baseline database"
  apply_goose_up_upgrade "$migration"
done < <(find "$repo_root/backend/migrations/postgres" -maxdepth 1 -type f -name '*.sql' | sort)

run_upgrade_psql <<'SQL'
DO $$
DECLARE
  bad_rows integer;
  new_constraint integer;
BEGIN
  SELECT count(*) INTO bad_rows
  FROM vaccination_capacity_config
  WHERE overflow_policy <> 'split_within_safe_window_last_safe_may_exceed_cap';
  IF bad_rows <> 0 THEN
    RAISE EXCEPTION 'upgrade left % row(s) on the retired overflow_policy value', bad_rows;
  END IF;
  SELECT count(*) INTO new_constraint
  FROM pg_constraint
  WHERE conname = 'vaccination_capacity_config_overflow_check'
    AND pg_get_constraintdef(oid) LIKE '%last_safe_may_exceed_cap%';
  IF new_constraint <> 1 THEN
    RAISE EXCEPTION 'upgrade did not install the new overflow_policy CHECK constraint';
  END IF;
END $$;
SQL

echo "Running R50 forward-compatibility assertions (old-baseline upgrade convergence)"
printf '%s\n' "$r50_forward_compat_assertions_sql" | run_upgrade_psql

echo "Old-baseline upgrade validation passed"

echo "Migration validation passed"
