#!/usr/bin/env bash
# Local procurement -> vaccination matrix proof.
#
# Drives the real local API for the four non-negotiable source-entry cases:
#   1. clean accepted goat reaches PHC handoff and vaccination obligation generation
#   2. pre-truck rejected goat never reaches PHC/vaccination
#   3. owner-missing goat cannot be accepted and never reaches PHC/vaccination
#   4. extra unknown arrival row is recorded but never reaches PHC/vaccination
set -euo pipefail
export PATH="/opt/homebrew/opt/postgresql@15/bin:$PATH"

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
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
export DATABASE_URL="${DATABASE_URL:-postgres://postgres:goatos@127.0.0.1:55432/goatos?sslmode=disable}"

TENANT="$GOATOS_TENANT_ID"
USER="$GOATOS_LOCAL_USER_ID"
API="${GOATOS_API_BASE_URL%/}"
PGURL="$DATABASE_URL"

SOURCE_PARTY="00000000-0000-4000-8000-000000001101"
PARK="00000000-0000-4000-8000-000000003001"
SHED="00000000-0000-4000-8000-000000003101"
VERSION="00000000-0000-4000-8000-00000000b011"
SOP_VERSION="b0000000-0000-4000-8000-000000000002"
VACCINE_ITEM="00000000-0000-4000-8000-00000000b001"
RULE="00000000-0000-4000-8000-00000000b012"

psqlq() { psql "$PGURL" -tAc "$1"; }
jqp() { python3 -c "import sys,json; d=json.load(sys.stdin); print($1)" 2>/dev/null; }
fail() { echo "FAIL $*" >&2; exit 1; }

relay_once() {
  local label="$1"
  local limit="${2:-100}"
  local out relay_bad
  out="$(
    cd "$backend_dir"
    GOATOS_OUTBOX_ALLOW_NONDURABLE=1 GOATOS_OUTBOX_PUBLISHER=eventbus \
      go run ./cmd/outbox-relay -limit "$limit" 2>&1
  )"
  echo "$out" | tail -1
  relay_bad="$(RELAY_OUTPUT="$out" python3 - <<'PY'
import json
import os
import re

bad = []
for line in os.environ.get("RELAY_OUTPUT", "").splitlines():
    try:
        obj = json.loads(line)
    except Exception:
        obj = {}
    for key in ("failed", "dead_letter", "retry_scheduled"):
        value = obj.get(key)
        if isinstance(value, int) and value > 0:
            bad.append(f"{key}={value}")
    for match in re.finditer(r'"?(failed|dead_letter|retry_scheduled)"?\s*[:=]\s*([1-9][0-9]*)', line):
        bad.append(f"{match.group(1)}={match.group(2)}")
print(" ".join(dict.fromkeys(bad)))
PY
)"
  [ -z "$relay_bad" ] || fail "$label relay reported non-green counts: $relay_bad output=$(echo "$out" | tail -3 | tr '\n' ' ')"
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

STAMP="${GOATOS_E2E_RUN_ID:-MATRIX-$(date +%Y%m%d-%H%M%S)}-$(date +%s)"
ENTRY_DATE="$(date -u +%F)"
if DOB_DAY21="$(date -u -v-21d +%F 2>/dev/null)"; then
  :
else
  DOB_DAY21="$(date -u -d "$ENTRY_DATE - 21 days" +%F)"
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
{"source_tag":"MATRIX-$suffix-$STAMP","source_rfid":"MATRIX-RFID-$suffix-$STAMP","temporary_id":"MATRIX-TEMP-$suffix-$STAMP","selection_state":"candidate","selection_reason":"procurement vaccination matrix","purpose":"breeding","current_state":"source_candidate","identity_review_state":"clean","ownership_state":"$ownership","health_state":"pending","proof_refs":[],"metadata":$metadata}
JSON
)")"
  goat_id="$(echo "$add_json" | jqp 'd["goat"]["goat_id"]')"
  [ -n "$goat_id" ] || fail "add goat $suffix returned no goat_id: $add_json"
  echo "$goat_id"
}

