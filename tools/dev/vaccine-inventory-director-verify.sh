#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
workspace_root="$(cd "$repo_root/.." && pwd)"
run_id="${GOATOS_VACCINE_INVENTORY_VERIFY_RUN_ID:-vaccine-inventory-$(date -u +%Y%m%d-%H%M%S)}"
default_report_rel=".codex-goatos-render/vaccine-inventory-director/$run_id"
report_dir="${GOATOS_VACCINE_INVENTORY_VERIFY_REPORT_DIR:-$repo_root/$default_report_rel}"
report_label="${GOATOS_VACCINE_INVENTORY_VERIFY_REPORT_LABEL:-$default_report_rel}"
mkdir -p "$report_dir"

branch="$(cd "$repo_root" && git branch --show-current 2>/dev/null || true)"
head_sha="$(cd "$repo_root" && git rev-parse HEAD 2>/dev/null || true)"
head_short="$(cd "$repo_root" && git rev-parse --short=12 HEAD 2>/dev/null || true)"
origin_main_base="$(cd "$repo_root" && git merge-base HEAD origin/main 2>/dev/null || true)"
worktree_dirty="unknown"
if [ -n "$(cd "$repo_root" && git status --porcelain 2>/dev/null)" ]; then
  worktree_dirty="true"
else
  worktree_dirty="false"
fi

commands_tsv="$report_dir/commands.tsv"
report_md="$report_dir/report.md"
: >"$commands_tsv"
overall_status=0

timestamp() {
  date -u +"%Y-%m-%dT%H:%M:%SZ"
}

slug() {
  printf '%s' "$1" | tr '[:upper:]' '[:lower:]' | sed -E 's/[^a-z0-9]+/-/g; s/^-//; s/-$//'
}

run_step() {
  local label="$1"
  local command="$2"
  local name log_file log_label started_at ended_at rc
  name="$(slug "$label")"
  log_file="$report_dir/$name.log"
  log_label="$report_label/$name.log"
  started_at="$(timestamp)"
  printf '%s START %s\n' "$started_at" "$label"
  set +e
  (
    cd "$repo_root"
    bash -lc "$command"
  ) >"$log_file" 2>&1
  rc=$?
  set -e
  ended_at="$(timestamp)"
  if [ "$rc" -eq 0 ]; then
    printf '%s PASS  %s\n' "$ended_at" "$label"
    printf '%s\tpassed\t%s\t%s\t%s\t%s\n' "$label" "$started_at" "$ended_at" "$rc" "$log_label" >>"$commands_tsv"
  else
    printf '%s FAIL  %s rc=%s log=%s\n' "$ended_at" "$label" "$rc" "$log_label"
    printf '%s\tfailed\t%s\t%s\t%s\t%s\n' "$label" "$started_at" "$ended_at" "$rc" "$log_label" >>"$commands_tsv"
    overall_status=1
  fi
}

run_func() {
  local label="$1"
  local fn="$2"
  local name log_file log_label started_at ended_at rc
  name="$(slug "$label")"
  log_file="$report_dir/$name.log"
  log_label="$report_label/$name.log"
  started_at="$(timestamp)"
  printf '%s START %s\n' "$started_at" "$label"
  set +e
  (
    cd "$repo_root"
    "$fn"
  ) >"$log_file" 2>&1
  rc=$?
  set -e
  ended_at="$(timestamp)"
  if [ "$rc" -eq 0 ]; then
    printf '%s PASS  %s\n' "$ended_at" "$label"
    printf '%s\tpassed\t%s\t%s\t%s\t%s\n' "$label" "$started_at" "$ended_at" "$rc" "$log_label" >>"$commands_tsv"
  else
    printf '%s FAIL  %s rc=%s log=%s\n' "$ended_at" "$label" "$rc" "$log_label"
    printf '%s\tfailed\t%s\t%s\t%s\t%s\n' "$label" "$started_at" "$ended_at" "$rc" "$log_label" >>"$commands_tsv"
    overall_status=1
  fi
}

