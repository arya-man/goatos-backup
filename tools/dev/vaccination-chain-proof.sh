#!/usr/bin/env bash
# Local DATA-PLANE vaccination business-chain proof — NOT browser/Playwright E2E.
#
# Drives the REAL local stack end-to-end and prints concrete IDs:
#   Existing in-DB goat without goat.created
#     -> backfill-goat-created emits the missing canonical event
#     -> outbox-relay eventbus delivery -> generation
#     -> obligation_instances
#   Herd Register create (POST /admin/goats)
#     -> goat.created outbox row
#     -> outbox-relay GOATOS_OUTBOX_PUBLISHER=eventbus delivery -> generation
#     -> obligation_instances
#     -> obligation-sweeper -> obligation_batch + SOP task
#     -> shed-level video proof upload (local storage) -> SOP task submission -> vaccination_completion (recorded)
#     -> verification-queue -> SOP task verify -> completion accepted + obligation completed
#     -> CT / AC / PA / WF / vaccination ops / shed drilldown / Passport read the same Postgres truth
#
# Prerequisites (see docs/runbooks/vaccination-local-business-chain.md):
#   - local stack up: admin-web :3300, api :8080, one shared DATABASE_URL (`make dev-local`)
#   - DB migrated to head. `make dev-local` does NOT migrate; in particular migration
#     000082 (sop_task_review_fanouts / sop_task_submission_fanouts) must be applied or the
#     SOP submission step 500s with: relation "sop_task_submission_fanouts" does not exist.
#   - seeded vaccination fixtures. The script resolves the currently published
#     vaccination matrix/rules/stock from Postgres after seeding; it does not pin
#     stale local proof UUIDs.
#
# This uses the source-derived local/dev baseline in
# context/source-findings/preventive-care-vaccination-roster-stage-proposal.md. Production can replace it
# with a later source-backed version if PC/vet data changes.
set -euo pipefail
export PATH="/opt/homebrew/opt/postgresql@15/bin:$PATH"
REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
. "$REPO_ROOT/tools/dev/e2e-db-env.sh"
goatos_e2e_require_isolated_database "tools/dev/vaccination-chain-proof.sh"
export GOATOS_ENV=local GOATOS_AUTH_MODE=bearer
export GOATOS_AUTH_ISSUER=goatos-local GOATOS_AUTH_AUDIENCE=goatos-api
export GOATOS_AUTH_HS256_SECRET="${GOATOS_AUTH_HS256_SECRET:-goatos-local-dev-secret-32-bytes-min}" GOATOS_AUTH_MAX_TOKEN_TTL=24h
PGURL="$DATABASE_URL"; API="${GOATOS_API_BASE_URL:-http://127.0.0.1:8080}"
TENANT=00000000-0000-4000-8000-000000000001
ADMIN_USER=90000000-0000-4000-8000-000000000101
OPERATOR_USER=90000000-0000-4000-8000-000000000102
VERIFIER_USER=90000000-0000-4000-8000-000000000103
LEADERSHIP_USER=90000000-0000-4000-8000-000000000104
PROOF_PARK=00000000-0000-4000-8000-000000003001
OLD_VERSION=00000000-0000-4000-8000-00000000b011    # retired ET+TT baseline; must not generate obligations
OLD_RULE=00000000-0000-4000-8000-00000000b012       # retired legacy ET primary rule
BACKEND="$REPO_ROOT/backend"
psqlq(){ psql "$PGURL" -tAc "$1"; }
jqp(){ python3 -c "import sys,json;d=json.load(sys.stdin);print($1)" 2>/dev/null; }
has(){ grep -q "$1" <<<"$2" && echo HIT || echo MISS; }
fail(){ echo "FAIL $*" >&2; exit 1; }
assert_hit(){ local label=$1 needle=$2 haystack=$3; [ "$(has "$needle" "$haystack")" = HIT ] || fail "$label missing $needle"; }
relay_once(){
  local label=$1 limit=${2:-50} out
  out=$(cd "$BACKEND" && GOATOS_OUTBOX_ALLOW_NONDURABLE=1 GOATOS_OUTBOX_PUBLISHER=eventbus go run ./cmd/outbox-relay -limit "$limit" 2>&1)
  if [ "${GOATOS_PROOF_VERBOSE_RELAY:-0}" = "1" ]; then
    echo "$out" | tail -1
  fi
}
relay_until_published(){
  local label=$1 aggregate=$2 event_type=$3
  local i status last_error
  for i in $(seq 1 5); do
    relay_once "$label" 500
    status=$(psqlq "select status from outbox_messages where tenant_id='$TENANT' and aggregate_id='$aggregate' and event_type='$event_type' order by created_at desc limit 1")
    if [ "$status" = "published" ]; then
      echo "$label: $event_type published"
      return 0
    fi
    if [ "$status" = "failed" ] || [ "$status" = "dead_letter" ]; then
      last_error=$(psqlq "select coalesce(last_error,'') from outbox_messages where tenant_id='$TENANT' and aggregate_id='$aggregate' and event_type='$event_type' order by created_at desc limit 1")
      fail "$label target $event_type for aggregate=$aggregate reached status=$status last_error=$last_error"
    fi
    sleep 1
  done
  fail "$label did not publish $event_type for aggregate=$aggregate; last_status=$status"
}
select_active_obligation_for_goat(){
  local goat=$1
  psqlq "
    SELECT oi.obligation_id::text || '|' || oi.rule_id::text || '|' || pr.dose_code
    FROM obligation_instances oi
    JOIN protocol_rules pr
      ON pr.tenant_id = oi.tenant_id
     AND pr.protocol_version_id = oi.protocol_version_id
     AND pr.rule_id = oi.rule_id
    WHERE oi.tenant_id = '$TENANT'
      AND oi.target_id = '$goat'
      AND oi.protocol_version_id = '$VERSION'
      AND oi.status IN ('scheduled','due','deferred')
    ORDER BY oi.due_at NULLS LAST, pr.sequence, pr.sort_order, oi.obligation_id
    LIMIT 1"
}
assert_outbox_published(){
  local label=$1 aggregate=$2 event_type=$3
  local bad not_published
  bad=$(psqlq "select count(*) from outbox_messages where tenant_id='$TENANT' and aggregate_id='$aggregate' and event_type='$event_type' and status in ('failed','dead_letter')")
  [ "$bad" = "0" ] || fail "$label outbox has failed/dead_letter rows=$bad"
  not_published=$(psqlq "select count(*) from outbox_messages where tenant_id='$TENANT' and aggregate_id='$aggregate' and event_type='$event_type' and status <> 'published'")
  [ "$not_published" = "0" ] || fail "$label outbox has non-published rows=$not_published"
}
. "$(cd "$(dirname "$0")" && pwd)/vaccination-active-fixture.sh"
install_vaccination_proof_cleanup_trap

