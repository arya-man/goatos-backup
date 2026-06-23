#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
source "$repo_root/tools/postgres-ci.sh"

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
  local processing_state="${PLAN_PROCESSING_STATE:-NULL::text}"
  local reason_code="${PLAN_REASON_CODE:-NULL::text}"
  local cursor_row_number="${PLAN_CURSOR_ROW_NUMBER:-NULL::int}"
  local cursor_legacy_row_id="${PLAN_CURSOR_LEGACY_ROW_ID:-NULL::uuid}"
  local created_by="${PLAN_CREATED_BY:-NULL::uuid}"
  local state="${PLAN_STATE:-NULL::text}"
  local cursor_event_id="${PLAN_CURSOR_EVENT_ID:-NULL::uuid}"
  local cursor_correction_request_id="${PLAN_CURSOR_CORRECTION_REQUEST_ID:-NULL::uuid}"

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
    -e "s/sqlc.narg('last_processed_recorded_at')::timestamptz/'2026-06-09T12:00:00Z'::timestamptz/g" \
    -e "s/sqlc.narg('last_processed_event_id')::uuid/'60000000-0000-4000-8000-000000000001'::uuid/g" \
    -e "s/sqlc.narg('cursor_created_at')::timestamptz/NULL::timestamptz/g" \
    -e "s/sqlc.narg('cursor_candidate_id')::uuid/NULL::uuid/g" \
    -e "s/sqlc.narg('created_by')::uuid/${created_by}/g" \
    -e "s/sqlc.narg('state')::text/${state}/g" \
    -e "s/sqlc.narg('cursor_occurred_at')::timestamptz/NULL::timestamptz/g" \
    -e "s/sqlc.narg('cursor_event_id')::uuid/${cursor_event_id}/g" \
    -e "s/sqlc.narg('cursor_correction_request_id')::uuid/${cursor_correction_request_id}/g" \
    -e "s/sqlc.narg('processing_state')::text/${processing_state}/g" \
    -e "s/sqlc.narg('reason_code')::text/${reason_code}/g" \
    -e "s/sqlc.narg('cursor_row_number')::int/${cursor_row_number}/g" \
    -e "s/sqlc.narg('cursor_legacy_row_id')::uuid/${cursor_legacy_row_id}/g" \
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
    GetImportRunByID)
      printf '%s\n' 'Seq Scan on (legacy_import_runs|legacy_import_rows)'
      ;;
    ListImportRuns)
      printf '%s\n' 'Seq Scan on legacy_import_runs'
      ;;
    ListImportRunRows)
      printf '%s\n' 'Seq Scan on legacy_import_rows'
      ;;
    ListGoatTimeline)
      printf '%s\n' 'Seq Scan on goat_identity_events'
      ;;
    ListCorrectionRequests)
      printf '%s\n' 'Seq Scan on identity_correction_requests'
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
    ListPendingLegacyImportRowsForApply|ListRFIDApplyCandidateRowsForBlankSuffixPolicy)
      printf '%s\n' 'Seq Scan on legacy_import_rows'
      ;;
    ListIdentityCounts)
      printf '%s\n' 'Seq Scan on goat_identity_counters'
      ;;
    GetIdentityCounterProjectionState)
      printf '%s\n' 'Seq Scan on goat_identity_counter_projection_state'
      ;;
    ListIdentityEventsAfterCheckpoint)
      printf '%s\n' 'Seq Scan on goat_identity_events'
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

  case "$query_name" in
    ListPendingLegacyImportRowsForApply|ListRFIDApplyCandidateRowsForBlankSuffixPolicy|ListImportRuns|ListImportRunRows|ListGoatTimeline|ListCorrectionRequests)
      explain_must_use_index "$query_name" "$forbidden" "EXPLAIN (COSTS OFF)
$sql" '^[[:space:]]*(->[[:space:]]*)?(Sort|Incremental Sort)[[:space:]]*$'
      ;;
    *)
      explain_must_use_index "$query_name" "$forbidden" "EXPLAIN (COSTS OFF)
$sql"
      ;;
  esac
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
ORDER BY role"
}

validate_import_run_reason_gin_probe_plan() {
  explain_must_use_index "ListImportRunRowsReasonGINProbe" 'Seq Scan on legacy_import_rows' "EXPLAIN (COSTS OFF)
SELECT legacy_row_id
FROM legacy_import_rows
WHERE tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
  AND import_run_id = '30000000-0000-4000-8000-000000000001'::uuid
  AND (normalized_payload -> 'processing_reasons') ? 'blank_old_tag_suffix'
LIMIT 10"
}

