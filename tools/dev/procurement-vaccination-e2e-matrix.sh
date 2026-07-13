#!/usr/bin/env bash
# Local procurement -> vaccination matrix proof.
#
# Drives the real local API for the four non-negotiable source-entry cases:
#   1. clean accepted goat reaches PC handoff and vaccination obligation generation
#   2. pre-truck rejected goat never reaches PC/vaccination
#   3. blocked goat cannot be accepted and never reaches PC/vaccination
#   4. extra unknown arrival row is recorded but never reaches PC/vaccination
set -euo pipefail
export PATH="/opt/homebrew/opt/postgresql@15/bin:$PATH"

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
. "$repo_root/tools/dev/e2e-db-env.sh"
goatos_e2e_require_isolated_database "tools/dev/procurement-vaccination-e2e-matrix.sh"
backend_dir="$repo_root/backend"

export GOATOS_ENV="${GOATOS_ENV:-local}"
export GOATOS_AUTH_MODE="${GOATOS_AUTH_MODE:-bearer}"
export GOATOS_AUTH_ISSUER="${GOATOS_AUTH_ISSUER:-goatos-local}"
export GOATOS_AUTH_AUDIENCE="${GOATOS_AUTH_AUDIENCE:-goatos-api}"
export GOATOS_AUTH_HS256_SECRET="${GOATOS_AUTH_HS256_SECRET:-goatos-local-dev-secret-32-bytes-min}"
export GOATOS_AUTH_MAX_TOKEN_TTL="${GOATOS_AUTH_MAX_TOKEN_TTL:-24h}"
export GOATOS_TENANT_ID="${GOATOS_TENANT_ID:-00000000-0000-4000-8000-000000000001}"
export GOATOS_LOCAL_USER_ID="${GOATOS_LOCAL_USER_ID:-90000000-0000-4000-8000-000000000101}"
export GOATOS_API_BASE_URL="${GOATOS_API_BASE_URL:-http://127.0.0.1:8080}"

TENANT="$GOATOS_TENANT_ID"
USER="$GOATOS_LOCAL_USER_ID"
API="${GOATOS_API_BASE_URL%/}"
PGURL="$DATABASE_URL"

SOURCE_PARTY="00000000-0000-4000-8000-000000001101"
PARK="00000000-0000-4000-8000-000000003001"
SHED="00000000-0000-4000-8000-000000003101"

psqlq() { psql "$PGURL" -tAc "$1"; }
jqp() { python3 -c "import sys,json; d=json.load(sys.stdin); print($1)" 2>/dev/null; }
fail() { echo "FAIL $*" >&2; exit 1; }
. "$repo_root/tools/dev/vaccination-active-fixture.sh"

relay_once() {
  local label="$1"
  local limit="${2:-100}"
  local out
  out="$(
    cd "$backend_dir"
    GOATOS_OUTBOX_ALLOW_NONDURABLE=1 GOATOS_OUTBOX_PUBLISHER=eventbus \
      go run ./cmd/outbox-relay -limit "$limit" 2>&1
  )"
  echo "$out" | tail -1
}

relay_until_published() {
  local label="$1"
  local aggregate="$2"
  local event_type="$3"
  local i status last_error
  for i in $(seq 1 5); do
    relay_once "$label" 500
    status="$(psqlq "select status from outbox_messages where tenant_id='$TENANT' and aggregate_id='$aggregate' and event_type='$event_type' order by created_at desc limit 1")"
    if [ "$status" = "published" ]; then
      return 0
    fi
    if [ "$status" = "failed" ] || [ "$status" = "dead_letter" ]; then
      last_error="$(psqlq "select coalesce(last_error,'') from outbox_messages where tenant_id='$TENANT' and aggregate_id='$aggregate' and event_type='$event_type' order by created_at desc limit 1")"
      fail "$label target $event_type for aggregate=$aggregate reached status=$status last_error=$last_error"
    fi
    sleep 1
  done
  fail "$label did not publish $event_type for aggregate=$aggregate; last_status=$status"
}

TOKEN="$(cd "$backend_dir" && go run ./cmd/mint-dev-token -tenant-id "$TENANT" -user-id "$USER" -ttl 2h 2>/dev/null)"
AUTH=(-H "Authorization: Bearer $TOKEN")