( cd "$BACKEND" && GOATOS_TENANT_ID=$TENANT go run ./cmd/seed-dev-grant -tenant-id "$TENANT" -user-id "$ADMIN_USER" -role ceo_internal >/dev/null )
( cd "$BACKEND" && GOATOS_TENANT_ID=$TENANT go run ./cmd/seed-dev-grant -tenant-id "$TENANT" -user-id "$OPERATOR_USER" -role operator -park-id "$PROOF_PARK" >/dev/null )
( cd "$BACKEND" && GOATOS_TENANT_ID=$TENANT go run ./cmd/seed-dev-grant -tenant-id "$TENANT" -user-id "$VERIFIER_USER" -role verifier >/dev/null )
( cd "$BACKEND" && GOATOS_TENANT_ID=$TENANT go run ./cmd/seed-dev-grant -tenant-id "$TENANT" -user-id "$LEADERSHIP_USER" -role park_head -park-id "$PROOF_PARK" >/dev/null )
ADMIN_TOKEN=$(cd "$BACKEND" && go run ./cmd/mint-dev-token -tenant-id "$TENANT" -user-id "$ADMIN_USER" -ttl 2h 2>/dev/null)
OPERATOR_TOKEN=$(cd "$BACKEND" && go run ./cmd/mint-dev-token -tenant-id "$TENANT" -user-id "$OPERATOR_USER" -ttl 2h 2>/dev/null)
VERIFIER_TOKEN=$(cd "$BACKEND" && go run ./cmd/mint-dev-token -tenant-id "$TENANT" -user-id "$VERIFIER_USER" -ttl 2h 2>/dev/null)
LEADERSHIP_TOKEN=$(cd "$BACKEND" && go run ./cmd/mint-dev-token -tenant-id "$TENANT" -user-id "$LEADERSHIP_USER" -ttl 2h 2>/dev/null)
A=(-H "Authorization: Bearer $ADMIN_TOKEN")
O=(-H "Authorization: Bearer $OPERATOR_TOKEN")
V=(-H "Authorization: Bearer $VERIFIER_TOKEN")
L=(-H "Authorization: Bearer $LEADERSHIP_TOKEN")
STAMP=$(date +%s); RFID="CHAINPROOF-$STAMP"
BACKFILL_SHED_CODE="CHAIN-BF-$STAMP"
MAIN_SHED_CODE="CHAIN-MAIN-$STAMP"
ENTRY_DATE=$(date -u +%F)
ADMINISTERED_AT=$(date -u +%Y-%m-%dT%H:%M:%SZ)
if DOB_DAY28=$(date -u -v-28d +%F 2>/dev/null); then
  :
else
  DOB_DAY28=$(date -u -d "$ENTRY_DATE - 28 days" +%F)
fi
GOAT_ENTRY_DATE="$DOB_DAY28"
echo "## vaccination-chain-proof stamp=$STAMP api=$API"