CLEAN="$(add_goat "CLEAN" "mesha_owned" "{\"sex\":\"female\",\"dob\":\"$DOB_DAY21\",\"dob_estimated\":true,\"management_stage\":\"K1\",\"matrix_case\":\"clean_accepted\"}")"
REJECTED="$(add_goat "REJECTED" "mesha_owned" "{\"sex\":\"female\",\"dob\":\"$DOB_DAY21\",\"dob_estimated\":true,\"management_stage\":\"K1\",\"matrix_case\":\"rejected_before_truck\"}")"
OWNER_MISSING="$(add_goat "OWNERMISS" "pending" "{\"sex\":\"female\",\"dob\":\"$DOB_DAY21\",\"dob_estimated\":true,\"management_stage\":\"K1\",\"matrix_case\":\"owner_missing\"}")"
EXTRA_TEMP="MATRIX-EXTRA-$STAMP"

echo "load=$LOAD clean=$CLEAN rejected=$REJECTED owner_missing=$OWNER_MISSING extra_temporary_id=$EXTRA_TEMP"

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
source_health_passed "$OWNER_MISSING" "owner-missing"
pre_dispatch_decision "$CLEAN" "clean" "accepted" "clean accepted before truck loading"
pre_dispatch_decision "$REJECTED" "rejected" "rejected" "rejected before truck loading"
api_post_expect_status "/procurement/source-entry/goats/$OWNER_MISSING/pre-dispatch-decision" \
  "matrix-owner-accept-fail-$STAMP" \
  "$(cat <<JSON
{"load_id":"$LOAD","decision_type":"accepted","reason":"owner missing must not be accepted","decided_at":"${ENTRY_DATE}T09:05:00Z","metadata":{"seed":"procurement-vaccination-e2e-matrix","stamp":"$STAMP"}}
JSON
)" "400" >/dev/null
pre_dispatch_decision "$OWNER_MISSING" "owner-missing" "blocked" "ownership missing"

echo "### dispatch accepted goat with proof"
DISPATCH_PROOF="$(mk_proof "$STAMP-dispatch")"
api_post "/procurement/source-entry/loads/$LOAD/dispatch" "matrix-dispatch-$STAMP" "$(cat <<JSON
{"to_location_id":"$PARK","goat_ids":["$CLEAN"],"dispatched_at":"${ENTRY_DATE}T10:00:00Z","proof_ref_id":"$DISPATCH_PROOF"}
JSON
)" >/dev/null

echo "### arrival review with one accepted goat and one extra unknown"
api_post "/procurement/source-entry/loads/$LOAD/arrival-review" "matrix-arrival-$STAMP" "$(cat <<JSON
{"park_location_id":"$PARK","expected_count":3,"loaded_count":1,"arrived_count":2,"matched_count":1,"missing_count":0,"extra_count":1,"rejected_count":0,"health_flags":[],"weight_flags":[],"status":"accepted","reviewed_at":"${ENTRY_DATE}T11:00:00Z","goats":[{"goat_id":"$CLEAN","arrival_state":"accepted","notes":"clean matrix goat arrived"},{"temporary_id":"$EXTRA_TEMP","arrival_state":"extra_unresolved","notes":"extra unknown arrival matrix row"}]}
JSON
)" >/dev/null

echo "### accept clean goat into herd"
api_post "/procurement/source-entry/loads/$LOAD/accept-intake" "matrix-intake-$STAMP" "$(cat <<JSON
{"goat_ids":["$CLEAN"],"park_location_id":"$PARK","shed_location_id":"$SHED","accepted_at":"${ENTRY_DATE}T12:00:00Z","entry_date":"$ENTRY_DATE","trusted_vaccination_history":[],"intake_health_signal":"clear"}
JSON
)" >/dev/null

