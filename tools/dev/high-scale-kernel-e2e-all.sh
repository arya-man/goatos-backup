#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
run_id="${GOATOS_KERNEL_E2E_RUN_ID:-KERNEL-E2E-$(date -u +%Y%m%d-%H%M%S)}"
report_dir="${GOATOS_KERNEL_E2E_REPORT_DIR:-$repo_root/.codex-goatos-render/high-scale-kernel-e2e/$run_id}"
certification_mode="${GOATOS_KERNEL_E2E_CERTIFICATION:-0}"
if [ "$certification_mode" = "1" ]; then
  scope="${GOATOS_KERNEL_E2E_SCOPE:-strict_certification}"
  allow_not_implemented="${GOATOS_KERNEL_E2E_ALLOW_NOT_IMPLEMENTED:-0}"
else
  scope="${GOATOS_KERNEL_E2E_SCOPE:-vaccination_slice_local}"
  allow_not_implemented="${GOATOS_KERNEL_E2E_ALLOW_NOT_IMPLEMENTED:-1}"
fi
run_static="${GOATOS_KERNEL_E2E_RUN_STATIC:-1}"
run_tests="${GOATOS_KERNEL_E2E_RUN_TESTS:-1}"
run_live="${GOATOS_KERNEL_E2E_RUN_LIVE:-1}"
run_browser="${GOATOS_KERNEL_E2E_RUN_BROWSER:-0}"
allow_local_checksum_drift="${GOATOS_KERNEL_E2E_ALLOW_LOCAL_CHECKSUM_DRIFT:-0}"
local_database_url="${DATABASE_URL:-postgres://postgres:goatos@127.0.0.1:55432/goatos?sslmode=disable}"
tenant_id="${GOATOS_TENANT_ID:-00000000-0000-4000-8000-000000000001}"
local_user_id="${GOATOS_LOCAL_USER_ID:-90000000-0000-4000-8000-000000000101}"
api_base_url="${GOATOS_API_BASE_URL:-http://127.0.0.1:8080}"
admin_web_base_url="${GOATOS_ADMIN_WEB_BASE_URL:-http://127.0.0.1:3300}"
auth_secret="${GOATOS_AUTH_HS256_SECRET:-goatos-local-dev-secret-32-bytes-min}"

require_bool() {
  local name="$1"
  local value="$2"
  case "$value" in
    0 | 1) ;;
    *)
      printf '%s must be 0 or 1, got %q\n' "$name" "$value" >&2
      exit 2
      ;;
  esac
}

require_bool GOATOS_KERNEL_E2E_CERTIFICATION "$certification_mode"
require_bool GOATOS_KERNEL_E2E_ALLOW_NOT_IMPLEMENTED "$allow_not_implemented"
require_bool GOATOS_KERNEL_E2E_ALLOW_LOCAL_CHECKSUM_DRIFT "$allow_local_checksum_drift"

if [ "$certification_mode" = "1" ] && [ "$allow_not_implemented" != "0" ]; then
  printf 'GOATOS_KERNEL_E2E_CERTIFICATION=1 requires GOATOS_KERNEL_E2E_ALLOW_NOT_IMPLEMENTED=0\n' >&2
  exit 2
fi

if [ "$certification_mode" = "1" ] && [ "$allow_local_checksum_drift" = "1" ]; then
  printf 'GOATOS_KERNEL_E2E_CERTIFICATION=1 cannot use GOATOS_KERNEL_E2E_ALLOW_LOCAL_CHECKSUM_DRIFT=1\n' >&2
  exit 2
fi

migrate_flags=()
if [ "$allow_local_checksum_drift" = "1" ]; then
  migrate_flags+=("-allow-local-checksum-drift")
fi
migrate_flags_text=""
if [ "${#migrate_flags[@]}" -gt 0 ]; then
  migrate_flags_text="${migrate_flags[*]}"
fi

mkdir -p "$report_dir"

commands_tsv="$report_dir/commands.tsv"
checklist_tsv="$report_dir/e2e-checklist.tsv"
report_md="$report_dir/report.md"
: >"$commands_tsv"
: >"$checklist_tsv"

overall_status=0

timestamp() {
  date -u +"%Y-%m-%dT%H:%M:%SZ"
}