echo; echo "### 0. active published vaccination matrix fixture is present"
( cd "$BACKEND" && go run ./cmd/seed-vaccination-trigger -tenant-id "$TENANT" >/dev/null )
psqlq "
  INSERT INTO inventory_stock (
    stock_id, tenant_id, item_id, location_id, lot_code, expiry_date,
    quantity_in_stock, quantity_reserved, quantity_unit, status
  )
  SELECT
    '00000000-0000-4000-8000-00000000c777'::uuid,
    '$TENANT'::uuid,
    i.item_id,
    '$PROOF_PARK'::uuid,
    'CHAIN-PROOF-ETTT',
    DATE '2027-12-31',
    1000,
    0,
    'dose',
    'active'
  FROM inventory_items i
  WHERE i.tenant_id = '$TENANT'
    AND i.item_code = 'VAC-ET-TT'
  ORDER BY i.item_id
  LIMIT 1
  ON CONFLICT (stock_id) DO UPDATE
  SET quantity_in_stock = GREATEST(inventory_stock.quantity_reserved + 1000, inventory_stock.quantity_in_stock, 1000),
      status = 'active',
      updated_at = now()" >/dev/null
resolve_vaccination_fixture
OLD_STATUS=$(psqlq "select status from protocol_versions where tenant_id='$TENANT' and protocol_version_id='$OLD_VERSION'")
[ "$OLD_STATUS" = "retired" ] || fail "legacy ET baseline status=$OLD_STATUS, want retired for version=$OLD_VERSION"
echo "matrix=$MATRIX_SUMMARY primary=$RULE_SUMMARY booster=$BOOSTER_SUMMARY sop=$SOPVER item=$ITEM lot=$LOT"
BACKFILL_SHED=$(psqlq "insert into locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, country, state_region, timezone, status) values (gen_random_uuid(), '$TENANT', 'shed', '$BACKFILL_SHED_CODE', 'Chain Proof Backfill $STAMP', '$PROOF_PARK', 'IN', 'Tamil Nadu', 'Asia/Kolkata', 'active') returning location_id" | head -n 1)
MAIN_SHED=$(psqlq "insert into locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, country, state_region, timezone, status) values (gen_random_uuid(), '$TENANT', 'shed', '$MAIN_SHED_CODE', 'Chain Proof Main $STAMP', '$PROOF_PARK', 'IN', 'Tamil Nadu', 'Asia/Kolkata', 'active') returning location_id" | head -n 1)
psqlq "insert into location_operational_attributes (tenant_id, location_id, usable_for_counts, usable_for_feed, usable_for_vaccination, usable_for_sop, is_holding, is_quarantine, is_icu, display_order, notes) values ('$TENANT', '$BACKFILL_SHED', true, true, true, true, false, false, false, 101, 'Chain proof isolated backfill shed'), ('$TENANT', '$MAIN_SHED', true, true, true, true, false, false, false, 102, 'Chain proof isolated main shed') on conflict (location_id) do update set usable_for_vaccination=true, usable_for_sop=true, updated_at=now()" >/dev/null
echo "proof_park=$PROOF_PARK proof_sheds backfill=$BACKFILL_SHED_CODE/$BACKFILL_SHED main=$MAIN_SHED_CODE/$MAIN_SHED"