api_client_generation_drift_scope() {
  npm --prefix packages/api-client run generate

  local changed
  changed="$(git diff --name-only -- packages/api-client/src/generated)"
  if [ "$changed" != "packages/api-client/src/generated/app-api.ts" ]; then
    printf 'unexpected generated client diff scope:\n%s\n' "$changed"
    return 1
  fi

  local diff_terms="$report_dir/api-client-diff-terms.log"
  git diff -- contracts/openapi/app-api.yaml packages/api-client/src/generated/app-api.ts |
    rg -n 'inventory_vaccine|task_proof|stock_fridge_video|PCCareInventoryRequirement|PCCareTaskProof|task_proofs|current_or_carry|appRegisterPCCareTaskProof|human-plannable|Kernel-owned|create wizard' >"$diff_terms"

  local term
  for term in inventory_vaccine task_proof stock_fridge_video PCCareInventoryRequirement PCCareTaskProof task_proofs current_or_carry appRegisterPCCareTaskProof human-plannable Kernel-owned "create wizard"; do
    if ! rg -q "$term" "$diff_terms"; then
      printf 'missing expected api-client diff term: %s\n' "$term"
      return 1
    fi
  done
}

feature_docs_and_scripts_hygiene() {
  local bad_home bad_user bad_short bad_local_docker bad_docker_backed bad_skip_no_docker
  local allowed_skip_no_docker_regex
  local bad_pg_docker bad_db_container bad_local_pg bad_local_db
  local bad_local_database bad_loopback_a bad_loopback_b
  bad_home="$(printf '/%s' 'Users')"
  bad_user="$(printf 'ra%s' 'viteja')"
  bad_short="$(printf 'ra%s' 'vi')"
  bad_local_docker="$(printf 'local %s' 'Docker')"
  bad_docker_backed="$(printf '%s-backed' 'Docker')"
  bad_skip_no_docker="$(printf 'SkipIfNo%s' 'Docker')"
  allowed_skip_no_docker_regex="$(printf 'pgtest\\.SkipIfNo%s\\(t\\)' 'Docker')"
  bad_pg_docker="$(printf 'Postgres/%s' 'Docker')"
  bad_db_container="$(printf 'database %s' 'container')"
  bad_local_pg="$(printf 'local %s' 'Postgres')"
  bad_local_db="$(printf 'local %s' 'DB')"
  bad_local_database="$(printf 'local %s' 'database')"
  bad_loopback_a="$(printf '127.%s' '0.0.1')"
  bad_loopback_b="$(printf 'local%s' 'host')"

  local hygiene_targets="$report_dir/hygiene-targets.txt"
  local hygiene_findings="$report_dir/hygiene-findings.log"
  : >"$hygiene_targets"
  printf '%s\n' \
    context/execution/vaccine-inventory-director-task-progress.md \
    context/execution/vaccine-inventory-director-final-coverage-report.md \
    tools/dev/vaccine-inventory-director-verify.sh \
    context/execution/vaccine-inventory-director-e2e-2026-08-26 >>"$hygiene_targets"
  {
    git diff --name-only --diff-filter=ACMRTUXB
    git ls-files --others --exclude-standard
  } | rg '(^|/)(context|docs|tools|scripts)/|\.(md|sh|mjs|js|yaml|yml|json|xml)$' >>"$hygiene_targets" || true
  sort -u "$hygiene_targets" -o "$hygiene_targets"

  set +e
  tr '\n' '\0' <"$hygiene_targets" |
    xargs -0 rg -n --no-messages \
      -e "$bad_home" \
      -e "$bad_user" \
      -e "$bad_short" \
      -e "$bad_local_docker" \
      -e "$bad_docker_backed" \
      -e "$bad_skip_no_docker" \
      -e "$bad_pg_docker" \
      -e "$bad_db_container" \
      -e "$bad_local_pg" \
      -e "$bad_local_db" \
      -e "$bad_local_database" \
      -e "$bad_loopback_a" \
      -e "$bad_loopback_b" >"$hygiene_findings" 2>/dev/null
  local rc=$?
  set -e
  if [ "$rc" -eq 0 ]; then
    cat "$hygiene_findings"
    return 1
  fi
  if [ "$rc" -gt 1 ]; then
    cat "$hygiene_findings" 2>/dev/null || true
    return "$rc"
  fi

  local source_hygiene_targets="$report_dir/source-hygiene-targets.txt"
  local source_hygiene_findings="$report_dir/source-hygiene-findings.log"
  : >"$source_hygiene_targets"
  {
    git diff --name-only --diff-filter=ACMRTUXB
    git ls-files --others --exclude-standard
  } | rg '\.(go|kt|kts|tsx|ts|mjs|js|sql|yaml|yml|sh)$' >>"$source_hygiene_targets" || true
  sort -u "$source_hygiene_targets" -o "$source_hygiene_targets"

  set +e
  tr '\n' '\0' <"$source_hygiene_targets" |
    xargs -0 rg -n --no-messages \
      -e "$bad_skip_no_docker" \
      -e "$bad_local_docker" \
      -e "$bad_docker_backed" \
      -e "$bad_pg_docker" >"$source_hygiene_findings" 2>/dev/null
  rc=$?
  set -e
  if [ "$rc" -eq 0 ]; then
    if rg -v "$allowed_skip_no_docker_regex" "$source_hygiene_findings"; then
      return 1
    fi
    return 0
  fi
  if [ "$rc" -gt 1 ]; then
    cat "$source_hygiene_findings" 2>/dev/null || true
    return "$rc"
  fi
}

