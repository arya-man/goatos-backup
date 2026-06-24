#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
source "$repo_root/tools/postgres-ci.sh"

container_name="goatos-sqlc-plan-validation-$$"
image="${GOATOS_SQLC_POSTGRES_IMAGE:-${GOATOS_POSTGRES_IMAGE:-postgres:16.9-alpine}}"
db_name="goatos"
db_user="postgres"

cleanup() {
  docker rm -f "$container_name" >/dev/null 2>&1 || true
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
  ' "$migration" | run_psql
}

explain_must_use_index() {
  local label="$1"
  local forbidden="$2"
  local sql="$3"
  local extra_forbidden="${4:-}"
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
  if [ -n "$extra_forbidden" ] && grep -E "$extra_forbidden" <<<"$plan" >/dev/null; then
    echo "$plan"
    echo "Unexpected plan node in $label matching: $extra_forbidden" >&2
    exit 1
  fi
  echo "Indexed plan observed: $label"
}

validate_identity_lookup_plans() {
  explain_must_use_index "GoatByID" 'Seq Scan on goats' "EXPLAIN (COSTS OFF)
SELECT goat_id
FROM goats
WHERE tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
  AND goat_id = '10000000-0000-4000-8000-000000000001'::uuid;"

  explain_must_use_index "GoatSearchDisplay" 'Seq Scan on goats' "EXPLAIN (COSTS OFF)
SELECT goat_id
FROM goats
WHERE tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
  AND identity_state <> 'merged'
ORDER BY display_id ASC
LIMIT 50;" '(Sort|Incremental Sort)'

  explain_must_use_index "IdentifierResolution" 'Seq Scan on goat_identifiers' "EXPLAIN (COSTS OFF)
SELECT goat_id
FROM goat_identifiers
WHERE tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
  AND identifier_type = 'old_tag'
  AND normalized_value = '1900'
  AND status = 'active'
LIMIT 10;"

  explain_must_use_index "GoatTimeline" 'Seq Scan on goat_identity_events' "EXPLAIN (COSTS OFF)
SELECT identity_event_id, occurred_at
FROM goat_identity_events
WHERE tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
  AND goat_id = '10000000-0000-4000-8000-000000000001'::uuid
ORDER BY occurred_at DESC, identity_event_id DESC
LIMIT 50;"
}

validate_outbox_claim_plan() {
  explain_must_use_index "OutboxClaimPending" 'Seq Scan on outbox_messages' "EXPLAIN (COSTS OFF)
SELECT outbox_id
FROM outbox_messages
WHERE status = 'pending'
  AND (next_attempt_at IS NULL OR next_attempt_at <= '2026-06-09T12:00:00Z'::timestamptz)
ORDER BY created_at, outbox_id
LIMIT 10
FOR UPDATE SKIP LOCKED;"
}

validate_auth_grant_lookup_plan() {
  explain_must_use_index "ActiveTenantRoles" 'Seq Scan on user_scope_grants' "EXPLAIN (COSTS OFF)
SELECT role
FROM user_scope_grants
WHERE user_id = '90000000-0000-4000-8000-000000000001'::uuid
  AND tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
  AND scope_type = 'tenant'
  AND scope_id = '00000000-0000-4000-8000-000000000001'::uuid
  AND status = 'active'
  AND valid_from <= now()
  AND (valid_to IS NULL OR valid_to > now())
ORDER BY role;"
}

validate_obligation_due_window_plan() {
  explain_must_use_index "ObligationDueWindow" 'Seq Scan on obligation_instances' "EXPLAIN (COSTS OFF)
SELECT obligation_id, due_at
FROM obligation_instances
WHERE tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
  AND status = 'scheduled'
  AND due_at <= TIMESTAMPTZ '2026-12-31 00:00:00+00'
ORDER BY due_at ASC, obligation_id ASC
LIMIT 100;"
}

validate_obligation_scope_count_plan() {
  explain_must_use_index "ObligationCountByScope" 'Seq Scan on obligation_instances' "EXPLAIN (COSTS OFF)
SELECT count(*)
FROM obligation_instances
WHERE tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
  AND scope_type = 'park'
  AND scope_id = '00000000-0000-4000-8000-000000003001'::uuid
  AND status = 'scheduled';"
}

validate_obligation_open_by_goat_plan() {
  explain_must_use_index "ObligationOpenByGoat" 'Seq Scan on obligation_instances' "EXPLAIN (COSTS OFF)
SELECT obligation_id, due_at, status
FROM obligation_instances
WHERE tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
  AND target_type = 'goat'
  AND target_id = '00000000-0000-4000-8000-0000000000aa'::uuid
  AND status IN ('scheduled', 'due', 'in_progress')
ORDER BY due_at ASC, obligation_id ASC
LIMIT 200;"
}