echo; echo "### 0b. existing-goat backfill generates active matrix due work"
BACKFILL_GOAT=$(psqlq "insert into goats (goat_id, tenant_id, lifecycle_status, health_status, reproductive_status, custodian_party_id, current_location_id, park_id, shed_id, management_stage, sex, breed, dob, approx_dob, entry_date, species, origin_type) values (gen_random_uuid(), '$TENANT', 'alive', 'healthy', 'open', '00000000-0000-4000-8000-000000001001', '$BACKFILL_SHED', '$PROOF_PARK', '$BACKFILL_SHED', 'K2', 'female', 'all', DATE '$DOB_DAY28', DATE '$DOB_DAY28', DATE '$GOAT_ENTRY_DATE', 'goat', 'birth') returning goat_id" | head -n 1)
BACKFILL_OUTBOX_COUNT=$(psqlq "select count(*) from outbox_messages where tenant_id='$TENANT' and aggregate_id='$BACKFILL_GOAT' and event_type='goat.created'")
[ "$BACKFILL_OUTBOX_COUNT" = "0" ] || fail "backfill goat unexpectedly has goat.created outbox count=$BACKFILL_OUTBOX_COUNT"
( cd "$BACKEND" && GOATOS_TENANT_ID=$TENANT go run ./cmd/backfill-goat-created -tenant-id "$TENANT" -goat-id "$BACKFILL_GOAT" -limit 1 )
BACKFILL_EVENT_COUNT=$(psqlq "select count(*) from goat_identity_events where tenant_id='$TENANT' and goat_id='$BACKFILL_GOAT' and event_type='goat.created'")
[ "$BACKFILL_EVENT_COUNT" = "1" ] || fail "backfill goat.created identity event count=$BACKFILL_EVENT_COUNT goat=$BACKFILL_GOAT"
relay_until_published "backfilled goat.created delivery" "$BACKFILL_GOAT" "goat.created"
BACKFILL_OBL_COUNT=$(psqlq "select count(*) from obligation_instances where target_id='$BACKFILL_GOAT' and protocol_version_id='$VERSION' and status in ('scheduled','due','deferred')")
[ "$BACKFILL_OBL_COUNT" -ge 1 ] || fail "backfill active obligation count=$BACKFILL_OBL_COUNT goat=$BACKFILL_GOAT"
BACKFILL_SELECTED=$(select_active_obligation_for_goat "$BACKFILL_GOAT")
[ -n "$BACKFILL_SELECTED" ] || fail "backfill did not generate selectable active vaccination work goat=$BACKFILL_GOAT"
BACKFILL_OLD_OBL_COUNT=$(psqlq "select count(*) from obligation_instances where target_id='$BACKFILL_GOAT' and protocol_version_id='$OLD_VERSION' and rule_id='$OLD_RULE'")
[ "$BACKFILL_OLD_OBL_COUNT" = "0" ] || fail "backfill generated retired legacy ET obligation count=$BACKFILL_OLD_OBL_COUNT goat=$BACKFILL_GOAT"
( cd "$BACKEND" && GOATOS_TENANT_ID=$TENANT go run ./cmd/backfill-goat-created -tenant-id "$TENANT" -goat-id "$BACKFILL_GOAT" -limit 1 >/dev/null )
BACKFILL_OUTBOX_REPLAY_COUNT=$(psqlq "select count(*) from outbox_messages where tenant_id='$TENANT' and aggregate_id='$BACKFILL_GOAT' and event_type='goat.created'")
[ "$BACKFILL_OUTBOX_REPLAY_COUNT" = "1" ] || fail "backfill replay outbox count=$BACKFILL_OUTBOX_REPLAY_COUNT goat=$BACKFILL_GOAT"
BACKFILL_REPLAY_COUNT=$(psqlq "select count(*) from obligation_instances where target_id='$BACKFILL_GOAT' and protocol_version_id='$VERSION' and status in ('scheduled','due','deferred')")
[ "$BACKFILL_REPLAY_COUNT" = "$BACKFILL_OBL_COUNT" ] || fail "backfill replay active obligation count changed $BACKFILL_OBL_COUNT -> $BACKFILL_REPLAY_COUNT goat=$BACKFILL_GOAT"
BACKFILL_OLD_REPLAY_COUNT=$(psqlq "select count(*) from obligation_instances where target_id='$BACKFILL_GOAT' and protocol_version_id='$OLD_VERSION' and rule_id='$OLD_RULE'")
[ "$BACKFILL_OLD_REPLAY_COUNT" = "0" ] || fail "backfill replay generated retired legacy ET obligation count=$BACKFILL_OLD_REPLAY_COUNT goat=$BACKFILL_GOAT"
echo "BACKFILL_GOAT=$BACKFILL_GOAT active_obligations=$BACKFILL_OBL_COUNT selected=$BACKFILL_SELECTED replay=$BACKFILL_REPLAY_COUNT"
psqlq "update obligation_instances set status='canceled', updated_at=now() where target_id='$BACKFILL_GOAT' and protocol_version_id='$VERSION' and status in ('scheduled','due','deferred')" >/dev/null

echo; echo "### 1. Herd Register create  POST /admin/goats"
CREATE=$(curl -s "${A[@]}" -H "Idempotency-Key: chain-$STAMP" -H "Content-Type: application/json" -X POST "$API/admin/goats" -d @- <<JSON
{"animal_identifier_1":"$RFID-A1","animal_identifier_2":"$RFID-A2","species":"goat","park_id":"$PROOF_PARK","shed_id":"$MAIN_SHED","sex":"female","breed":"all","dob":"$DOB_DAY28","dob_estimated":true,"origin_type":"birth","entry_date":"$GOAT_ENTRY_DATE","management_stage":"K2","health_status":"healthy","reproductive_status":"open","evidence_refs":[{"evidence_type":"source_record","evidence_id":"chain-$STAMP"}]}
JSON
)
GOAT=$(echo "$CREATE" | jqp 'd["goat"]["goat_id"]'); SHED=$(echo "$CREATE" | jqp 'd["goat"]["location_path"]["shed_id"]')
[ -n "$GOAT" ] || { echo "FAIL step1: $CREATE"; exit 1; }
echo "GOAT=$GOAT SHED=$SHED"
psqlq "update obligation_instances oi set status='canceled', updated_at=now() from goats g join locations l on l.location_id = g.shed_id where oi.tenant_id='$TENANT' and oi.target_id=g.goat_id and oi.protocol_version_id='$VERSION' and oi.status in ('scheduled','due','deferred') and g.goat_id <> '$GOAT' and l.location_code like 'CHAIN-%'" >/dev/null

echo; echo "### 2. goat.created outbox row"
psql "$PGURL" -c "select event_id,event_type,status from outbox_messages where aggregate_id='$GOAT'"
EVENT=$(psqlq "select event_id from outbox_messages where aggregate_id='$GOAT' limit 1")

