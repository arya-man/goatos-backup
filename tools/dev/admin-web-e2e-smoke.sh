#!/usr/bin/env bash
# Mutating local admin-web E2E smoke.
#
# This is a runner for the currently implemented proof paths:
#   1. data-plane vaccination chain proof
#   2. four-goat procurement -> vaccination matrix proof
#   3. live admin-web visual/click smoke
set -euo pipefail
export PATH="/opt/homebrew/opt/postgresql@15/bin:$PATH"

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
. "$repo_root/tools/dev/e2e-db-env.sh"
goatos_e2e_require_isolated_database "tools/dev/admin-web-e2e-smoke.sh"
backend_dir="$repo_root/backend"
run_id="${GOATOS_E2E_RUN_ID:-E2E-$(date +%Y%m%d-%H%M%S)}"
report_dir="${GOATOS_E2E_REPORT_DIR:-$repo_root/.codex-goatos-render/e2e-smoke/$run_id}"

export GOATOS_ENV="${GOATOS_ENV:-local}"
export GOATOS_AUTH_MODE="${GOATOS_AUTH_MODE:-bearer}"
export GOATOS_AUTH_ISSUER="${GOATOS_AUTH_ISSUER:-goatos-local}"
export GOATOS_AUTH_AUDIENCE="${GOATOS_AUTH_AUDIENCE:-goatos-api}"
export GOATOS_AUTH_HS256_SECRET="${GOATOS_AUTH_HS256_SECRET:-goatos-local-dev-secret-32-bytes-min}"
export GOATOS_AUTH_MAX_TOKEN_TTL="${GOATOS_AUTH_MAX_TOKEN_TTL:-24h}"
export GOATOS_TENANT_ID="${GOATOS_TENANT_ID:-00000000-0000-4000-8000-000000000001}"
export GOATOS_LOCAL_USER_ID="${GOATOS_LOCAL_USER_ID:-90000000-0000-4000-8000-000000000101}"
export GOATOS_API_BASE_URL="${GOATOS_API_BASE_URL:-http://127.0.0.1:8080}"
export GOATOS_ADMIN_WEB_BASE_URL="${GOATOS_ADMIN_WEB_BASE_URL:-http://127.0.0.1:3300}"

mkdir -p "$report_dir"

fail() {
  echo "FAIL $*" >&2
  exit 1
}

jq_py() {
  python3 -c "import sys,json;d=json.load(sys.stdin);print($1)" 2>/dev/null || true
}

psqlq() {
  psql "$DATABASE_URL" -tAc "$1"
}

. "$repo_root/tools/dev/vaccination-active-fixture.sh"

relay_once() {
  local limit="${1:-500}"
  (
    cd "$backend_dir"
    GOATOS_OUTBOX_ALLOW_NONDURABLE=1 GOATOS_OUTBOX_PUBLISHER=eventbus \
      go run ./cmd/outbox-relay -limit "$limit" >/dev/null
  )
}

relay_until_published() {
  local label="$1"
  local aggregate="$2"
  local event_type="$3"
  local i status last_error
  for i in $(seq 1 5); do
    relay_once 500
    status="$(
      psqlq "select status from outbox_messages where tenant_id='$GOATOS_TENANT_ID' and aggregate_id='$aggregate' and event_type='$event_type' order by created_at desc limit 1"
    )"
    if [ "$status" = "published" ]; then
      echo "$label: $event_type published"
      return 0
    fi
    if [ "$status" = "failed" ] || [ "$status" = "dead_letter" ]; then
      last_error="$(
        psqlq "select coalesce(last_error,'') from outbox_messages where tenant_id='$GOATOS_TENANT_ID' and aggregate_id='$aggregate' and event_type='$event_type' order by created_at desc limit 1"
      )"
      fail "$label target $event_type for aggregate=$aggregate reached status=$status last_error=$last_error"
    fi
    sleep 1
  done
  fail "$label did not publish $event_type for aggregate=$aggregate; last_status=$status"
}