source_marker_audit() {
  local source_targets="$report_dir/source-marker-targets.txt"
  local source_findings="$report_dir/source-marker-findings.log"
  local marker_todo marker_fixme marker_xxx marker_hack marker_debugger
  marker_todo="$(printf 'TO%s' 'DO')"
  marker_fixme="$(printf 'FIX%s' 'ME')"
  marker_xxx="$(printf 'X%s' 'XX')"
  marker_hack="$(printf 'HA%s' 'CK')"
  marker_debugger="$(printf 'debug%s' 'ger;')"
  : >"$source_targets"
  {
    git diff --name-only --diff-filter=ACMRTUXB
    git ls-files --others --exclude-standard
  } | rg '\.(go|kt|kts|tsx|ts|mjs|js|sql|yaml|yml|sh)$' >>"$source_targets" || true
  sort -u "$source_targets" -o "$source_targets"

  set +e
  tr '\n' '\0' <"$source_targets" |
    xargs -0 rg -n --no-messages \
      -e "$marker_todo" \
      -e "$marker_fixme" \
      -e "$marker_xxx" \
      -e "$marker_hack" \
      -e 'console\.log' \
      -e 'println\(' \
      -e 'printStackTrace\(' \
      -e "$marker_debugger" \
      -e 'spew\.' \
      -e 'dump\(' >"$source_findings" 2>/dev/null
  local rc=$?
  set -e
  if [ "$rc" -eq 0 ]; then
    if rg -v 'backend/cmd/kernel-worker/main.go:28:.*fmt\.Fprintln\(os\.Stderr, err\)' "$source_findings"; then
      return 1
    fi
    return 0
  fi
  if [ "$rc" -gt 1 ]; then
    cat "$source_findings" 2>/dev/null || true
    return "$rc"
  fi
}

stg_deploy_contract_guard() {
  jq -e '
    .stg_deploy.deploy_authority == "slack-cloud-build-cloud-deploy" and
    .stg_deploy.source_ref == "origin/main" and
    .stg_deploy.requires_clean_worktree == true and
    .stg_deploy.scripts_usage == "break_glass_or_repair_only"
  ' context/deploy-contract.json >/dev/null

  rg -q 'Slack deploy button|Slack/Cloud Build/Cloud Deploy|Cloud Deploy' docs/runbooks/stg-deploy.md
  rg -q 'Direct `gcloud run services update`|direct Cloud Run service updates|Forbidden Deploy Detours' docs/runbooks/cloud-deploy-staging.md
}

final_report_consistency_guard() {
  local report="context/execution/vaccine-inventory-director-final-coverage-report.md"
  local latest_run="codex-resume-20260826-full-oci-after-doc-hygiene"

  rg -q '^## Completion Ledger$' "$report"
  rg -q "$latest_run" "$report"
  rg -q 'stg_proves_current_worktree=false' "$report"
  rg -q 'STG Cloud Run is not serving this[[:space:]]+feature worktree' "$report"
  rg -q 'STG must serve this committed feature for final completion' "$report"
  rg -q 'Automatic director task at T-7, including direct DB schedules' "$report"
  rg -q 'Kernel-owned category cannot be manually planned' "$report"
  rg -q 'Always-on watcher, not UI-only or one-off script' "$report"
  rg -q '24-hour task and overdue carry-over' "$report"
  rg -q 'Hide old finished cards while showing same-day done' "$report"
  rg -q 'Director app card on top and fridge photo/video proof' "$report"
  rg -q 'CEO progress and verifier media' "$report"
  rg -q 'Manual/procured animal and age/rule obligation generation' "$report"
  rg -q 'Shifting/partition edge cases' "$report"
  rg -q 'ET\+TT 10.*PPR 25|PPR 25.*ET\+TT 10' "$report"
}