echo; echo "### 3. outbox-relay eventbus delivery -> generation"
relay_until_published "goat.created delivery" "$GOAT" "goat.created"
ACTIVE_SELECTED=$(select_active_obligation_for_goat "$GOAT")
[ -n "$ACTIVE_SELECTED" ] || { echo "FAIL step3 (no active obligation generated)"; exit 1; }
IFS='|' read -r OBL RULE ACTIVE_DOSE_CODE <<<"$ACTIVE_SELECTED"
[ -n "$OBL" ] && [ -n "$RULE" ] || { echo "FAIL step3 (malformed active obligation selection: $ACTIVE_SELECTED)"; exit 1; }
OLD_OBL_COUNT=$(psqlq "select count(*) from obligation_instances where target_id='$GOAT' and protocol_version_id='$OLD_VERSION' and rule_id='$OLD_RULE'")
[ "$OLD_OBL_COUNT" = "0" ] || fail "goat generated retired legacy ET obligation count=$OLD_OBL_COUNT goat=$GOAT"
assert_outbox_published "goat.created" "$GOAT" "goat.created"
echo "selected_obligation=$OBL selected_rule=$RULE selected_dose=$ACTIVE_DOSE_CODE"
psql "$PGURL" -c "select oi.obligation_id,pr.dose_code,oi.status,oi.due_at from obligation_instances oi join protocol_rules pr on pr.tenant_id=oi.tenant_id and pr.protocol_version_id=oi.protocol_version_id and pr.rule_id=oi.rule_id where oi.target_id='$GOAT' and oi.protocol_version_id='$VERSION' order by oi.due_at nulls last, pr.sequence"
SWEEP_DUE_BEFORE=$(psqlq "select to_char(due_at + interval '1 second', 'YYYY-MM-DD\"T\"HH24:MI:SS\"Z\"') from obligation_instances where obligation_id='$OBL'")
[ -n "$SWEEP_DUE_BEFORE" ] || fail "selected obligation $OBL has no due_at for sweep cutoff"

echo; echo "### 4. obligation-sweeper through selected due date -> batch + SOP task"
( cd "$BACKEND" && GOATOS_TENANT_ID=$TENANT go run ./cmd/obligation-sweeper -tenant-id "$TENANT" -version-id "$VERSION" -sop-version-id "$SOPVER" -vaccine-item-id "$ITEM" -actor-id "$OPERATOR_USER" -due-before "$SWEEP_DUE_BEFORE" 2>&1 | tail -1 )
BATCH=$(psqlq "select batch_id from obligation_instances where obligation_id='$OBL'")
TASK=$(psqlq "select sop_task_id from obligation_batches where batch_id='$BATCH'")
[ -n "$TASK" ] || { echo "FAIL step4 (no SOP task)"; exit 1; }
echo "BATCH=$BATCH TASK=$TASK"