assert_migration_head() {
  local latest_version applied
  latest_version="$(
    find "$repo_root/backend/migrations/postgres" -maxdepth 1 -type f -name '*.sql' \
      -exec basename {} .sql \; | sort | tail -1
  )"
  [ -n "$latest_version" ] || fail "no Postgres migrations found"
  applied="$(psqlq "select count(*) from goatos_schema_migrations where version='$latest_version'" 2>/dev/null || true)"
  [ "$applied" = "1" ] || fail "local Postgres is not migrated to head version $latest_version"
}

assert_calendar_missed_status_allowed() {
  local constraint
  constraint="$(psqlq "select pg_get_constraintdef(oid) from pg_constraint where conname='calendar_event_status_check'" 2>/dev/null || true)"
  case "$constraint" in
    *"'missed'"*) ;;
    *) fail "local Postgres has stale calendar_event_status_check; rebuild it from the clean-slate baseline before running vaccination smoke" ;;
  esac
}

on_exit() {
  local status=$?
  {
    printf 'run_id=%s\n' "$run_id"
    printf 'status=%s\n' "$status"
    printf 'api=%s\n' "$GOATOS_API_BASE_URL"
    printf 'admin_web=%s\n' "$GOATOS_ADMIN_WEB_BASE_URL"
    printf 'tenant=%s\n' "$GOATOS_TENANT_ID"
    printf 'ended_at=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  } >"$report_dir/summary.env"

  if [ "$status" -ne 0 ]; then
    echo "admin-web E2E smoke failed; report_dir=$report_dir" >&2
    if [ -f "$repo_root/.codex-goatos-render/logs/local-api.log" ]; then
      tail -120 "$repo_root/.codex-goatos-render/logs/local-api.log" >"$report_dir/local-api-tail.log" || true
    fi
  else
    echo "admin-web E2E smoke passed; report_dir=$report_dir"
  fi
}
trap on_exit EXIT

echo "## admin-web-e2e-smoke run_id=$run_id"

echo "### readiness: API"
curl -fsS "$GOATOS_API_BASE_URL/readyz" >/dev/null

echo "### readiness: Postgres migrations"
assert_migration_head
assert_calendar_missed_status_allowed

echo "### readiness: admin-web"
curl -fsS "$GOATOS_ADMIN_WEB_BASE_URL/" >/dev/null

echo "### seed: local grant"
(
  cd "$backend_dir"
  go run ./cmd/seed-dev-grant \
    -tenant-id "$GOATOS_TENANT_ID" \
    -user-id "$GOATOS_LOCAL_USER_ID" \
    -role ceo_internal
)

echo "### seed: vaccination trigger baseline"
(
  cd "$backend_dir"
  go run ./cmd/seed-vaccination-trigger
)
resolve_vaccination_fixture
echo "vaccination_fixture=$MATRIX_SUMMARY primary=$RULE_SUMMARY item=$ITEM"

echo "### token: mint local bearer"
GOATOS_BEARER_TOKEN="$(
  cd "$backend_dir"
  go run ./cmd/mint-dev-token \
    -tenant-id "$GOATOS_TENANT_ID" \
    -user-id "$GOATOS_LOCAL_USER_ID" \
    -ttl 2h
)"
export GOATOS_BEARER_TOKEN

echo "### frontend guard: herd import security"
npm --prefix "$repo_root/apps/admin-web" run check:herd-import-security | tee "$report_dir/check-herd-import-security.log"

