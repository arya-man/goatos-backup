#!/usr/bin/env bash
# Local DATA-PLANE vaccination rework proof.
#
# Proves the V1 negative/repair path on the real local stack:
#   goat.created -> obligation -> sweeper -> SOP task -> proof submission
#   -> verifier requests rework -> completion rejected and obligation remains open
#   -> corrected proof resubmitted -> verifier accepts -> obligation completed
#   -> vaccination.completed outbox -> booster obligation generated
set -euo pipefail
export PATH="/opt/homebrew/opt/postgresql@15/bin:$PATH"
REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
. "$REPO_ROOT/tools/dev/e2e-db-env.sh"
goatos_e2e_require_isolated_database "tools/dev/vaccination-rework-proof.sh"
export GOATOS_ENV=local GOATOS_AUTH_MODE=bearer
export GOATOS_AUTH_ISSUER=goatos-local GOATOS_AUTH_AUDIENCE=goatos-api
export GOATOS_AUTH_HS256_SECRET="${GOATOS_AUTH_HS256_SECRET:-goatos-local-dev-secret-32-bytes-min}" GOATOS_AUTH_MAX_TOKEN_TTL=24h

PGURL="$DATABASE_URL"; API="${GOATOS_API_BASE_URL:-http://127.0.0.1:8080}"
TENANT=00000000-0000-4000-8000-000000000001
USER=90000000-0000-4000-8000-000000000101
BACKEND="$REPO_ROOT/backend"

psqlq(){ psql "$PGURL" -tAc "$1"; }
jqp(){ python3 -c "import sys,json;d=json.load(sys.stdin);print($1)" 2>/dev/null; }
has(){ grep -q "$1" <<<"$2" && echo HIT || echo MISS; }
fail(){ echo "FAIL $*" >&2; exit 1; }
. "$(cd "$(dirname "$0")" && pwd)/vaccination-active-fixture.sh"
install_vaccination_proof_cleanup_trap

relay_once(){
  local out
  out=$(cd "$BACKEND" && GOATOS_OUTBOX_ALLOW_NONDURABLE=1 GOATOS_OUTBOX_PUBLISHER=eventbus go run ./cmd/outbox-relay -limit "${1:-500}" 2>&1)
  if [ "${GOATOS_PROOF_VERBOSE_RELAY:-0}" = "1" ]; then
    echo "$out" | tail -1
  fi
}

relay_until_published(){
  local label=$1 aggregate=$2 event_type=$3
  local i status last_error
  for i in $(seq 1 5); do
    relay_once 500
    status=$(psqlq "select status from outbox_messages where tenant_id='$TENANT' and aggregate_id='$aggregate' and event_type='$event_type' order by created_at desc limit 1")
    if [ "$status" = "published" ]; then
      echo "$label: $event_type published"
      return 0
    fi
    if [ "$status" = "failed" ] || [ "$status" = "dead_letter" ]; then
      last_error=$(psqlq "select coalesce(last_error,'') from outbox_messages where tenant_id='$TENANT' and aggregate_id='$aggregate' and event_type='$event_type' order by created_at desc limit 1")
      fail "$label $event_type aggregate=$aggregate status=$status last_error=$last_error"
    fi
    sleep 1
  done
  fail "$label did not publish $event_type for aggregate=$aggregate; last_status=$status"
}

TOKEN=$(cd "$BACKEND" && go run ./cmd/mint-dev-token -tenant-id "$TENANT" -user-id "$USER" -ttl 2h 2>/dev/null)
A=(-H "Authorization: Bearer $TOKEN")
STAMP=$(date +%s)
RFID="REWORKPROOF-$STAMP"
SHED_CODE="REWORK-MAIN-$STAMP"
ENTRY_DATE=$(date -u +%F)
ADMINISTERED_AT=$(date -u +%Y-%m-%dT%H:%M:%SZ)
PROOF_PARK=00000000-0000-4000-8000-000000003001
if DOB_DAY28=$(date -u -v-28d +%F 2>/dev/null); then
  :
else
  DOB_DAY28=$(date -u -d "$ENTRY_DATE - 28 days" +%F)
fi
GOAT_ENTRY_DATE="$DOB_DAY28"

echo "## vaccination-rework-proof stamp=$STAMP api=$API"

echo; echo "### 0. seed and resolve active vaccination fixture"
( cd "$BACKEND" && go run ./cmd/seed-vaccination-trigger -tenant-id "$TENANT" >/dev/null )
resolve_vaccination_fixture
echo "matrix=$MATRIX_SUMMARY primary=$RULE_SUMMARY booster=$BOOSTER_SUMMARY sop=$SOPVER item=$ITEM lot=$LOT"