validate_import_run_generated_reason_filter_plan() {
  local sql
  local old_reason_set=0
  local old_reason=""

  if [ "${PLAN_REASON_CODE+x}" ]; then
    old_reason_set=1
    old_reason="$PLAN_REASON_CODE"
  fi
  PLAN_REASON_CODE="'blank_old_tag_suffix'::text"
  sql="$(extract_query "$repo_root/backend/internal/identity/adapters/postgres/sqlc/query.sql" "ListImportRunRows" | bind_query_params)"
  if [ "$old_reason_set" -eq 1 ]; then
    PLAN_REASON_CODE="$old_reason"
  else
    unset PLAN_REASON_CODE
  fi

  # This intentionally guards the Phase 1 local ordered-keyset shape. When the
  # staging/1M sparse-reason strategy lands, revise this no-Sort assertion if
  # the chosen reason-keyset or GIN-bitmap plan legitimately needs a sort.
  explain_must_use_index "ListImportRunRowsGeneratedReasonFilter" 'Seq Scan on legacy_import_rows' "EXPLAIN (COSTS OFF)
$sql" '(Sort|Incremental Sort)'
}

validate_import_run_state_filter_plan() {
  explain_must_use_index "ListImportRunRowsStateFilter" 'Seq Scan on legacy_import_rows' "EXPLAIN (COSTS OFF)
SELECT legacy_row_id
FROM legacy_import_rows
WHERE tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
  AND import_run_id = '30000000-0000-4000-8000-000000000001'::uuid
  AND processing_state = 'needs_review'
ORDER BY row_number, legacy_row_id
LIMIT 10" '(Sort|Incremental Sort)'
}

validate_correction_request_actor_plan() {
  local sql
  PLAN_CREATED_BY="'90000000-0000-4000-8000-000000000001'::uuid"
  sql="$(extract_query "$repo_root/backend/internal/identity/adapters/postgres/sqlc/query.sql" "ListCorrectionRequests" | bind_query_params)"
  unset PLAN_CREATED_BY
  explain_must_use_index "ListCorrectionRequestsActorFilter" 'Seq Scan on identity_correction_requests' "EXPLAIN (COSTS OFF)
$sql" '(Sort|Incremental Sort)'
}

validate_correction_request_state_plan() {
  local sql
  PLAN_STATE="'open'::text"
  sql="$(extract_query "$repo_root/backend/internal/identity/adapters/postgres/sqlc/query.sql" "ListCorrectionRequests" | bind_query_params)"
  unset PLAN_STATE
  explain_must_use_index "ListCorrectionRequestsStateFilter" 'Seq Scan on identity_correction_requests' "EXPLAIN (COSTS OFF)
$sql" '(Sort|Incremental Sort)'
}

validate_herd_search_filter_plans() {
  explain_must_use_index "SearchGoatsBreedSexFilter" 'Seq Scan on goats' "EXPLAIN (COSTS OFF)
SELECT goat_id
FROM goats g
WHERE g.tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
  AND g.identity_state <> 'merged'
  AND g.breed = 'Sojat'
  AND g.sex = 'male'
ORDER BY g.display_id ASC
LIMIT 10" '(Sort|Incremental Sort)'

  explain_must_use_index "SearchGoatsSexFilter" 'Seq Scan on goats' "EXPLAIN (COSTS OFF)
SELECT goat_id
FROM goats g
WHERE g.tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
  AND g.identity_state <> 'merged'
  AND g.sex = 'female'
ORDER BY g.display_id ASC
LIMIT 10" '(Sort|Incremental Sort)'

  explain_must_use_index "SearchGoatsLifecycleFilter" 'Seq Scan on goats' "EXPLAIN (COSTS OFF)
SELECT goat_id
FROM goats g
WHERE g.tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
  AND g.identity_state <> 'merged'
  AND g.lifecycle_status = 'alive'
ORDER BY g.display_id ASC
LIMIT 10" '(Sort|Incremental Sort)'
}

docker run --rm --name "$container_name" \
  -e POSTGRES_PASSWORD=goatos \
  -e POSTGRES_DB="$db_name" \
  -d "$image" >/dev/null

postgres_ci_wait_ready "$container_name" "$db_user" "$db_name"

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

# --- Phase 0 protocol/obligation/inventory hot paths (million-goat scale) ---

validate_obligation_due_window_plan() {
  explain_must_use_index "ObligationDueWindow" 'Seq Scan on obligation_instances' "EXPLAIN (COSTS OFF)
SELECT obligation_id, due_at
FROM obligation_instances
WHERE tenant_id = '00000000-0000-4000-8000-000000000001'
  AND status = 'scheduled'
  AND due_at <= TIMESTAMPTZ '2026-12-31 00:00:00+00'
ORDER BY due_at ASC, obligation_id ASC
LIMIT 100;"
}

validate_obligation_scope_count_plan() {
  explain_must_use_index "ObligationCountByScope" 'Seq Scan on obligation_instances' "EXPLAIN (COSTS OFF)
SELECT count(*)
FROM obligation_instances
WHERE tenant_id = '00000000-0000-4000-8000-000000000001'
  AND scope_type = 'park'
  AND scope_id = '00000000-0000-4000-8000-000000003001'
  AND status = 'scheduled';"
}