seed_open_vaccination_goat() {
  local stamp="$1"
  local proof_park="00000000-0000-4000-8000-000000003001"
  local seed_shed="00000000-0000-4000-8000-000000003101"
  local entry_date
  local dob_day28
  entry_date="$(date -u +%F)"
  if dob_day28="$(date -u -v-28d +%F 2>/dev/null)"; then
    :
  else
    dob_day28="$(date -u -d "$entry_date - 28 days" +%F)"
  fi
  entry_date="$dob_day28"

  local create
  create="$(
    curl -sS \
      -H "Authorization: Bearer $GOATOS_BEARER_TOKEN" \
      -H "Idempotency-Key: smoke-open-$stamp" \
      -H "Content-Type: application/json" \
      -X POST "$GOATOS_API_BASE_URL/admin/goats" \
      -d @- <<JSON
{"animal_identifier_1":"SMOKE-OPEN-$stamp-A1","animal_identifier_2":"SMOKE-OPEN-$stamp-A2","species":"goat","park_id":"$proof_park","shed_id":"$seed_shed","sex":"female","breed":"all","dob":"$dob_day28","dob_estimated":true,"origin_type":"birth","entry_date":"$entry_date","management_stage":"K2","health_status":"healthy","reproductive_status":"open","evidence_refs":[{"evidence_type":"source_record","evidence_id":"smoke-open-$stamp"}]}
JSON
  )"
  local goat_id
  goat_id="$(echo "$create" | jq_py 'd["goat"]["goat_id"]')"
  if [ -z "$goat_id" ]; then
    echo "open visual seed failed: $create" >&2
    return 1
  fi

  relay_until_published "open visual goat.created delivery" "$goat_id" "goat.created"
  (
    cd "$backend_dir"
    GOATOS_TENANT_ID="$GOATOS_TENANT_ID" \
      go run ./cmd/obligation-sweeper \
        -tenant-id "$GOATOS_TENANT_ID" \
        -version-id "$VERSION" \
        -sop-version-id "$SOPVER" \
        -vaccine-item-id "$ITEM" \
        -actor-id "$GOATOS_LOCAL_USER_ID" >/dev/null
  )

  local obligation_count
  obligation_count="$(
    psql "$DATABASE_URL" -tAc \
      "select count(*) from obligation_instances where tenant_id='$GOATOS_TENANT_ID' and target_id='$goat_id' and protocol_version_id='$VERSION' and rule_id='$RULE'"
  )"
  if [ "$obligation_count" != "1" ]; then
    echo "open visual seed did not create exactly one obligation for goat=$goat_id; count=$obligation_count" >&2
    return 1
  fi
  local obligation_id batch_id task_id due_before administered_at
  obligation_id="$(psqlq "select obligation_id from obligation_instances where tenant_id='$GOATOS_TENANT_ID' and target_id='$goat_id' and protocol_version_id='$VERSION' and rule_id='$RULE' limit 1")"
  batch_id="$(psqlq "select batch_id from obligation_instances where tenant_id='$GOATOS_TENANT_ID' and obligation_id='$obligation_id'")"
  task_id="$(psqlq "select sop_task_id from obligation_batches where tenant_id='$GOATOS_TENANT_ID' and batch_id='$batch_id'")"
  due_before="$(psqlq "select to_char((coalesce(window_end, due_at) + interval '2 days') at time zone 'UTC', 'YYYY-MM-DD\"T\"HH24:MI:SS\"Z\"') from obligation_instances where tenant_id='$GOATOS_TENANT_ID' and obligation_id='$obligation_id'")"
  if [ -z "$task_id" ]; then
    echo "open visual seed did not create a SOP task for goat=$goat_id batch=$batch_id" >&2
    return 1
  fi
  if [ -z "$due_before" ]; then
    echo "open visual seed could not resolve due_before horizon for obligation=$obligation_id" >&2
    return 1
  fi

  administered_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  mkproof() {
    local subject="$1"
    local response proof_id upload_url full_url
    response="$(
      curl -sS \
        -H "Authorization: Bearer $GOATOS_BEARER_TOKEN" \
        -H "Content-Type: application/json" \
        -X POST "$GOATOS_API_BASE_URL/app/proofs/uploads" \
        -d "{\"proof_type\":\"video\",\"mime_type\":\"video/mp4\",\"scope_type\":\"task\",\"scope_id\":\"$task_id\",\"subject_type\":\"$subject\"}"
    )"
    proof_id="$(echo "$response" | jq_py 'd["proof"]["proof_id"]')"
    upload_url="$(echo "$response" | jq_py 'd["upload_url"]')"
    if [ -z "$proof_id" ] || [ -z "$upload_url" ]; then
      echo "open visual proof seed failed subject=$subject response=$response" >&2
      return 1
    fi
    case "$upload_url" in
      http*) full_url="$upload_url" ;;
      /*) full_url="$GOATOS_API_BASE_URL$upload_url" ;;
      *) full_url="$GOATOS_API_BASE_URL/$upload_url" ;;
    esac
    printf 'admin-web-smoke-%s-%s' "$subject" "$stamp" >"/tmp/goatos-admin-web-smoke-$subject-$stamp.mp4"
    curl -fsS \
      -H "Authorization: Bearer $GOATOS_BEARER_TOKEN" \
      -H "Content-Type: video/mp4" \
      -X PUT --data-binary @"/tmp/goatos-admin-web-smoke-$subject-$stamp.mp4" \
      "$full_url" >/dev/null
    echo "$proof_id"
  }
  local proof_shed_id proof_vial proof_admin submission completion_id
  proof_shed_id="$(mkproof shed)"
  proof_vial="$(mkproof vial_lot)"
  proof_admin="$(mkproof administration)"
  submission="$(
    curl -sS \
      -H "Authorization: Bearer $GOATOS_BEARER_TOKEN" \
      -H "Content-Type: application/json" \
      -X POST "$GOATOS_API_BASE_URL/app/tasks/$task_id/submissions" \
      -d @- <<JSON
{"sop_version_id":"$SOPVER","idempotency_key":"smoke-submit-$stamp","answers":{"vaccine_lot_id":"$LOT","cold_chain_verified":true,"shed_video":"$proof_shed_id","vial_lot_video":"$proof_vial","administration_video":"$proof_admin","goat_ids":["$goat_id"],"dose_ml_given":2,"route_site":"subcutaneous","administered_at":"$administered_at","adverse_reaction":false},"proof_refs":[{"proof_id":"$proof_shed_id","proof_type":"video","subject_type":"shed","upload_state":"completed"},{"proof_id":"$proof_vial","proof_type":"video","subject_type":"vial_lot","upload_state":"completed"},{"proof_id":"$proof_admin","proof_type":"video","subject_type":"administration","upload_state":"completed"}]}
JSON
  )"
  if [ -z "$(echo "$submission" | jq_py 'd.get("submission",{}).get("submission_id","")')" ]; then
    echo "open visual SOP submission failed: $submission" >&2
    return 1
  fi
  completion_id="$(psqlq "select completion_id from vaccination_completions where tenant_id='$GOATOS_TENANT_ID' and goat_id='$goat_id' and status='recorded' order by created_at desc limit 1")"
  if [ -z "$completion_id" ]; then
    echo "open visual seed did not create a recorded completion for goat=$goat_id task=$task_id" >&2
    return 1
  fi
  export GOATOS_SMOKE_GOAT_ID="$goat_id"
  export GOATOS_SMOKE_SHED_ID="$seed_shed"
  export GOATOS_SMOKE_BATCH_ID="$batch_id"
  export GOATOS_SMOKE_TASK_ID="$task_id"
  export GOATOS_SMOKE_COMPLETION_ID="$completion_id"
  export GOATOS_SMOKE_DUE_BEFORE="$due_before"
  echo "open visual goat=$goat_id obligation=$obligation_id batch=$batch_id task=$task_id completion=$completion_id due_before=$due_before"
}

assert_open_vaccination_owner_chain_ready() {
  local batch_id="${GOATOS_SMOKE_BATCH_ID:-}"
  local shed_id="${GOATOS_SMOKE_SHED_ID:-}"
  local due_before="${GOATOS_SMOKE_DUE_BEFORE:-}"
  [ -n "$batch_id" ] || fail "open vaccination seed did not export GOATOS_SMOKE_BATCH_ID"
  [ -n "$shed_id" ] || fail "open vaccination seed did not export GOATOS_SMOKE_SHED_ID"
  [ -n "$due_before" ] || fail "open vaccination seed did not export GOATOS_SMOKE_DUE_BEFORE"
  export GOATOS_ASSERT_BATCH_ID="$batch_id"

  local execution drive_count execution_count execution_state execution_operator execution_next
  execution="$(
    curl -sS \
      -H "Authorization: Bearer $GOATOS_BEARER_TOKEN" \
      "$GOATOS_API_BASE_URL/vaccination/execution/sheds/$shed_id?work_state=verification_pending&due_before=$due_before&limit=500"
  )"
  drive_count="$(echo "$execution" | jq_py 'len([r for r in d.get("drives", []) if r.get("driveId") == __import__("os").environ["GOATOS_ASSERT_BATCH_ID"]])')"
  [ "$drive_count" != "0" ] || fail "open vaccination batch $batch_id missing from /vaccination/execution/sheds/$shed_id drive summary"
  execution_count="$(echo "$execution" | jq_py 'len([r for r in d.get("rows", []) if (r.get("batchId") or r.get("driveId")) == __import__("os").environ["GOATOS_ASSERT_BATCH_ID"]])')"
  [ "$execution_count" != "0" ] || fail "open vaccination batch $batch_id missing from /vaccination/execution/sheds/$shed_id rows"
  execution_state="$(echo "$execution" | jq_py '(lambda rows: rows[0].get("workState", "") if rows else "")([r for r in d.get("rows", []) if (r.get("batchId") or r.get("driveId")) == __import__("os").environ["GOATOS_ASSERT_BATCH_ID"]])')"
  execution_operator="$(echo "$execution" | jq_py '(lambda rows: ((rows[0].get("owner") or {}).get("operatorName") or "") if rows else "")([r for r in d.get("rows", []) if (r.get("batchId") or r.get("driveId")) == __import__("os").environ["GOATOS_ASSERT_BATCH_ID"]])')"
  execution_next="$(echo "$execution" | jq_py '(lambda rows: rows[0].get("nextAction", "") if rows else "")([r for r in d.get("rows", []) if (r.get("batchId") or r.get("driveId")) == __import__("os").environ["GOATOS_ASSERT_BATCH_ID"]])')"
  [ "$execution_state" != "blocked" ] || fail "open vaccination execution batch $batch_id regressed to blocked"
  [ -n "$execution_operator" ] || fail "open vaccination execution batch $batch_id has no seeded operator"
  case "$execution_next" in
    *"Assign operator"*|*"Assign owner"*) fail "open vaccination execution batch $batch_id still routes to owner assignment: $execution_next" ;;
  esac

  local action action_count action_state action_owner_state action_operator action_next
  action="$(
    curl -sS \
      -H "Authorization: Bearer $GOATOS_BEARER_TOKEN" \
      "$GOATOS_API_BASE_URL/vaccination/action-center?work_state=verification_pending&due_before=$due_before&limit=500"
  )"
  action_count="$(echo "$action" | jq_py 'len([r for r in (d.get("items") or d.get("rows") or []) if r.get("batch_id") == __import__("os").environ["GOATOS_ASSERT_BATCH_ID"]])')"
  [ "$action_count" != "0" ] || fail "open vaccination batch $batch_id missing from /vaccination/action-center"
  action_state="$(echo "$action" | jq_py '(lambda rows: rows[0].get("work_state", "") if rows else "")([r for r in (d.get("items") or d.get("rows") or []) if r.get("batch_id") == __import__("os").environ["GOATOS_ASSERT_BATCH_ID"]])')"
  action_owner_state="$(echo "$action" | jq_py '(lambda rows: rows[0].get("owner_state", "") if rows else "")([r for r in (d.get("items") or d.get("rows") or []) if r.get("batch_id") == __import__("os").environ["GOATOS_ASSERT_BATCH_ID"]])')"
  action_operator="$(echo "$action" | jq_py '(lambda rows: ((rows[0].get("owner") or {}).get("operator_name") or (rows[0].get("owner") or {}).get("operatorName") or "") if rows else "")([r for r in (d.get("items") or d.get("rows") or []) if r.get("batch_id") == __import__("os").environ["GOATOS_ASSERT_BATCH_ID"]])')"
  action_next="$(echo "$action" | jq_py '(lambda rows: rows[0].get("next_action", "") if rows else "")([r for r in (d.get("items") or d.get("rows") or []) if r.get("batch_id") == __import__("os").environ["GOATOS_ASSERT_BATCH_ID"]])')"
  [ "$action_state" != "blocked" ] || fail "open vaccination action-center batch $batch_id regressed to blocked"
  [ "$action_owner_state" = "assigned" ] || fail "open vaccination action-center batch $batch_id owner_state=$action_owner_state, want assigned"
  [ -n "$action_operator" ] || fail "open vaccination action-center batch $batch_id has no seeded operator"
  case "$action_next" in
    *"Assign operator"*|*"Assign owner"*) fail "open vaccination action-center batch $batch_id still routes to owner assignment: $action_next" ;;
  esac

  echo "vaccination owner-chain guard: batch=$batch_id due_before=$due_before execution=$execution_state operator=$execution_operator action=$action_state next=$action_next"
}

seed_procurement_warmup_load() {
  local stamp="$1"
  local source_party_id="00000000-0000-4000-8000-000000001101"
  local purchase_date
  purchase_date="$(date -u +%F)"

  local create
  create="$(
    curl -sS \
      -H "Authorization: Bearer $GOATOS_BEARER_TOKEN" \
      -H "Idempotency-Key: smoke-proc-load-$stamp" \
      -H "Content-Type: application/json" \
      -X POST "$GOATOS_API_BASE_URL/procurement/source-entry/loads" \
      -d @- <<JSON
{"source_party_id":"$source_party_id","source_location_id":null,"expected_count":1,"purchase_date":"$purchase_date","planned_dispatch_at":null,"notes":"admin-web smoke supplier warmup seed","context":{"seed":"admin-web-e2e-smoke","run_id":"$run_id"}}
JSON
  )"
  local load_id
  load_id="$(echo "$create" | jq_py 'd["load"]["load_id"]')"
  if [ -z "$load_id" ]; then
    echo "procurement visual load seed failed: $create" >&2
    return 1
  fi

  local add
  add="$(
    curl -sS \
      -H "Authorization: Bearer $GOATOS_BEARER_TOKEN" \
      -H "Idempotency-Key: smoke-proc-goat-$stamp" \
      -H "Content-Type: application/json" \
      -X POST "$GOATOS_API_BASE_URL/procurement/source-entry/loads/$load_id/goats" \
      -d @- <<JSON
{"animal_identifier_1":"SMOKE-PROC-$stamp-A1","animal_identifier_2":"SMOKE-PROC-$stamp-A2","species":"goat","sex":"female","selection_state":"candidate","selection_reason":"admin-web smoke supplier warmup seed","purpose":"breeding","current_state":"source_warmup","source_entry_state":"pending","ownership_state":"pending","health_state":"pending","warmup_days":45,"holding_location_id":null,"proof_refs":[],"metadata":{"seed":"admin-web-e2e-smoke","run_id":"$run_id"}}
JSON
  )"
  local goat_id
  goat_id="$(echo "$add" | jq_py 'd["goat"]["goat_id"]')"
  if [ -z "$goat_id" ]; then
    echo "procurement visual goat seed failed: $add" >&2
    return 1
  fi

  echo "procurement visual load=$load_id goat=$goat_id"
}

echo "### data-plane: vaccination chain proof"
bash "$repo_root/tools/dev/vaccination-chain-proof.sh" | tee "$report_dir/vaccination-chain-proof.log"

echo "### data-plane: procurement vaccination matrix"
bash "$repo_root/tools/dev/procurement-vaccination-e2e-matrix.sh" | tee "$report_dir/procurement-vaccination-e2e-matrix.log"

echo "### seed: open vaccination work for browser click coverage"
seed_open_vaccination_goat "$(date +%s)" > >(tee "$report_dir/open-visual-goat.log")

echo "### guard: open vaccination work has seeded owner chain"
assert_open_vaccination_owner_chain_ready | tee "$report_dir/open-vaccination-owner-chain.log"

echo "### seed: procurement supplier warmup row for browser click coverage"
seed_procurement_warmup_load "$(date +%s)" > >(tee "$report_dir/procurement-warmup.log")

echo "### frontend: live visual/click smoke"
npm --prefix "$repo_root/apps/admin-web" run smoke:visual:live | tee "$report_dir/smoke-visual-live.log"

echo "## admin-web-e2e-smoke closed run_id=$run_id"