SHED=$(psqlq "insert into locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, country, state_region, timezone, status) values (gen_random_uuid(), '$TENANT', 'shed', '$SHED_CODE', 'Rework Proof Main $STAMP', '$PROOF_PARK', 'IN', 'Tamil Nadu', 'Asia/Kolkata', 'active') returning location_id" | head -n 1)

echo; echo "### 1. create goat -> goat.created -> obligation"
CREATE=$(curl -s "${A[@]}" -H "Idempotency-Key: rework-$STAMP" -H "Content-Type: application/json" -X POST "$API/admin/goats" -d @- <<JSON
{"animal_identifier_1":"$RFID-A1","animal_identifier_2":"$RFID-A2","species":"goat","park_id":"$PROOF_PARK","shed_id":"$SHED","sex":"female","breed":"all","dob":"$DOB_DAY28","dob_estimated":true,"origin_type":"birth","entry_date":"$GOAT_ENTRY_DATE","management_stage":"K2","health_status":"healthy","reproductive_status":"open","evidence_refs":[{"evidence_type":"source_record","evidence_id":"rework-$STAMP"}]}
JSON
)
GOAT=$(echo "$CREATE" | jqp 'd["goat"]["goat_id"]')
[ -n "$GOAT" ] || fail "goat create failed: $CREATE"
psqlq "update obligation_instances oi set status='canceled', updated_at=now() from goats g join locations l on l.location_id = g.shed_id where oi.tenant_id='$TENANT' and oi.target_id=g.goat_id and oi.protocol_version_id='$VERSION' and oi.status in ('scheduled','due','deferred','in_progress') and g.goat_id <> '$GOAT' and (l.location_code like 'REWORK-%' or l.location_code like 'CHAIN-%' or l.location_code like 'TRUST-HIST-%')" >/dev/null
relay_until_published "goat.created delivery" "$GOAT" "goat.created"
OBL=$(psqlq "select obligation_id from obligation_instances where target_id='$GOAT' and protocol_version_id='$VERSION' and rule_id='$RULE' limit 1")
[ -n "$OBL" ] || fail "no obligation generated for goat=$GOAT"
echo "GOAT=$GOAT OBLIGATION=$OBL"

echo; echo "### 2. sweeper -> batch + SOP task"
psqlq "update obligation_instances oi set status='canceled', updated_at=now() from goats g join locations l on l.location_id = g.shed_id where oi.tenant_id='$TENANT' and oi.target_id=g.goat_id and oi.protocol_version_id='$VERSION' and oi.status in ('scheduled','due','deferred','in_progress') and g.goat_id <> '$GOAT' and (l.location_code like 'REWORK-%' or l.location_code like 'CHAIN-%' or l.location_code like 'TRUST-HIST-%')" >/dev/null
( cd "$BACKEND" && GOATOS_TENANT_ID=$TENANT go run ./cmd/obligation-sweeper -tenant-id "$TENANT" -version-id "$VERSION" -sop-version-id "$SOPVER" -vaccine-item-id "$ITEM" -actor-id "$USER" 2>&1 | tail -1 )
BATCH=$(psqlq "select batch_id from obligation_instances where obligation_id='$OBL'")
TASK=$(psqlq "select sop_task_id from obligation_batches where batch_id='$BATCH'")
[ -n "$TASK" ] || fail "no SOP task for batch=$BATCH"
echo "BATCH=$BATCH TASK=$TASK"

EXPECTED_WORKFLOW_ROW="batch:$BATCH:rule:$RULE:shed:$SHED"
PP_OPEN=$(curl -s "${A[@]}" "$API/goats/$GOAT/passport")
PASSPORT_WORKFLOW_ROW=$(echo "$PP_OPEN" | python3 -c "import sys,json; d=json.load(sys.stdin); rows=d.get('open_obligations', []); print(next((r.get('workflow_row_id','') for r in rows if r.get('obligation_id') == '$OBL'), ''))")
[ "$PASSPORT_WORKFLOW_ROW" = "$EXPECTED_WORKFLOW_ROW" ] || fail "passport workflow row mismatch got=$PASSPORT_WORKFLOW_ROW want=$EXPECTED_WORKFLOW_ROW passport=$PP_OPEN"
echo "Passport open obligation workflow row: $PASSPORT_WORKFLOW_ROW"