api_post() {
  local path="$1"
  local idempotency_key="$2"
  local body="$3"
  local args=(-sS -w $'\n%{http_code}' "${AUTH[@]}" -H "Content-Type: application/json")
  if [ -n "$idempotency_key" ]; then
    args+=(-H "Idempotency-Key: $idempotency_key")
  fi
  local response status payload
  response="$(curl "${args[@]}" -X POST "$API$path" -d "$body")"
  status="${response##*$'\n'}"
  payload="${response%$'\n'*}"
  if [[ "$status" != 2* ]]; then
    fail "POST $path status=$status body=$payload"
  fi
  printf '%s' "$payload"
}

api_post_expect_status() {
  local path="$1"
  local idempotency_key="$2"
  local body="$3"
  local want="$4"
  local args=(-sS -w $'\n%{http_code}' "${AUTH[@]}" -H "Content-Type: application/json")
  if [ -n "$idempotency_key" ]; then
    args+=(-H "Idempotency-Key: $idempotency_key")
  fi
  local response status payload
  response="$(curl "${args[@]}" -X POST "$API$path" -d "$body")"
  status="${response##*$'\n'}"
  payload="${response%$'\n'*}"
  if [ "$status" != "$want" ]; then
    fail "POST $path status=$status want=$want body=$payload"
  fi
  printf '%s' "$payload"
}

expect_count() {
  local label="$1"
  local sql="$2"
  local want="$3"
  local got
  got="$(psqlq "$sql")"
  if [ "$got" != "$want" ]; then
    fail "$label count=$got want=$want"
  fi
  echo "$label=$got"
}