validate_inventory_fefo_plan() {
  explain_must_use_index "InventoryFEFOPick" 'Seq Scan on inventory_stock' "EXPLAIN (COSTS OFF)
SELECT stock_id, expiry_date
FROM inventory_stock
WHERE tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
  AND location_id = '00000000-0000-4000-8000-000000003001'::uuid
  AND item_id = '00000000-0000-4000-8000-0000000000bb'::uuid
  AND quantity_in_stock > 0
  AND quantity_in_stock > quantity_reserved
ORDER BY expiry_date ASC NULLS LAST, stock_id ASC
LIMIT 1;"
}

validate_inventory_movements_ledger_plan() {
  explain_must_use_index "InventoryMovementsByLot" 'Seq Scan on inventory_stock_movements' "EXPLAIN (COSTS OFF)
SELECT movement_id, occurred_at
FROM inventory_stock_movements
WHERE tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
  AND lot_id = '00000000-0000-4000-8000-0000000000cc'::uuid
ORDER BY occurred_at;"
}

validate_vaccination_generation_scan_plan() {
  explain_must_use_index "VaccinationGenerationKeyset" 'Seq Scan on goats' "EXPLAIN (COSTS OFF)
SELECT goat_id, dob
FROM goats
WHERE tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
  AND lifecycle_status IN ('alive', 'sick', 'under_treatment', 'quarantine', 'icu')
  AND management_stage = 'K1'
  AND goat_id > '00000000-0000-0000-0000-000000000000'::uuid
ORDER BY goat_id
LIMIT 500;"
}

validate_vaccination_review_queue_plan() {
  explain_must_use_index "VaccinationReviewQueue" 'Seq Scan on vaccination_completions' "EXPLAIN (COSTS OFF)
SELECT completion_id
FROM vaccination_completions
WHERE tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
  AND status = 'recorded'
ORDER BY administered_at ASC, completion_id ASC
LIMIT 100;"
}

validate_vaccination_fanout_plan() {
  explain_must_use_index "VaccinationFanoutByTask" 'Seq Scan on vaccination_completions' "EXPLAIN (COSTS OFF)
SELECT c.completion_id
FROM vaccination_completions c
JOIN sop_submission_items i ON i.tenant_id = c.tenant_id AND i.item_id = c.sop_submission_item_id
JOIN sop_submissions s ON s.tenant_id = i.tenant_id AND s.submission_id = i.submission_id
WHERE c.tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
  AND s.task_id = '00000000-0000-4000-8000-0000000000dd'::uuid
  AND c.status = 'recorded'
  AND c.sop_submission_item_id IS NOT NULL
ORDER BY c.completion_id;"
}

validate_feed_review_queue_plan() {
  explain_must_use_index "FeedReviewQueue" 'Seq Scan on feed_direction_completions' "EXPLAIN (COSTS OFF)
SELECT completion_id
FROM feed_direction_completions
WHERE tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
  AND status = 'recorded'
ORDER BY fed_at ASC, completion_id ASC
LIMIT 100;"
}

validate_feed_shed_history_plan() {
  explain_must_use_index "FeedShedHistory" 'Seq Scan on feed_direction_completions' "EXPLAIN (COSTS OFF)
SELECT completion_id, fed_at, status
FROM feed_direction_completions
WHERE tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
  AND shed_id = '55000000-0000-4000-8000-0000000000f1'::uuid
ORDER BY fed_at DESC, completion_id DESC
LIMIT 100;"
}

docker run --rm --name "$container_name" \
  -e POSTGRES_PASSWORD=goatos \
  -e POSTGRES_DB="$db_name" \
  -d "$image" >/dev/null

postgres_ci_wait_ready "$container_name" "$db_user" "$db_name"

while IFS= read -r migration; do
  apply_goose_up "$migration"
done < <(find "$repo_root/backend/migrations/postgres" -maxdepth 1 -type f -name '*.sql' | sort)

validate_identity_lookup_plans
validate_outbox_claim_plan
validate_auth_grant_lookup_plan
validate_obligation_due_window_plan
validate_obligation_scope_count_plan
validate_obligation_open_by_goat_plan
validate_inventory_fefo_plan
validate_inventory_movements_ledger_plan
validate_vaccination_generation_scan_plan
validate_vaccination_review_queue_plan
validate_vaccination_fanout_plan
validate_feed_review_queue_plan
validate_feed_shed_history_plan

echo "Validated current hot-path query plans"