mkproof(){ local phase=$1 subj=$2 subject_id=$3
  local r pid url full
  r=$(curl -s "${A[@]}" -H "Content-Type: application/json" -X POST "$API/app/proofs/uploads" \
    -d "{\"proof_type\":\"video\",\"mime_type\":\"video/mp4\",\"scope_type\":\"task\",\"scope_id\":\"$TASK\",\"subject_type\":\"$subj\",\"subject_id\":\"$subject_id\"}")
  pid=$(echo "$r" | jqp 'd["proof"]["proof_id"]'); url=$(echo "$r" | jqp 'd["upload_url"]')
  [ -n "$pid" ] || fail "proof upload failed phase=$phase subject=$subj response=$r"
  case "$url" in http*) full=$url;; /*) full="$API$url";; *) full="$API/$url";; esac
  printf 'rework-%s-%s-%s' "$phase" "$subj" "$STAMP" >/tmp/rp_${phase}_${subj}.bin
  curl -s "${A[@]}" -H "Content-Type: video/mp4" -X PUT --data-binary @/tmp/rp_${phase}_${subj}.bin "$full" >/dev/null
  echo "$pid"
}

submit_task(){ local phase=$1
  local p_goat sub subid comp
  p_goat=$(mkproof "$phase" goat "$GOAT")
  sub=$(curl -s "${A[@]}" -H "Content-Type: application/json" -X POST "$API/app/tasks/$TASK/submissions" -d @- <<JSON
{"sop_version_id":"$SOPVER","idempotency_key":"sub-$STAMP-$phase","answers":{"vaccine_lot_id":"$LOT","cold_chain_verified":true,"goat_video":"$p_goat","goat_ids":["$GOAT"],"dose_ml_given":2,"route_site":"subcutaneous","administered_at":"$ADMINISTERED_AT","adverse_reaction":false},"proof_refs":[{"proof_id":"$p_goat","proof_type":"video","subject_type":"goat","subject_id":"$GOAT","upload_state":"completed"}]}
JSON
)
  subid=$(echo "$sub" | jqp 'd.get("submission",{}).get("submission_id","")')
  [ -n "$subid" ] || fail "submission failed phase=$phase response=$sub"
  comp=$(psqlq "select completion_id from vaccination_completions where tenant_id='$TENANT' and goat_id='$GOAT' and status='recorded' order by created_at desc limit 1")
  [ -n "$comp" ] || fail "no recorded completion after phase=$phase submission=$subid"
  echo "$subid|$comp"
}

echo; echo "### 3. first submission -> verifier requests rework"
FIRST=$(submit_task first)
SUB1=${FIRST%%|*}; COMP1=${FIRST##*|}
TASK_ROW_VERSION=$(psqlq "select row_version from sop_tasks where tenant_id='$TENANT' and task_id='$TASK'")
REWORK=$(curl -s "${A[@]}" -H "Content-Type: application/json" -X POST "$API/admin/tasks/$TASK/rework" -d "{\"reason\":\"blurry proof - request resubmission\",\"row_version\":$TASK_ROW_VERSION}")
echo "$REWORK" | python3 -c 'import sys,json; d=json.load(sys.stdin); assert d.get("task",{}).get("state") == "rework_requested", d' >/dev/null \
  || fail "rework request did not move task to rework_requested: $REWORK"
REWORK_STATE=$(psqlq "select c.status || ':' || o.status || ':' || (o.completed_at is null)::text from vaccination_completions c join obligation_instances o on o.obligation_id=c.obligation_id where c.completion_id='$COMP1'")
[ "$REWORK_STATE" != "accepted:completed:false" ] || fail "rework incorrectly completed obligation state=$REWORK_STATE"
COMP1_STATUS=$(psqlq "select status from vaccination_completions where completion_id='$COMP1'")
TASK_STATE=$(psqlq "select state from sop_tasks where task_id='$TASK'")
[ "$COMP1_STATUS" = "rejected" ] || fail "first completion status=$COMP1_STATUS want rejected"
[ "$TASK_STATE" = "rework_requested" ] || fail "task state=$TASK_STATE want rework_requested"
echo "REWORKED submission=$SUB1 completion=$COMP1 state=$REWORK_STATE"

echo; echo "### 4. corrected resubmission -> verifier accepts"
SECOND=$(submit_task second)
SUB2=${SECOND%%|*}; COMP2=${SECOND##*|}
[ "$COMP2" != "$COMP1" ] || fail "resubmission reused rejected completion id=$COMP2"
RECORDED_COUNT=$(psqlq "select count(*) from vaccination_completions where tenant_id='$TENANT' and goat_id='$GOAT' and status='recorded'")
[ "$RECORDED_COUNT" = "1" ] || fail "recorded completion count after resubmission=$RECORDED_COUNT"
TASK_STATE=$(psqlq "select state from sop_tasks where task_id='$TASK'")
[ "$TASK_STATE" = "needs_review" ] || fail "task state after resubmission=$TASK_STATE want needs_review"
TASK_ROW_VERSION=$(psqlq "select row_version from sop_tasks where tenant_id='$TENANT' and task_id='$TASK'")
ACCEPT=$(curl -s "${A[@]}" -H "Content-Type: application/json" -X POST "$API/admin/tasks/$TASK/verify" -d "{\"reason\":\"corrected proof accepted\",\"row_version\":$TASK_ROW_VERSION}")
echo "$ACCEPT" | python3 -c 'import sys,json; d=json.load(sys.stdin); assert d.get("task",{}).get("state") == "accepted", d' >/dev/null \
  || fail "verify did not accept corrected proof: $ACCEPT"
FINAL_STATE=$(psqlq "select (select status from vaccination_completions where completion_id='$COMP1') || ':' || (select status from vaccination_completions where completion_id='$COMP2') || ':' || o.status || ':' || (o.completed_at is not null)::text from obligation_instances o where o.obligation_id='$OBL'")
[ "$FINAL_STATE" = "rejected:accepted:completed:true" ] || fail "final state=$FINAL_STATE"
VAX_OUTBOX_COUNT=$(psqlq "select count(*) from outbox_messages where tenant_id='$TENANT' and event_type='vaccination.completed' and aggregate_id='$OBL'")
[ "$VAX_OUTBOX_COUNT" = "1" ] || fail "vaccination.completed outbox count=$VAX_OUTBOX_COUNT"
echo "ACCEPTED corrected_submission=$SUB2 rejected_completion=$COMP1 accepted_completion=$COMP2"

echo; echo "### 5. completed event -> next-dose obligation and replay idempotency"
relay_until_published "vaccination.completed delivery" "$OBL" "vaccination.completed"
BOOSTER_OBL=$(psqlq "select obligation_id from obligation_instances where target_id='$GOAT' and protocol_version_id='$VERSION' and rule_id='$BOOSTER_RULE' limit 1")
[ -n "$BOOSTER_OBL" ] || fail "no booster obligation generated for goat=$GOAT"
psql "$PGURL" -tAc "update outbox_messages set status='pending', published_at=null, next_attempt_at=null where aggregate_id='$GOAT' and event_type='goat.created'" >/dev/null
relay_until_published "goat.created replay" "$GOAT" "goat.created"
( cd "$BACKEND" && GOATOS_TENANT_ID=$TENANT go run ./cmd/obligation-sweeper -tenant-id "$TENANT" -version-id "$VERSION" -sop-version-id "$SOPVER" -vaccine-item-id "$ITEM" -actor-id "$USER" 2>&1 | tail -1 )
PRIMARY_COUNT=$(psqlq "select count(*) from obligation_instances where target_id='$GOAT' and protocol_version_id='$VERSION' and rule_id='$RULE'")
BOOSTER_COUNT=$(psqlq "select count(*) from obligation_instances where target_id='$GOAT' and protocol_version_id='$VERSION' and rule_id='$BOOSTER_RULE'")
COMPLETION_COUNT=$(psqlq "select count(*) from vaccination_completions where goat_id='$GOAT'")
[ "$PRIMARY_COUNT" = "1" ] || fail "primary count after replay=$PRIMARY_COUNT"
[ "$BOOSTER_COUNT" = "1" ] || fail "booster count after replay=$BOOSTER_COUNT"
[ "$COMPLETION_COUNT" = "2" ] || fail "completion count after replay=$COMPLETION_COUNT"

AC=$(curl -s "${A[@]}" "$API/vaccination/action-center?limit=500")
PP=$(curl -s "${A[@]}" "$API/goats/$GOAT/passport")
echo "Action Center contains task: $(has "$TASK" "$AC")"
echo "Passport contains accepted completion: $(has "$COMP2" "$PP")"

echo; echo "## CLOSED rework goat=$GOAT obligation=$OBL booster_obligation=$BOOSTER_OBL batch=$BATCH task=$TASK rejected_submission=$SUB1 rejected_completion=$COMP1 corrected_submission=$SUB2 accepted_completion=$COMP2"