run_step "backend focused vaccine inventory suite" \
  "cd backend && go test -count=1 ./internal/verification/... ./internal/pccare/... ./internal/kernelstages ./internal/workforce/app ./internal/permissions ./cmd/kernel-worker"

run_step "vaccination procurement scheduling support" \
  "cd backend && go test -count=1 ./internal/vaccination/app -run 'Procured|procured|SchedulePath|GenerateForVersion' -v"

run_step "admin web inventory progress and typecheck" \
  "cd apps/admin-web && npm test -- --test-name-pattern='inventory|pc care inventory|vaccination page mounts|verification action labels|filters out old' && npm run typecheck"

run_step "contract validation" \
  "npm --prefix tools/contract-validation run validate"

run_func "api client generation drift scope" api_client_generation_drift_scope

run_step "migration duplicate versions guard" \
  "make migration-duplicate-versions-guard"

run_step "android pc care task proof sync" \
  "cd apps/goatos-android && ./gradlew --console=plain -q :core:core-data:testDebugUnitTest --tests 'sg.mesha.goatos.core.data.sync.SyncEngineTest.PC Care task proof registration resolves uploaded proof and uses stable idempotency key'"

run_step "android inventory route and worklist tests" \
  "cd apps/goatos-android && ./gradlew --console=plain -q :app:testStgDebugUnitTest --tests 'sg.mesha.goatos.ui.AppStartDestinationTest' --tests 'sg.mesha.goatos.ui.ExecutionRouteIdentityTest' --tests 'sg.mesha.goatos.ui.PcCareInventoryTaskScreenshotTest' --tests 'sg.mesha.goatos.viewmodel.PcCareInventoryTaskProofTest' --tests 'sg.mesha.goatos.viewmodel.PcCareWorklistDateWindowTest' --tests 'sg.mesha.goatos.viewmodel.PcCareSubmitGateTest' --tests 'sg.mesha.goatos.viewmodel.PcCareSlotParallelismTest'"

run_func "feature docs and scripts hygiene" feature_docs_and_scripts_hygiene

run_func "source marker audit" source_marker_audit

run_func "stg deploy contract guard" stg_deploy_contract_guard

run_func "final report consistency guard" final_report_consistency_guard

oci_status="skipped"
oci_reason=""
oci_helper="${GOATOS_OCI_HELPER:-$workspace_root/tools/local/oci-goatos-a1-dev.sh}"
oci_env="${GOATOS_OCI_DB_ENV:-$workspace_root/local-data/goatos-stg-to-oci/oci-goatos-db.env}"
oci_helper_label="${GOATOS_OCI_HELPER_LABEL:-tools/local/oci-goatos-a1-dev.sh}"
oci_env_label="${GOATOS_OCI_DB_ENV_LABEL:-local-data/goatos-stg-to-oci/oci-goatos-db.env}"
if [ -x "$oci_helper" ] && [ -f "$oci_env" ]; then
  set +e
  (
    # shellcheck disable=SC1090
    source "$oci_env"
    psql "$DATABASE_URL" -tAc 'select current_database(), inet_server_addr(), inet_server_port();'
  ) >"$report_dir/oci-db-check.log" 2>&1
  rc=$?
  set -e
  if [ "$rc" -eq 0 ]; then
    oci_status="reachable"
    printf '%s PASS  oci tunnel database check\n' "$(timestamp)"
    printf '%s\tpassed\t%s\t%s\t%s\t%s\n' "oci tunnel database check" "$(timestamp)" "$(timestamp)" "0" "$report_label/oci-db-check.log" >>"$commands_tsv"
  else
    oci_reason="OCI DB tunnel is not reachable; run '$oci_helper_label tunnel' in another terminal, then rerun this helper"
    printf '%s SKIP  oci tunnel database check: %s\n' "$(timestamp)" "$oci_reason"
    printf '%s\tskipped\t%s\t%s\t%s\t%s\n' "oci tunnel database check" "$(timestamp)" "$(timestamp)" "0" "$oci_reason" >>"$commands_tsv"
  fi