echo; echo "### 5. shed-level video proof upload (scope=task subject=shed)"
mkshedproof(){ local shed=$1 clip=${2:-1} source=${3:-in_app_camera}
  local start_ms end_ms r pid url full upload hash size complete state
  start_ms=$(date +%s000)
  end_ms=$((start_ms + 30000))
  r=$(curl -s "${O[@]}" -H "Content-Type: application/json" -H "Idempotency-Key: proof-$STAMP-shed-$clip" -X POST "$API/app/proofs/uploads" -d @- <<JSON
{"proof_type":"video","mime_type":"video/mp4","scope_type":"task","scope_id":"$TASK","subject_type":"shed","subject_id":"$shed","metadata":{"capture_source":"$source","captured_start_ms":$start_ms,"captured_end_ms":$end_ms,"clip_ordinal":$clip,"vaccines_covered":["$ACTIVE_DOSE_CODE"],"proof_mode":"shed_level_video"}}
JSON
)
  local pid=$(echo "$r" | jqp 'd["proof"]["proof_id"]'); local url=$(echo "$r" | jqp 'd["upload_url"]')
  [ -n "$pid" ] || { echo "  proof FAIL (shed=$shed clip=$clip source=$source): $r" >&2; return 1; }
  case "$url" in http*) full=$url;; /*) full="$API$url";; *) full="$API/$url";; esac
  printf 'chain-shed-%s-clip-%s-%s' "$shed" "$clip" "$STAMP" >/tmp/cp_shed_$clip.bin
  upload=$(curl -s "${O[@]}" -H "Content-Type: video/mp4" -X PUT --data-binary @/tmp/cp_shed_$clip.bin "$full")
  hash=$(echo "$upload" | jqp 'd.get("proof",{}).get("content_hash","")')
  size=$(echo "$upload" | jqp 'd.get("proof",{}).get("size_bytes",0)')
  [ -n "$hash" ] && [ "$size" != "0" ] || { echo "  proof upload FAIL (shed=$shed clip=$clip): $upload" >&2; return 1; }
  complete=$(curl -s "${O[@]}" -H "Content-Type: application/json" -X POST "$API/app/proofs/$pid/complete" -d @- <<JSON
{"content_hash":"$hash","mime_type":"video/mp4","size_bytes":$size,"duration_ms":30000,"metadata":{"completed_by":"local_e2e","clip_ordinal":$clip}}
JSON
)
  state=$(echo "$complete" | jqp 'd.get("proof",{}).get("upload_state","")')
  [ "$state" = "completed" ] || { echo "  proof upload FAIL (shed=$shed clip=$clip): $upload" >&2; return 1; }
  echo "$pid"; }
P_SHED=$(mkshedproof "$SHED" 1 "in_app_camera")
echo "PROOF shed=$SHED clip1=$P_SHED"

echo; echo "### 5b. goat scan capture (server draft roster)"
SCAN_CAPTURED_AT_MS=$(date +%s000)
SCAN=$(curl -s "${O[@]}" -H "Content-Type: application/json" -H "Idempotency-Key: scan-$STAMP-$GOAT" -X POST "$API/app/tasks/$TASK/scan-captures" -d @- <<JSON
{"field_key":"__scan_roster__","tag":"$GOAT","goat_id":"$GOAT","obligation_id":"$OBL","captured_at_ms":$SCAN_CAPTURED_AT_MS}
JSON
)
SCAN_ID=$(echo "$SCAN" | jqp 'd.get("capture",{}).get("capture_id","")')
[ -n "$SCAN_ID" ] || { echo "FAIL step5b: $SCAN"; exit 1; }
SCAN_STORED_MS=$(psqlq "select floor(extract(epoch from captured_at) * 1000)::bigint from sop_task_scan_captures where tenant_id='$TENANT' and capture_id='$SCAN_ID'::uuid")
[ "$SCAN_STORED_MS" = "$SCAN_CAPTURED_AT_MS" ] || fail "scan timestamp mismatch stored=$SCAN_STORED_MS expected=$SCAN_CAPTURED_AT_MS"
echo "SCAN_CAPTURE=$SCAN_ID goat=$GOAT obligation=$OBL"

echo; echo "### 6. SOP task submission  POST /app/tasks/{task}/submissions"
SUB=$(curl -s "${O[@]}" -H "Content-Type: application/json" -X POST "$API/app/tasks/$TASK/submissions" -d @- <<JSON
{"sop_version_id":"$SOPVER","idempotency_key":"sub-$STAMP","answers":{"goat_ids":["$GOAT"]},"proof_refs":[{"proof_id":"$P_SHED","proof_type":"video","subject_type":"shed","subject_id":"$SHED","upload_state":"completed"}]}
JSON
)
SUBID=$(echo "$SUB" | jqp 'd.get("submission",{}).get("submission_id","")')
[ -n "$SUBID" ] || { echo "FAIL step6: $SUB"; exit 1; }
COMP=$(psqlq "select completion_id from vaccination_completions where goat_id='$GOAT' order by created_at desc limit 1")
[ -n "$COMP" ] || { echo "FAIL step6 (no completion recorded); fanout last_error:"; psqlq "select last_error from sop_task_submission_fanouts where submission_id='$SUBID'"; exit 1; }
echo "SUBMISSION=$SUBID COMPLETION=$COMP"
psql "$PGURL" -c "select completion_id,status,obligation_id from vaccination_completions where completion_id='$COMP'"

echo; echo "### 7. verification queue + record verdict + close"
VQ=$(curl -s "${A[@]}" "$API/vaccination/verification-queue?limit=100")
echo "verification-queue contains completion: $(has "$COMP" "$VQ")"

# Get verification item IDs for this submission (created when submission was recorded)
VITEM_IDS=$(psqlq "select array_agg(distinct item_id::text) from verification_items where tenant_id='$TENANT' and source_submission_id='$SUBID'::uuid")
echo "verification items for submission $SUBID: count=$(psqlq "select count(*) from verification_items where tenant_id='$TENANT' and source_submission_id='$SUBID'::uuid")"
[ "$VITEM_IDS" != "NULL" ] || fail "step7 no verification items created for submission $SUBID"

# For each verification item, record a verdict with the real endpoint.
for ITEM_ID in $(psqlq "select item_id::text from verification_items where tenant_id='$TENANT' and source_submission_id='$SUBID'::uuid"); do
  ITEM_ROW_VERSION=$(psqlq "select row_version from verification_items where tenant_id='$TENANT' and item_id='$ITEM_ID'")
  [ -n "$ITEM_ROW_VERSION" ] || fail "step7 missing verification item row_version for item $ITEM_ID"

  IDEM_KEY="verdict-$ITEM_ID-$(date +%s)"
  VERDICT=$(curl -s "${A[@]}" -H "Content-Type: application/json" -H "Idempotency-Key: $IDEM_KEY" -X POST "$API/verification/items/$ITEM_ID/verdict" -d "{\"decision\":\"approved\",\"reason\":\"proof accepted\",\"row_version\":$ITEM_ROW_VERSION}")
  echo "  recorded verdict for $ITEM_ID: $(echo "$VERDICT" | python3 -c 'import sys,json; d=json.load(sys.stdin); print(d.get("item",{}).get("status","ERROR"))' 2>/dev/null || echo "ERROR")"
done

# Submission-owned verification items are closed atomically through the submission
# endpoint; per-item close is intentionally rejected for source_submission_id rows.
CLOSE_KEY="close-submission-$SUBID-$(date +%s)"
CLOSED=$(curl -s "${A[@]}" -H "Content-Type: application/json" -H "Idempotency-Key: $CLOSE_KEY" -X POST "$API/verification/submissions/$SUBID/close")
CLOSED_COUNT=$(echo "$CLOSED" | python3 -c 'import sys,json; d=json.load(sys.stdin); print(len(d.get("items") or []))' 2>/dev/null || echo "ERROR")
[ "$CLOSED_COUNT" != "ERROR" ] || { echo "FAIL step7 close submission: $CLOSED"; exit 1; }
echo "  closed submission $SUBID: items=$CLOSED_COUNT"
psql "$PGURL" -c "select c.completion_id,c.status comp,o.obligation_id,o.status obl,o.completed_at from vaccination_completions c join obligation_instances o on o.obligation_id=c.obligation_id where c.completion_id='$COMP'"
COMPLETE_STATE=$(psqlq "select c.status || ':' || o.status || ':' || (o.completed_at is not null)::text from vaccination_completions c join obligation_instances o on o.obligation_id=c.obligation_id where c.completion_id='$COMP'")
[ "$COMPLETE_STATE" = "accepted:completed:true" ] || fail "step7 inconsistent completion state $COMPLETE_STATE"

# Verify all verification items are now closed
CLOSED_ITEM_COUNT=$(psqlq "select count(*) from verification_items where tenant_id='$TENANT' and source_submission_id='$SUBID'::uuid and status='approved' and closed_at is not null")
TOTAL_ITEM_COUNT=$(psqlq "select count(*) from verification_items where tenant_id='$TENANT' and source_submission_id='$SUBID'::uuid")
[ "$CLOSED_ITEM_COUNT" = "$TOTAL_ITEM_COUNT" ] || fail "step7 not all verification items closed: $CLOSED_ITEM_COUNT/$TOTAL_ITEM_COUNT"

VAX_OUTBOX_COUNT=$(psqlq "select count(*) from outbox_messages where tenant_id='$TENANT' and event_type='vaccination.completed' and aggregate_id='$OBL'")
[ "$VAX_OUTBOX_COUNT" = "1" ] || fail "step7 vaccination.completed outbox count=$VAX_OUTBOX_COUNT"

echo; echo "### 7b. vaccination.completed delivery keeps successor scheduling bounded"
relay_until_published "vaccination.completed delivery" "$OBL" "vaccination.completed"
SUCCESSOR_OBL_COUNT=$(psqlq "select count(*) from obligation_instances where target_id='$GOAT' and protocol_version_id='$VERSION' and obligation_id <> '$OBL' and status in ('scheduled','due','deferred','completed')")
[ "$SUCCESSOR_OBL_COUNT" -ge 1 ] || fail "step7b no remaining/successor vaccination work for goat=$GOAT after completing $ACTIVE_DOSE_CODE"
SUCCESSOR_OBL=$(psqlq "select obligation_id from obligation_instances where target_id='$GOAT' and protocol_version_id='$VERSION' and obligation_id <> '$OBL' order by created_at desc, obligation_id limit 1")
psql "$PGURL" -c "select oi.obligation_id,pr.dose_code,oi.status,oi.due_at from obligation_instances oi join protocol_rules pr on pr.tenant_id=oi.tenant_id and pr.protocol_version_id=oi.protocol_version_id and pr.rule_id=oi.rule_id where oi.target_id='$GOAT' and oi.protocol_version_id='$VERSION' order by oi.due_at nulls last, pr.sequence"

echo; echo "### 8. read models reflect the same Postgres truth"
RID="batch:$BATCH:rule:$RULE:shed:$SHED"
AC=$(curl -s "${A[@]}" "$API/vaccination/action-center?limit=500")
WF=$(curl -s "${A[@]}" "$API/vaccination/workflows/$RID")
PP=$(curl -s "${A[@]}" "$API/goats/$GOAT/passport")
SD=$(curl -s "${A[@]}" "$API/vaccination/execution/sheds/$SHED")
printf '%-26s %s\n' "Action Center (API):"    "completed_obl_in_active_list:$(has "$OBL" "$AC")"
printf '%-26s %s\n' "Workflows row (API):"    "row_id=$RID obl:$(has "$OBL" "$WF") comp:$(has "$COMP" "$WF")"
printf '%-26s %s\n' "Goat Passport (API):"    "goat:$(has "$GOAT" "$PP") comp:$(has "$COMP" "$PP") obl:$(has "$OBL" "$PP")"
printf '%-26s %s\n' "Shed drilldown (API):"   "batch-drive workState=$(echo "$SD" | python3 -c 'import sys,json;d=json.load(sys.stdin);print(next((x.get("workState") for x in d.get("drives",[]) if x.get("driveId")=="'$BATCH'"),"MISS"))')"
[ -n "$RID" ] || fail "Workflows row id not found for batch $BATCH"
assert_hit "Workflows obligation" "$OBL" "$WF"
assert_hit "Workflows completion" "$COMP" "$WF"
assert_hit "Passport goat" "$GOAT" "$PP"
assert_hit "Passport completion" "$COMP" "$PP"
assert_hit "Passport obligation" "$OBL" "$PP"
SD_STATE=$(echo "$SD" | python3 -c 'import sys,json;d=json.load(sys.stdin);print(next((x.get("workState") for x in d.get("drives",[]) if x.get("driveId")=="'$BATCH'"),"MISS"))')
[ "$SD_STATE" != "MISS" ] || fail "Shed drilldown missing batch $BATCH"
# Aggregate surfaces reflect the chain via scoped counts (only chain-proof completion is 'accepted' in a clean DB).
echo "Control Tower (API+SQL):  verification_backlog=$(echo "$(curl -s "${A[@]}" "$API/control-tower/vaccination")" | jqp 'd["summary"]["verification_backlog"]') (completed obligation is not a gap)"
echo "Adherence (API):          completed_count=$(curl -s "${A[@]}" "$API/vaccination/adherence" | jqp 'd["summary"]["completed_count"]')"
echo "Operations (API):         my-shed accepted=$(curl -s "${A[@]}" "$API/vaccination/operations" | python3 -c 'import sys,json;d=json.load(sys.stdin);print(next((c["counts"].get("accepted") for c in d.get("cohorts",[]) if c.get("shedId")=="'$SHED'"),"MISS"))')"

echo; echo "### 9. replay / idempotency (no duplicates)"
psql "$PGURL" -tAc "update outbox_messages set status='pending', published_at=null, next_attempt_at=null where aggregate_id='$GOAT'" >/dev/null
relay_until_published "goat.created replay" "$GOAT" "goat.created"
( cd "$BACKEND" && GOATOS_TENANT_ID=$TENANT go run ./cmd/obligation-sweeper -tenant-id "$TENANT" -version-id "$VERSION" -sop-version-id "$SOPVER" -vaccine-item-id "$ITEM" -actor-id "$OPERATOR_USER" -due-before "$SWEEP_DUE_BEFORE" 2>&1 | tail -1 )
assert_outbox_published "goat.created replay" "$GOAT" "goat.created"
assert_outbox_published "vaccination.completed delivery" "$OBL" "vaccination.completed"
echo "selected-rule obligations for goat after replay (expect 1): $(psqlq "select count(*) from obligation_instances where target_id='$GOAT' and protocol_version_id='$VERSION' and rule_id='$RULE'")"
echo "non-selected vaccination obligations for goat after replay (expect $SUCCESSOR_OBL_COUNT): $(psqlq "select count(*) from obligation_instances where target_id='$GOAT' and protocol_version_id='$VERSION' and obligation_id <> '$OBL' and status in ('scheduled','due','deferred','completed')")"
echo "completions for goat after replay (expect 1): $(psqlq "select count(*) from vaccination_completions where goat_id='$GOAT'")"
OBL_REPLAY_COUNT=$(psqlq "select count(*) from obligation_instances where target_id='$GOAT' and protocol_version_id='$VERSION' and rule_id='$RULE'")
OLD_REPLAY_COUNT=$(psqlq "select count(*) from obligation_instances where target_id='$GOAT' and protocol_version_id='$OLD_VERSION' and rule_id='$OLD_RULE'")
SUCCESSOR_REPLAY_COUNT=$(psqlq "select count(*) from obligation_instances where target_id='$GOAT' and protocol_version_id='$VERSION' and obligation_id <> '$OBL' and status in ('scheduled','due','deferred','completed')")
COMP_REPLAY_COUNT=$(psqlq "select count(*) from vaccination_completions where goat_id='$GOAT'")
[ "$OBL_REPLAY_COUNT" = "1" ] || fail "replay obligation count=$OBL_REPLAY_COUNT"
[ "$OLD_REPLAY_COUNT" = "0" ] || fail "replay generated retired legacy ET obligation count=$OLD_REPLAY_COUNT"
[ "$SUCCESSOR_REPLAY_COUNT" = "$SUCCESSOR_OBL_COUNT" ] || fail "replay successor obligation count changed $SUCCESSOR_OBL_COUNT -> $SUCCESSOR_REPLAY_COUNT"
[ "$COMP_REPLAY_COUNT" = "1" ] || fail "replay completion count=$COMP_REPLAY_COUNT"

echo; echo "## CLOSED  goat=$GOAT event=$EVENT obligation=$OBL successor_obligation=$SUCCESSOR_OBL batch=$BATCH task=$TASK submission=$SUBID completion=$COMP rule=$RULE dose=$ACTIVE_DOSE_CODE version=$VERSION"