slug() {
  printf '%s' "$1" | tr '[:upper:]' '[:lower:]' | sed -E 's/[^a-z0-9]+/-/g; s/^-//; s/-$//'
}

log() {
  printf '%s %s\n' "$(timestamp)" "$*"
}

run_step() {
  local label="$1"
  local command="$2"
  local name
  local log_file
  local started_at ended_at rc
  name="$(slug "$label")"
  log_file="$report_dir/$name.log"
  started_at="$(timestamp)"
  log "START $label"
  set +e
  (
    cd "$repo_root"
    bash -lc "$command"
  ) >"$log_file" 2>&1
  rc=$?
  set -e
  ended_at="$(timestamp)"
  if [ "$rc" -eq 0 ]; then
    log "PASS  $label"
    printf '%s\tpassed\t%s\t%s\t%s\t%s\n' "$label" "$started_at" "$ended_at" "$rc" "$log_file" >>"$commands_tsv"
  else
    log "FAIL  $label rc=$rc log=$log_file"
    printf '%s\tfailed\t%s\t%s\t%s\t%s\n' "$label" "$started_at" "$ended_at" "$rc" "$log_file" >>"$commands_tsv"
    overall_status=1
  fi
}

command_status() {
  local label="$1"
  awk -F '\t' -v label="$label" '$1 == label { status = $2 } END { print status }' "$commands_tsv"
}

passed_if() {
  local label="$1"
  local evidence="$2"
  shift 2
  local dep
  for dep in "$@"; do
    if [ "$(command_status "$dep")" != "passed" ]; then
      printf '%s\tfailed\t%s (blocked by failed command: %s)\n' "$label" "$evidence" "$dep" >>"$checklist_tsv"
      overall_status=1
      return
    fi
  done
  printf '%s\tpassed\t%s\n' "$label" "$evidence" >>"$checklist_tsv"
}

not_implemented() {
  local label="$1"
  local evidence="$2"
  printf '%s\tnot implemented yet\t%s\n' "$label" "$evidence" >>"$checklist_tsv"
  if [ "$allow_not_implemented" != "1" ]; then
    overall_status=1
  fi
}

not_run() {
  local label="$1"
  local evidence="$2"
  printf '%s\tfailed\t%s\n' "$label" "$evidence" >>"$checklist_tsv"
  overall_status=1
}

kernel_test_packages="./internal/protocol/... ./internal/vaccination/... ./internal/obligation/... ./internal/inventory/... ./internal/outbox/... ./internal/notification/... ./internal/calendar/... ./internal/processintegrity/... ./internal/sop/... ./internal/procurement/... ./cmd/outbox-dlq ./cmd/outbox-relay ./cmd/domain-event-consumer ./cmd/notification-dispatcher ./cmd/obligation-sweeper ./cmd/generate-vaccination-obligations ./cmd/calendar-vaccination-projector ./cmd/calendar-reminder-sweeper ./cmd/calendar-escalation-sweeper ./cmd/idempotency-key-sweeper"

log "High-scale kernel E2E-all run_id=$run_id scope=$scope certification=$certification_mode report_dir=$report_dir"

if [ "$run_static" = "1" ]; then
  run_step "static guards: hot-index migrations" "make validate-hot-index-migrations"
  run_step "static guards: postgres migrations" "make validate-migrations"
  run_step "static guards: sqlc hot plans" "make validate-sqlc-plans"
fi

if [ "$run_tests" = "1" ]; then
  run_step "backend kernel targeted tests" "cd backend && go test $kernel_test_packages"
  if [ "${GOATOS_KERNEL_E2E_FULL_BACKEND_TESTS:-0}" = "1" ]; then
    run_step "backend full go test" "cd backend && go test ./..."
  fi
fi