else
  oci_reason="OCI helper/env missing; expected helper=$oci_helper_label env=$oci_env_label"
  printf '%s SKIP  oci tunnel database check: %s\n' "$(timestamp)" "$oci_reason"
  printf '%s\tskipped\t%s\t%s\t%s\t%s\n' "oci tunnel database check" "$(timestamp)" "$(timestamp)" "0" "$oci_reason" >>"$commands_tsv"
fi

stg_status="skipped"
stg_reason=""
stg_images_match_head_commit="unknown"
stg_proves_current_worktree="unknown"
stg_report="$report_dir/stg-cloud-run.tsv"
stg_report_label="$report_label/stg-cloud-run.tsv"
if command -v gcloud >/dev/null 2>&1; then
  stg_status="attempted"
  printf 'service\trevision\timage\n' >"$stg_report"
  for service in goatos-api-stg goatos-admin-web-stg goatos-kernel-worker-stg; do
    set +e
    desc="$(gcloud run services describe "$service" --project goatos-stg --region asia-south1 --format='value(status.latestReadyRevisionName,spec.template.spec.containers[0].image)' 2>"$report_dir/stg-$service.err")"
    rc=$?
    set -e
    if [ "$rc" -eq 0 ]; then
      printf '%s\t%s\n' "$service" "$desc" >>"$stg_report"
    else
      stg_status="failed"
    stg_reason="gcloud describe failed for $service; see $report_label/stg-$service.err"
      overall_status=1
      break
    fi
  done
else
  stg_reason="gcloud command not found; STG Cloud Run inspection skipped"
fi
if [ -s "$stg_report" ] && [ -n "$head_short" ]; then
  if awk -v sha="$head_short" 'NR > 1 && index($0, sha) == 0 { missing = 1 } END { exit missing ? 1 : 0 }' "$stg_report"; then
    stg_images_match_head_commit="true"
  else
    stg_images_match_head_commit="false"
  fi
  if [ "$worktree_dirty" = "false" ] && [ "$stg_images_match_head_commit" = "true" ]; then
    stg_proves_current_worktree="true"
  else
    stg_proves_current_worktree="false"
  fi
fi

{
  printf '# Vaccine Inventory Director Verification\n\n'
  printf -- '- run_id: `%s`\n' "$run_id"
  printf -- '- generated_at: `%s`\n' "$(timestamp)"
  printf -- '- branch: `%s`\n' "$branch"
  printf -- '- head_sha: `%s`\n' "$head_sha"
  printf -- '- head_short: `%s`\n' "$head_short"
  printf -- '- merge_base_origin_main: `%s`\n' "$origin_main_base"
  printf -- '- worktree_dirty: `%s`\n' "$worktree_dirty"
  printf -- '- status: `%s`\n' "$([ "$overall_status" -eq 0 ] && printf passed || printf failed)"
  printf -- '- oci_db_check: `%s`\n' "$oci_status"
  printf -- '- oci_helper: `%s`\n' "$oci_helper_label"
  printf -- '- oci_db_env: `%s`\n' "$oci_env_label"
  if [ -n "$oci_reason" ]; then
    printf -- '- oci_reason: `%s`\n' "$oci_reason"
  fi
  printf -- '- stg_cloud_run: `%s`\n' "$stg_status"
  if [ -n "$stg_reason" ]; then
    printf -- '- stg_reason: `%s`\n' "$stg_reason"
  fi
  if [ -s "$stg_report" ]; then
    printf -- '- stg_report: `%s`\n' "$stg_report_label"
  fi
  printf -- '- stg_images_match_head_commit: `%s`\n' "$stg_images_match_head_commit"
  printf -- '- stg_proves_current_worktree: `%s`\n' "$stg_proves_current_worktree"
  printf '\n## Commands\n\n'
  printf '| Step | Status | Exit | Log |\n'
  printf '|---|---:|---:|---|\n'
  while IFS=$'\t' read -r label status _started _ended rc log; do
    printf '| %s | %s | %s | `%s` |\n' "$label" "$status" "$rc" "$log"
  done <"$commands_tsv"
  if [ -s "$stg_report" ]; then
    printf '\n## STG Cloud Run\n\n'
    printf '| Service | Revision | Image |\n'
    printf '|---|---|---|\n'
    tail -n +2 "$stg_report" | while IFS=$'\t' read -r service revision image; do
      printf '| %s | `%s` | `%s` |\n' "$service" "$revision" "$image"
    done
  fi
} >"$report_md"

printf 'report=%s\n' "$report_label/report.md"
exit "$overall_status"