echo "### assertions: procurement boundary"
expect_count "clean_phc_handoff" "select count(*) from procurement_phc_handoffs where tenant_id='$TENANT' and load_id='$LOAD' and goat_id='$CLEAN'" "1"
expect_count "rejected_phc_handoff" "select count(*) from procurement_phc_handoffs where tenant_id='$TENANT' and load_id='$LOAD' and goat_id='$REJECTED'" "0"
expect_count "owner_missing_phc_handoff" "select count(*) from procurement_phc_handoffs where tenant_id='$TENANT' and load_id='$LOAD' and goat_id='$OWNER_MISSING'" "0"
expect_count "extra_unknown_arrival_row" "select count(*) from arrival_intake_review_goats where tenant_id='$TENANT' and load_id='$LOAD' and temporary_id='$EXTRA_TEMP' and goat_id is null and arrival_state='extra_unresolved'" "1"
expect_count "clean_goat_created_outbox" "select count(*) from outbox_messages where tenant_id='$TENANT' and event_type='goat.created' and aggregate_id='$CLEAN'" "1"
expect_count "rejected_goat_created_outbox" "select count(*) from outbox_messages where tenant_id='$TENANT' and event_type='goat.created' and aggregate_id='$REJECTED'" "0"
expect_count "owner_missing_goat_created_outbox" "select count(*) from outbox_messages where tenant_id='$TENANT' and event_type='goat.created' and aggregate_id='$OWNER_MISSING'" "0"

CANONICAL_STATE="$(psqlq "select sex || ':' || to_char(dob, 'YYYY-MM-DD') || ':' || dob_estimated::text || ':' || management_stage || ':' || health_status || ':' || lifecycle_status || ':' || park_id::text || ':' || shed_id::text from goats where tenant_id='$TENANT' and goat_id='$CLEAN'")"
EXPECTED_STATE="female:$DOB_DAY21:true:K1:healthy:alive:$PARK:$SHED"
[ "$CANONICAL_STATE" = "$EXPECTED_STATE" ] || fail "clean goat canonical state=$CANONICAL_STATE want=$EXPECTED_STATE"
echo "clean_canonical_state=$CANONICAL_STATE"

echo "### relay goat.created and generate vaccination work"
relay_once "clean goat.created delivery" 100
(
  cd "$backend_dir"
  GOATOS_TENANT_ID="$TENANT" \
    go run ./cmd/obligation-sweeper \
      -tenant-id "$TENANT" \
      -version-id "$VERSION" \
      -sop-version-id "$SOP_VERSION" \
      -vaccine-item-id "$VACCINE_ITEM" \
      -actor-id "$USER" >/dev/null
)
expect_count "clean_goat_created_outbox_published" "select count(*) from outbox_messages where tenant_id='$TENANT' and event_type='goat.created' and aggregate_id='$CLEAN' and status='published'" "1"
expect_count "clean_goat_created_outbox_failed" "select count(*) from outbox_messages where tenant_id='$TENANT' and event_type='goat.created' and aggregate_id='$CLEAN' and status in ('failed','dead_letter')" "0"

OBLIGATION="$(psqlq "select obligation_id from obligation_instances where tenant_id='$TENANT' and target_id='$CLEAN' and protocol_version_id='$VERSION' and rule_id='$RULE' limit 1")"
[ -n "$OBLIGATION" ] || fail "clean goat produced no vaccination obligation"
BATCH="$(psqlq "select batch_id from obligation_instances where obligation_id='$OBLIGATION'")"
TASK="$(psqlq "select sop_task_id from obligation_batches where batch_id='$BATCH'")"
[ -n "$TASK" ] || fail "clean goat obligation did not reach batch/task"
expect_count "clean_vaccination_obligation" "select count(*) from obligation_instances where tenant_id='$TENANT' and target_id='$CLEAN' and protocol_version_id='$VERSION' and rule_id='$RULE'" "1"
expect_count "rejected_vaccination_obligation" "select count(*) from obligation_instances where tenant_id='$TENANT' and target_id='$REJECTED' and protocol_version_id='$VERSION' and rule_id='$RULE'" "0"
expect_count "owner_missing_vaccination_obligation" "select count(*) from obligation_instances where tenant_id='$TENANT' and target_id='$OWNER_MISSING' and protocol_version_id='$VERSION' and rule_id='$RULE'" "0"

echo "## CLOSED procurement-vaccination-e2e-matrix load=$LOAD clean=$CLEAN rejected=$REJECTED owner_missing=$OWNER_MISSING extra_temporary_id=$EXTRA_TEMP obligation=$OBLIGATION batch=$BATCH task=$TASK"
