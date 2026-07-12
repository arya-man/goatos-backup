#!/usr/bin/env bash
set -euo pipefail

repo="${GOATOS_E2E_INTEGRITY_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"

if ! command -v rg >/dev/null 2>&1; then
  printf 'e2e-kernel-integrity: ripgrep (rg) is required; refusing to pass without scanning\n' >&2
  exit 1
fi

# E2E may seed source/input fixtures (tenant, herd animal, location, workforce,
# stock, and authored configuration). It must never seed the derived business
# result it claims to prove. Scan every source-like artifact in the E2E trees,
# including compiled helpers, SQL fixtures, nested scripts, and untracked files.
source_files=()
story_files=()

collect_tree() {
  local dir="$repo/$1"
  [[ -d "$dir" ]] || return 0
  while IFS= read -r -d '' file; do
    source_files+=("$file")
  done < <(find "$dir" -type f \( \
    -name '*.go' -o -name '*.sql' -o -name '*.sh' -o -name '*.bash' -o \
    -name '*.mjs' -o -name '*.js' -o -name '*.ts' -o -name '*.tsx' -o \
    -name '*.py' -o -name '*.json' -o -name '*.yaml' -o -name '*.yml' \
  \) -print0)
}

collect_named_artifacts() {
  local dir="$repo/$1"
  [[ -d "$dir" ]] || return 0
  while IFS= read -r -d '' file; do
    local rel
    rel="${file#"$repo"/}"
    # Legacy proof scripts are manual operator artifacts (for live/procurement checks),
    # not kernel-e2e fixtures, and are covered by dedicated live-run workflows.
    if [[ "$rel" == tools/dev/*-proof.sh ]]; then
      continue
    fi
    source_files+=("$file")
  done < <(find "$dir" -type f \( \
    -name '*.go' -o -name '*.sql' -o -name '*.sh' -o -name '*.bash' -o -name '*.mjs' -o \
    -name '*.js' -o -name '*.ts' -o -name '*.py' -o -name '*.json' \
  \) -print0)
}

collect_story_files() {
  local dir="$repo/$1"
  [[ -d "$dir" ]] || return 0
  while IFS= read -r -d '' file; do
    story_files+=("$file")
  done < <(find "$dir" -type f -name 'story*_test.go' -print0)
}

collect_tree 'backend/tests/e2e'
collect_tree 'backend/tests/e2e-hrms'
collect_story_files 'backend/tests/e2e'
collect_story_files 'backend/tests/e2e-hrms'
collect_named_artifacts 'tools/dev'
collect_named_artifacts 'apps/admin-web/scripts'

if ((${#source_files[@]} == 0)); then
  printf 'e2e-kernel-integrity: no E2E source artifacts found\n' >&2
  exit 1
fi

# Outcome/state tables only. Authored protocol/SOP/config tables and initial
# inventory/goat/location inputs remain allowed fixture inputs.
derived_table_pattern='(obligation_instances|obligation_batches|obligation_status_events(_[[:alnum:]_]+)?|obligation_escalations|obligation_goat_shift_watermarks|vaccination_completions|vaccination_eligibility_rollups|vaccination_generation_runs|calendar_event_projections|calendar_event_identities|calendar_snoozes|process_integrity_projection_rows|process_integrity_projection_state|notification_requests|notification_deliveries|sop_tasks|sop_submissions|sop_submission_items|sop_task_review_fanouts|sop_task_submission_fanouts|proof_artifacts|inventory_stock_movements|outbox_messages|domain_event_processed_events)'
qualified_table="(([[:alnum:]_]+|\"[[:alnum:]_]+\")\.)?\"?${derived_table_pattern}\"?"
mutation="(insert[[:space:]]+into|update|delete[[:space:]]+from|merge[[:space:]]+into|truncate([[:space:]]+table)?|copy)[[:space:]]+${qualified_table}"
failed=0

if rg -n -U -i "$mutation" "${source_files[@]}"; then
  printf '\ne2e-kernel-integrity: E2E artifact directly mutates derived business state.\n' >&2
  failed=1
fi

# SQL can be hidden behind a compiled helper or generated query method. These
# repository/sqlc write seams bypass the application command/event/worker path.
direct_write_method='\.(InsertObligation|InsertObligationInstance|CreateBatch|CreateBatchWithObligations|CreateObligationBatch|InsertObligationStatusEvent)\('
if rg -n "$direct_write_method" "${source_files[@]}"; then
  printf '\ne2e-kernel-integrity: E2E artifact bypasses generation/batching through a direct repository write.\n' >&2
  failed=1
fi

# Vaccination completion and reschedule outcomes must enter through the SOP/API/event boundaries
# claimed by the story. Calling these application/repository shortcuts directly only proves the
# downstream mutation, not proof-backed submission, independent review, or the HTTP command path.
direct_vaccination_outcome='\.(RecordCompletion|AcceptExisting|RejectExisting|AcceptObligation|RescheduleObligationByID)\('
if rg -n "$direct_vaccination_outcome" "${source_files[@]}"; then
  printf '\ne2e-kernel-integrity: E2E artifact bypasses the vaccination SOP/API outcome boundary.\n' >&2
  failed=1
fi

# A nil TaskCreator is valid for sweeper-only stories such as missed/defer classification. It is
# not valid once the same story binds an SOP version or invokes proof/submission/review seams: that
# would create a batch with no executable task and then claim a surface the story never entered.
nil_task_creator='NewSweeperService\([^)]*,[[:space:]]*nil[[:space:]]*,'
execution_surface_claim='SOPVersionID[[:space:]]*[:=]|\.(SubmitTask|VerifyTask|ReworkTask|WithProofValidator|WithSubmissionHook|WithTaskReviewFanout)\('
if ((${#story_files[@]} > 0)); then
  for file in "${story_files[@]}"; do
    if rg -q -U "$nil_task_creator" "$file" && rg -q "$execution_surface_claim" "$file"; then
      rg -n -U "$nil_task_creator" "$file" || true
      printf '\ne2e-kernel-integrity: story uses a nil TaskCreator while claiming an SOP proof/submission/review surface: %s\n' "${file#"$repo"/}" >&2
      failed=1
    fi
  done
fi

goats_table='(([[:alnum:]_]+|\"[[:alnum:]_]+\")\.)?\"?goats\"?'
if rg -n -U -i "(update[[:space:]]+${goats_table}[[:space:]]+set|delete[[:space:]]+from[[:space:]]+${goats_table})" "${source_files[@]}"; then
  printf '\ne2e-kernel-integrity: E2E artifact bypasses the identity mutation/outbox path.\n' >&2
  failed=1
fi

if ((failed != 0)); then
  cat >&2 <<'EOF'

Allowed: seed external/input facts such as a goat, shed, workforce member,
inventory lot, or draft protocol configuration.

Forbidden: seed or directly update the obligation, completion, batch, SOP,
proof, notification, Calendar, process-integrity, or execution-ledger result
being asserted. Drive those outcomes through the production service/API/event/
worker path instead.

During a story, goat health, reproductive, move, and exit transitions must use
the production identity command and its emitted outbox event. Direct goat rows
are allowed only for initial fixture creation.
EOF
  exit 1
fi

printf 'e2e-kernel-integrity: passed (%d source artifacts scanned)\n' "${#source_files[@]}"