mk_proof() {
  local stamp="$1"
  local create proof_id upload_url full_url
  create="$(api_post "/app/proofs/uploads" "" "$(cat <<JSON
{"proof_type":"attachment","mime_type":"text/plain","scope_type":"tenant","scope_id":"$TENANT","subject_type":"other","metadata":{"seed":"procurement-vaccination-e2e-matrix","stamp":"$stamp"}}
JSON
)")"
  proof_id="$(echo "$create" | jqp 'd["proof"]["proof_id"]')"
  upload_url="$(echo "$create" | jqp 'd["upload_url"]')"
  [ -n "$proof_id" ] || fail "proof create returned no proof_id: $create"
  case "$upload_url" in
    http*) full_url="$upload_url" ;;
    /*) full_url="$API$upload_url" ;;
    *) full_url="$API/$upload_url" ;;
  esac
  printf 'procurement-vaccination-matrix-%s' "$stamp" >"/tmp/goatos-proc-matrix-$stamp.txt"
  curl -fsS "${AUTH[@]}" -H "Content-Type: text/plain" -X PUT \
    --data-binary @"/tmp/goatos-proc-matrix-$stamp.txt" "$full_url" >/dev/null
  echo "$proof_id"
}

echo "## procurement-vaccination-e2e-matrix api=$API"

echo "### readiness"
curl -fsS "$API/readyz" >/dev/null

echo "### seed: local grant and vaccination trigger"
(
  cd "$backend_dir"
  go run ./cmd/seed-dev-grant -tenant-id "$TENANT" -user-id "$USER" -role ceo_internal >/dev/null
  go run ./cmd/seed-vaccination-trigger >/dev/null
)
resolve_vaccination_fixture
echo "vaccination_fixture=$MATRIX_SUMMARY primary=$RULE_SUMMARY item=$VACCINE_ITEM"

STAMP="${GOATOS_E2E_RUN_ID:-MATRIX-$(date +%Y%m%d-%H%M%S)}-$(date +%s)"
BUSINESS_DATE="$(date -u +%F)"
if ENTRY_DATE="$(date -u -v-8d +%F 2>/dev/null)"; then
  :
else
  ENTRY_DATE="$(date -u -d "$BUSINESS_DATE - 8 days" +%F)"
fi
if DOB_DAY28="$(date -u -v-28d +%F 2>/dev/null)"; then
  :
else
  DOB_DAY28="$(date -u -d "$BUSINESS_DATE - 28 days" +%F)"
fi

echo "### create procurement load"
LOAD_BODY="$(cat <<JSON
{"source_party_id":"$SOURCE_PARTY","source_location_id":null,"expected_count":3,"purchase_date":"$ENTRY_DATE","planned_dispatch_at":null,"notes":"procurement vaccination e2e matrix $STAMP","context":{"seed":"procurement-vaccination-e2e-matrix","stamp":"$STAMP"}}
JSON
)"
LOAD_JSON="$(api_post "/procurement/source-entry/loads" "matrix-load-$STAMP" "$LOAD_BODY")"
LOAD="$(echo "$LOAD_JSON" | jqp 'd["load"]["load_id"]')"
[ -n "$LOAD" ] || fail "create load returned no load_id: $LOAD_JSON"

add_goat() {
  local suffix="$1"
  local ownership="$2"
  local metadata="$3"
  local add_json goat_id
  add_json="$(api_post "/procurement/source-entry/loads/$LOAD/goats" "matrix-goat-$suffix-$STAMP" "$(cat <<JSON
{"animal_identifier_1":"MATRIX-$suffix-$STAMP-A1","animal_identifier_2":"MATRIX-$suffix-$STAMP-A2","species":"goat","sex":"female","selection_state":"candidate","selection_reason":"procurement vaccination matrix","purpose":"breeding","current_state":"source_candidate","source_entry_state":"accepted","ownership_state":"$ownership","health_state":"pending","proof_refs":[],"metadata":$metadata}
JSON
)")"
  goat_id="$(echo "$add_json" | jqp 'd["goat"]["goat_id"]')"
  [ -n "$goat_id" ] || fail "add goat $suffix returned no goat_id: $add_json"
  echo "$goat_id"
}

CLEAN="$(add_goat "CLEAN" "mesha_owned" "{\"sex\":\"female\",\"dob\":\"$DOB_DAY28\",\"dob_estimated\":true,\"management_stage\":\"K2\",\"matrix_case\":\"clean_accepted\"}")"
REJECTED="$(add_goat "REJECTED" "mesha_owned" "{\"sex\":\"female\",\"dob\":\"$DOB_DAY28\",\"dob_estimated\":true,\"management_stage\":\"K2\",\"matrix_case\":\"rejected_before_truck\"}")"
BLOCKED="$(add_goat "OWNERMISS" "pending" "{\"sex\":\"female\",\"dob\":\"$DOB_DAY28\",\"dob_estimated\":true,\"management_stage\":\"K2\",\"matrix_case\":\"blocked\"}")"
EXTRA_ANIMAL_ID_2="MATRIX-EXTRA-$STAMP-A2"

echo "load=$LOAD clean=$CLEAN rejected=$REJECTED blocked=$BLOCKED extra_animal_identifier_2=$EXTRA_ANIMAL_ID_2"

source_health_passed() {
  local goat_id="$1"
  local suffix="$2"
  api_post "/procurement/source-entry/goats/$goat_id/source-health" "matrix-health-$suffix-$STAMP" "$(cat <<JSON
{"load_id":"$LOAD","health_state":"passed","reason":"matrix source health passed","checked_at":"${ENTRY_DATE}T08:00:00Z"}
JSON
)" >/dev/null
}

pre_dispatch_decision() {
  local goat_id="$1"
  local suffix="$2"
  local decision="$3"
  local reason="$4"
  api_post "/procurement/source-entry/goats/$goat_id/pre-dispatch-decision" "matrix-decision-$suffix-$STAMP" "$(cat <<JSON
{"load_id":"$LOAD","decision_type":"$decision","reason":"$reason","decided_at":"${ENTRY_DATE}T09:00:00Z","metadata":{"seed":"procurement-vaccination-e2e-matrix","stamp":"$STAMP"}}
JSON
)" >/dev/null
}

echo "### source health and pre-dispatch decisions"
source_health_passed "$CLEAN" "clean"
source_health_passed "$REJECTED" "rejected"
source_health_passed "$BLOCKED" "blocked"
pre_dispatch_decision "$CLEAN" "clean" "accepted" "clean accepted before truck loading"
pre_dispatch_decision "$REJECTED" "rejected" "rejected" "rejected before truck loading"
api_post_expect_status "/procurement/source-entry/goats/$BLOCKED/pre-dispatch-decision" \
  "matrix-owner-accept-fail-$STAMP" \
  "$(cat <<JSON
{"load_id":"$LOAD","decision_type":"accepted","reason":"blocked must not be accepted","decided_at":"${ENTRY_DATE}T09:05:00Z","metadata":{"seed":"procurement-vaccination-e2e-matrix","stamp":"$STAMP"}}
JSON
)" "400" >/dev/null
pre_dispatch_decision "$BLOCKED" "blocked" "blocked" "ownership missing"

echo "### dispatch accepted goat with proof"
DISPATCH_PROOF="$(mk_proof "$STAMP-dispatch")"
api_post "/procurement/source-entry/loads/$LOAD/dispatch" "matrix-dispatch-$STAMP" "$(cat <<JSON
{"to_location_id":"$PARK","goat_ids":["$CLEAN"],"dispatched_at":"${ENTRY_DATE}T10:00:00Z","proof_ref_id":"$DISPATCH_PROOF"}
JSON
)" >/dev/null

echo "### arrival review with one accepted goat and one extra unknown"
api_post "/procurement/source-entry/loads/$LOAD/arrival-review" "matrix-arrival-$STAMP" "$(cat <<JSON
{"park_location_id":"$PARK","expected_count":3,"loaded_count":1,"arrived_count":2,"matched_count":1,"missing_count":0,"extra_count":1,"rejected_count":0,"health_flags":[],"weight_flags":[],"status":"accepted","reviewed_at":"${ENTRY_DATE}T11:00:00Z","goats":[{"goat_id":"$CLEAN","arrival_state":"accepted","notes":"clean matrix goat arrived"},{"animal_identifier_2":"$EXTRA_ANIMAL_ID_2","arrival_state":"extra_unresolved","notes":"extra unknown arrival matrix row"}]}
JSON
)" >/dev/null

echo "### accept clean goat into herd"
api_post "/procurement/source-entry/loads/$LOAD/accept-intake" "matrix-intake-$STAMP" "$(cat <<JSON
{"goat_ids":["$CLEAN"],"park_location_id":"$PARK","shed_location_id":"$SHED","accepted_at":"${ENTRY_DATE}T12:00:00Z","entry_date":"$ENTRY_DATE","trusted_vaccination_history":[],"intake_health_signal":"clear"}
JSON
)" >/dev/null

echo "### assertions: procurement boundary"
expect_count "clean_pc_handoff" "select count(*) from procurement_pc_handoffs where tenant_id='$TENANT' and load_id='$LOAD' and goat_id='$CLEAN'" "1"
expect_count "rejected_pc_handoff" "select count(*) from procurement_pc_handoffs where tenant_id='$TENANT' and load_id='$LOAD' and goat_id='$REJECTED'" "0"
expect_count "blocked_pc_handoff" "select count(*) from procurement_pc_handoffs where tenant_id='$TENANT' and load_id='$LOAD' and goat_id='$BLOCKED'" "0"
expect_count "extra_unknown_arrival_row" "select count(*) from arrival_intake_review_goats where tenant_id='$TENANT' and load_id='$LOAD' and animal_identifier_2='$EXTRA_ANIMAL_ID_2' and goat_id is null and arrival_state='extra_unresolved'" "1"
expect_count "clean_goat_created_outbox" "select count(*) from outbox_messages where tenant_id='$TENANT' and event_type='goat.created' and aggregate_id='$CLEAN'" "1"
expect_count "rejected_goat_created_outbox" "select count(*) from outbox_messages where tenant_id='$TENANT' and event_type='goat.created' and aggregate_id='$REJECTED'" "0"
expect_count "blocked_goat_created_outbox" "select count(*) from outbox_messages where tenant_id='$TENANT' and event_type='goat.created' and aggregate_id='$BLOCKED'" "0"

CANONICAL_STATE="$(psqlq "select sex || ':' || to_char(dob, 'YYYY-MM-DD') || ':' || dob_estimated::text || ':' || management_stage || ':' || health_status || ':' || lifecycle_status || ':' || park_id::text || ':' || shed_id::text from goats where tenant_id='$TENANT' and goat_id='$CLEAN'")"
EXPECTED_STATE="female:$DOB_DAY28:true:K2:healthy:alive:$PARK:$SHED"
[ "$CANONICAL_STATE" = "$EXPECTED_STATE" ] || fail "clean goat canonical state=$CANONICAL_STATE want=$EXPECTED_STATE"
echo "clean_canonical_state=$CANONICAL_STATE"

echo "### relay goat.created and generate vaccination work"
relay_until_published "clean goat.created delivery" "$CLEAN" "goat.created"
OBLIGATION="$(psqlq "select obligation_id from obligation_instances where tenant_id='$TENANT' and target_id='$CLEAN' and protocol_version_id='$VERSION' and rule_id='$RULE' limit 1")"
[ -n "$OBLIGATION" ] || fail "clean goat produced no vaccination obligation"
INITIAL_OBLIGATION_STATE="$(psqlq "select status || ':' || (batch_id is null)::text from obligation_instances where obligation_id='$OBLIGATION'")"
[ "$INITIAL_OBLIGATION_STATE" = "deferred:true" ] || fail "clean goat initial vaccination state=$INITIAL_OBLIGATION_STATE want deferred:true"
expect_count "clean_vaccination_warmup_hold_event" "select count(*) from obligation_status_events where tenant_id='$TENANT' and obligation_id='$OBLIGATION' and event_type='deferred' and payload->>'defer_status'='warming_hold'" "1"
RECOVERY_AS_OF="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
(
  cd "$backend_dir"
  GOATOS_TENANT_ID="$TENANT" \
    go run ./cmd/generate-vaccination-obligations \
      -tenant-id "$TENANT" \
      -version-id "$VERSION" \
      -unsafe-version-id-bypass-effective-resolution \
      -as-of "$RECOVERY_AS_OF"
)
RECOVERED_OBLIGATION_STATE="$(psqlq "select status || ':' || (batch_id is null)::text from obligation_instances where obligation_id='$OBLIGATION'")"
case "$RECOVERED_OBLIGATION_STATE" in
  scheduled:true|due:true) ;;
  *) fail "clean goat recovered vaccination state=$RECOVERED_OBLIGATION_STATE want scheduled:true or due:true" ;;
esac
DUE_BEFORE="$(psqlq "select to_char((due_at + interval '1 hour') at time zone 'UTC', 'YYYY-MM-DD\"T\"HH24:MI:SS\"Z\"') from obligation_instances where obligation_id='$OBLIGATION'")"
(
  cd "$backend_dir"
  GOATOS_TENANT_ID="$TENANT" \
    go run ./cmd/obligation-sweeper \
      -tenant-id "$TENANT" \
      -version-id "$VERSION" \
      -sop-version-id "$SOP_VERSION" \
      -vaccine-item-id "$VACCINE_ITEM" \
      -actor-id "$USER" \
      -due-before "$DUE_BEFORE" >/dev/null
)
expect_count "clean_goat_created_outbox_published" "select count(*) from outbox_messages where tenant_id='$TENANT' and event_type='goat.created' and aggregate_id='$CLEAN' and status='published'" "1"
expect_count "clean_goat_created_outbox_failed" "select count(*) from outbox_messages where tenant_id='$TENANT' and event_type='goat.created' and aggregate_id='$CLEAN' and status in ('failed','dead_letter')" "0"

BATCH_ROW="$(psqlq "
SELECT concat_ws('|', oi.obligation_id::text, oi.rule_id::text, pr.dose_code, oi.batch_id::text, ob.sop_task_id::text)
FROM obligation_instances oi
JOIN protocol_rules pr
  ON pr.tenant_id = oi.tenant_id
 AND pr.rule_id = oi.rule_id
JOIN obligation_batches ob
  ON ob.tenant_id = oi.tenant_id
 AND ob.batch_id = oi.batch_id
WHERE oi.tenant_id = '$TENANT'
  AND oi.target_id = '$CLEAN'
  AND oi.protocol_version_id = '$VERSION'
  AND oi.batch_id IS NOT NULL
  AND ob.sop_task_id IS NOT NULL
ORDER BY pr.sequence
LIMIT 1
")"
[ -n "$BATCH_ROW" ] || fail "clean goat recovered vaccination work did not reach any batch/task"
IFS='|' read -r BATCH_OBLIGATION BATCH_RULE BATCH_DOSE BATCH TASK <<<"$BATCH_ROW"
[ -n "$BATCH" ] && [ -n "$TASK" ] || fail "clean goat batch row incomplete: $BATCH_ROW"
expect_count "clean_vaccination_obligation" "select count(*) from obligation_instances where tenant_id='$TENANT' and target_id='$CLEAN' and protocol_version_id='$VERSION' and rule_id='$RULE'" "1"
expect_count "rejected_vaccination_obligation" "select count(*) from obligation_instances where tenant_id='$TENANT' and target_id='$REJECTED' and protocol_version_id='$VERSION'" "0"
expect_count "blocked_vaccination_obligation" "select count(*) from obligation_instances where tenant_id='$TENANT' and target_id='$BLOCKED' and protocol_version_id='$VERSION'" "0"

echo "## CLOSED procurement-vaccination-e2e-matrix load=$LOAD clean=$CLEAN rejected=$REJECTED blocked=$BLOCKED extra_animal_identifier_2=$EXTRA_ANIMAL_ID_2 primary_obligation=$OBLIGATION batched_obligation=$BATCH_OBLIGATION batched_dose=$BATCH_DOSE batch=$BATCH task=$TASK"