validate_inventory_fefo_plan() {
  explain_must_use_index "InventoryFEFOPick" 'Seq Scan on inventory_stock' "EXPLAIN (COSTS OFF)
SELECT stock_id, expiry_date
FROM inventory_stock
WHERE tenant_id = '00000000-0000-4000-8000-000000000001'
  AND location_id = '00000000-0000-4000-8000-000000003001'
  AND item_id = '00000000-0000-4000-8000-0000000000bb'
  AND quantity_in_stock > 0
  AND quantity_in_stock > quantity_reserved
ORDER BY expiry_date ASC NULLS LAST, stock_id ASC
LIMIT 1;"
}

validate_inventory_movements_ledger_plan() {
  explain_must_use_index "InventoryMovementsByLot" 'Seq Scan on inventory_stock_movements' "EXPLAIN (COSTS OFF)
SELECT movement_id, occurred_at
FROM inventory_stock_movements
WHERE tenant_id = '00000000-0000-4000-8000-000000000001'
  AND lot_id = '00000000-0000-4000-8000-0000000000cc'
ORDER BY occurred_at;"
}

validate_obligation_target_lookup_plan() {
  explain_must_use_index "ObligationByTarget" 'Seq Scan on obligation_instances' "EXPLAIN (COSTS OFF)
SELECT obligation_id, status
FROM obligation_instances
WHERE tenant_id = '00000000-0000-4000-8000-000000000001'
  AND target_type = 'goat'
  AND target_id = '00000000-0000-4000-8000-0000000000aa'
  AND status = 'scheduled';"
}

validate_obligation_open_by_goat_plan() {
  # Goat Passport next-due: a goat's open obligations must come via obligation_instances_target_idx.
  explain_must_use_index "ObligationOpenByGoat" 'Seq Scan on obligation_instances' "EXPLAIN (COSTS OFF)
SELECT obligation_id, due_at, status
FROM obligation_instances
WHERE tenant_id = '00000000-0000-4000-8000-000000000001'
  AND target_type = 'goat'
  AND target_id = '00000000-0000-4000-8000-0000000000aa'
  AND status IN ('scheduled', 'due', 'in_progress')
ORDER BY due_at ASC, obligation_id ASC
LIMIT 200;"
}

validate_vaccination_eligible_plan() {
  explain_must_use_index "VaccinationEligibleGoats" 'Seq Scan on goats' "EXPLAIN (COSTS OFF)
SELECT count(*)
FROM goats
WHERE tenant_id = '00000000-0000-4000-8000-000000000001'
  AND lifecycle_status = 'alive'
  AND management_stage = 'K1';"
}

validate_vaccination_generation_scan_plan() {
  explain_must_use_index "VaccinationGenerationKeyset" 'Seq Scan on goats' "EXPLAIN (COSTS OFF)
SELECT goat_id, dob
FROM goats
WHERE tenant_id = '00000000-0000-4000-8000-000000000001'
  AND lifecycle_status IN ('alive', 'sick', 'under_treatment', 'quarantine', 'icu')
  AND management_stage = 'K1'
  AND goat_id > '00000000-0000-0000-0000-000000000000'::uuid
ORDER BY goat_id
LIMIT 500;"
}

validate_vaccination_fanout_plan() {
  # SOP verify fan-out resolves a task's recorded completions; the large vaccination_completions
  # table must be reached via vaccination_completions_submission_item_idx, not a scan.
  explain_must_use_index "VaccinationFanoutByTask" 'Seq Scan on vaccination_completions' "EXPLAIN (COSTS OFF)
SELECT c.completion_id
FROM vaccination_completions c
JOIN sop_submission_items i ON i.tenant_id = c.tenant_id AND i.item_id = c.sop_submission_item_id
JOIN sop_submissions s ON s.tenant_id = i.tenant_id AND s.submission_id = i.submission_id
WHERE c.tenant_id = '00000000-0000-4000-8000-000000000001'
  AND s.task_id = '00000000-0000-4000-8000-0000000000dd'
  AND c.status = 'recorded'
  AND c.sop_submission_item_id IS NOT NULL
ORDER BY c.completion_id;"
}

validate_outbox_claim_plan
validate_auth_grant_lookup_plan
validate_import_run_reason_gin_probe_plan
validate_import_run_generated_reason_filter_plan
validate_import_run_state_filter_plan
validate_correction_request_actor_plan
validate_correction_request_state_plan
validate_herd_search_filter_plans
validate_obligation_due_window_plan
validate_obligation_scope_count_plan
validate_inventory_fefo_plan
validate_inventory_movements_ledger_plan
validate_obligation_target_lookup_plan
validate_obligation_open_by_goat_plan
validate_vaccination_eligible_plan
validate_vaccination_generation_scan_plan
validate_vaccination_fanout_plan

echo "Validated $checked_count generated sqlc query plans and 19 hand-written query plans"