if [ "$run_live" = "1" ]; then
  run_step "local dev DB migration head" "cd backend && GOATOS_ENV=local DATABASE_URL='$local_database_url' go run ./cmd/migrate $migrate_flags_text"
  run_step "live vaccination chain proof" "DATABASE_URL='$local_database_url' bash tools/dev/vaccination-chain-proof.sh"
  run_step "live procurement vaccination matrix" "GOATOS_E2E_RUN_ID='$run_id' DATABASE_URL='$local_database_url' bash tools/dev/procurement-vaccination-e2e-matrix.sh"
  if [ "$run_browser" = "1" ]; then
    run_step "live admin-web visual smoke" "export GOATOS_ENV=local GOATOS_AUTH_MODE=bearer GOATOS_AUTH_ISSUER=goatos-local GOATOS_AUTH_AUDIENCE=goatos-api GOATOS_AUTH_HS256_SECRET='$auth_secret' GOATOS_AUTH_MAX_TOKEN_TTL=24h GOATOS_TENANT_ID='$tenant_id' GOATOS_LOCAL_USER_ID='$local_user_id' GOATOS_API_BASE_URL='$api_base_url' GOATOS_ADMIN_WEB_BASE_URL='$admin_web_base_url' DATABASE_URL='$local_database_url'; export GOATOS_BEARER_TOKEN=\"\$(cd backend && go run ./cmd/mint-dev-token -tenant-id '$tenant_id' -user-id '$local_user_id' -ttl 2h 2>/dev/null)\"; npm --prefix apps/admin-web run smoke:visual:live"
  fi
  run_step "live vaccination rework proof" "DATABASE_URL='$local_database_url' bash tools/dev/vaccination-rework-proof.sh"
  run_step "live vaccination trusted-history proof" "DATABASE_URL='$local_database_url' bash tools/dev/vaccination-trusted-history-proof.sh"
fi

if [ "$run_static" != "1" ]; then
  not_run "broad read/list query plan is index-backed or projection-backed" "static plan guard was disabled"
else
  passed_if "broad read/list query plan is index-backed or projection-backed" "make validate-sqlc-plans and validate-hot-index-migrations logs in $report_dir" "static guards: hot-index migrations" "static guards: sqlc hot plans"
fi

if [ "$run_tests" = "1" ]; then
  passed_if "publish company version, publish park override, retire old active version" "backend protocol/vaccination integration tests cover publish, retirement, and park override resolution" "backend kernel targeted tests"
  passed_if "replay publish with same idempotency key" "backend protocol/vaccination idempotency tests" "backend kernel targeted tests"
  passed_if "reject conflicting idempotency payload" "backend protocol/vaccination HTTP/app idempotency conflict tests" "backend kernel targeted tests"
  passed_if "outbox relay transient failure retry path" "outbox repository/app integration tests cover retry scheduling and stale claim reclaim" "backend kernel targeted tests"
  passed_if "outbox dead-letter + replay path" "outbox service/repository/http tests and cmd/outbox-dlq tests" "backend kernel targeted tests"
  passed_if "Pub/Sub/eventbus duplicate event path" "domain consumer/vaccination generation idempotency tests" "backend kernel targeted tests"
  passed_if "generation success, completed replay, stale running retry, failed retry" "vaccination generation integration tests cover run success/replay/stale heartbeat/failed replay" "backend kernel targeted tests"
  passed_if "cursor/page resume or explicit full-replay acceptance for non-parallel paths" "current non-parallel path is idempotent full replay; parallel cursor resume remains a future certification upgrade" "backend kernel targeted tests"
  passed_if "bounded page processing at realistic page size" "generation/sweeper targeted tests plus sql plan guards" "backend kernel targeted tests"
  passed_if "vaccination-slice multi-park scoped generation" "park override and effective version cache tests" "backend kernel targeted tests"
  passed_if "missed/recovered/deferred animal path" "obligation recovery/cancel and vaccination generation tests" "backend kernel targeted tests"
  passed_if "shift re-scope path" "SM-2 obligation shift integration tests" "backend kernel targeted tests"
  passed_if "death/sale/exit cancel path" "SM-3 cancel and identity exit tests" "backend kernel targeted tests"
  passed_if "no exited animal remains overdue in process surfaces after refresh" "cancel/projection targeted tests verify exited animals are excluded from open work" "backend kernel targeted tests"
  passed_if "proof submission accepted, rejected/rework, and duplicate submit" "SOP/vaccination tests and live rework proof when enabled" "backend kernel targeted tests"
  passed_if "inventory reserve/consume/release idempotency" "inventory and obligation reserve integration tests" "backend kernel targeted tests"
else
  not_run "backend kernel targeted test-backed checklist items" "GOATOS_KERNEL_E2E_RUN_TESTS=0"
fi

if [ "$run_live" = "1" ]; then
  passed_if "outbox relay success path" "live vaccination-chain-proof.log" "live vaccination chain proof"
  passed_if "sweeper creates batch/drive/task" "live vaccination-chain-proof.log" "live vaccination chain proof"
  passed_if "read model refresh and API surfaces reflect process state" "live vaccination-chain-proof.log" "live vaccination chain proof"
  passed_if "proof submission accepted path" "live vaccination-chain-proof.log" "live vaccination chain proof"
  passed_if "procurement accepted/rejected/source-only boundaries" "live procurement-vaccination-e2e-matrix.log" "live procurement vaccination matrix"
  if [ "$run_browser" = "1" ]; then
    passed_if "read model refresh and UI surface reflects process state" "live admin-web visual smoke covers CT/AC/Calendar/PA/Workflows/Vaccination/Passport/Procurement in desktop+narrow viewports" "live admin-web visual smoke"
  fi
  passed_if "proof submission rejected/rework path" "live vaccination-rework-proof.log" "live vaccination rework proof"
  passed_if "trusted procurement/existing vaccination history suppression" "live vaccination-trusted-history-proof.log" "live vaccination trusted-history proof"
else
  not_run "live data-plane and UI E2E cases" "GOATOS_KERNEL_E2E_RUN_LIVE=0"
fi

not_implemented "real GCP Pub/Sub topic/subscription/IAM/ack/DLQ proof before cloud readiness" "requires verified goatos-stg/test GCP context; local report does not mutate cloud"
not_implemented "multi-tenant noisy-neighbor outbox/sweeper fairness path" "reusable WorkClaimer/LeaseManager/BackpressurePolicy behavior still needs implementation and staged fairness load evidence"
not_implemented "effective-cohort CLI/backfill generation durable run path" "tracked as high-scale plan item 8.4"
not_implemented "notification circuit breaker open/half-open/closed behavior and pending visibility" "durable retry/exhausted notification queue exists; provider circuit breaker state is still a plan item"
not_implemented "multi-domain scoped generation for multi_domain_kernel certification" "Feed Direction is not operational; vaccination_slice_local scope does not require mixed categories"
not_implemented "1M full-chain staging scale certification thresholds" "requires goatos-stg 1M benchmark profile, loaded dataset checksum, and scaled EXPLAIN ANALYZE evidence"

{
  report_result="passed"
  if [ "$overall_status" -ne 0 ]; then
    report_result="failed"
  fi
  printf '%s\n\n' '# High-Scale Kernel E2E Report'
  printf '%s\n' "- Run ID: \`$run_id\`"
  printf '%s\n' "- Result: \`$report_result\`"
  printf '%s\n' "- Scope: \`$scope\`"
  printf '%s\n' "- Certification mode: \`$certification_mode\`"
  printf '%s\n' "- Allows not-implemented rows: \`$allow_not_implemented\`"
  printf '%s\n' "- Allows local checksum drift: \`$allow_local_checksum_drift\`"
  if [ "$certification_mode" = "1" ]; then
    printf '%s\n' "- Certification claim: \`strict_candidate\`"
  else
    printf '%s\n' "- Certification claim: \`none_local_proof_only\`"
  fi
  printf '%s\n' "- Started/ended: \`$(timestamp)\`"
  printf '%s\n' "- Report directory: \`$report_dir\`"
  printf '\n## Commands\n\n'
  printf '| Command group | Status | Started | Ended | Exit | Log |\n'
  printf '| --- | --- | --- | --- | --- | --- |\n'
  awk -F '\t' '{ printf "| `%s` | %s | `%s` | `%s` | `%s` | `%s` |\n", $1, $2, $3, $4, $5, $6 }' "$commands_tsv"
  printf '\n## Minimum E2E Checklist\n\n'
  printf '| Case | Result | Evidence |\n'
  printf '| --- | --- | --- |\n'
  awk -F '\t' '{ printf "| %s | %s | %s |\n", $1, $2, $3 }' "$checklist_tsv"
} >"$report_md"

log "Report written: $report_md"

if [ "$overall_status" -ne 0 ]; then
  log "High-scale kernel E2E-all finished with failures. See $report_md"
  exit "$overall_status"
fi

log "High-scale kernel E2E-all finished. See $report_md"
