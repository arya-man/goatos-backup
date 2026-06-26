#!/usr/bin/env bash
# Local DATA-PLANE vaccination business-chain proof — NOT browser/Playwright E2E.
#
# Drives the REAL local stack end-to-end and prints concrete IDs:
#   Herd Register create (POST /admin/goats)
#     -> goat.created outbox row
#     -> outbox-relay GOATOS_OUTBOX_PUBLISHER=eventbus delivery -> generation
#     -> obligation_instances
#     -> obligation-sweeper -> obligation_batch + SOP task
#     -> 3 video proofs (local storage) -> SOP task submission -> vaccination_completion (recorded)
#     -> verification-queue -> accept -> completion accepted + obligation completed
#     -> CT / AC / PA / WF / vaccination ops / shed drilldown / Passport read the same Postgres truth
#
# Prerequisites (see docs/runbooks/vaccination-local-business-chain.md):
#   - local stack up: admin-web :3300, api :8080, docker PG 127.0.0.1:55432 (`make dev-local`)
#   - DB migrated to head. `make dev-local` does NOT migrate; in particular migration
#     000082 (sop_task_review_fanouts / sop_task_submission_fanouts) must be applied or the
#     SOP submission step 500s with: relation "sop_task_submission_fanouts" does not exist.
#   - seeded source-derived ET dev baseline (seed-vaccination-trigger): published version b011,
#     rule b012 (ET-PRIMARY-1, birth_age day 21), linked published SOP b0..0002, FEFO vaccine lot b002.
#
# This uses the source-derived local/dev baseline in
# context/source-findings/phc-vaccination-roster-stage-proposal.md. Production can replace it
# with a later source-backed version if PHC/vet data changes.
set -uo pipefail
export PATH="/opt/homebrew/opt/postgresql@15/bin:$PATH"
export GOATOS_ENV=local GOATOS_AUTH_MODE=bearer
export GOATOS_AUTH_ISSUER=goatos-local GOATOS_AUTH_AUDIENCE=goatos-api
export GOATOS_AUTH_HS256_SECRET="${GOATOS_AUTH_HS256_SECRET:-goatos-local-dev-secret-32-bytes-min}" GOATOS_AUTH_MAX_TOKEN_TTL=24h
export DATABASE_URL="${DATABASE_URL:-postgres://postgres:goatos@127.0.0.1:55432/goatos?sslmode=disable}"
PGURL="$DATABASE_URL"; API="${GOATOS_API_BASE_URL:-http://127.0.0.1:8080}"
TENANT=00000000-0000-4000-8000-000000000001
USER=90000000-0000-4000-8000-000000000101
VERSION=00000000-0000-4000-8000-00000000b011        # published source-derived ET dev baseline
SOPVER=b0000000-0000-4000-8000-000000000002         # linked published SOP version
ITEM=00000000-0000-4000-8000-00000000b001           # vaccine inventory item
LOT=00000000-0000-4000-8000-00000000b002            # inventory_stock.stock_id (FEFO lot ET-LOT-001 @ CBE)
RULE=00000000-0000-4000-8000-00000000b012           # ET-PRIMARY-1 birth_age day-21 rule
BACKEND="$(cd "$(dirname "$0")/../../backend" && pwd)"
psqlq(){ psql "$PGURL" -tAc "$1"; }
jqp(){ python3 -c "import sys,json;d=json.load(sys.stdin);print($1)" 2>/dev/null; }
has(){ grep -q "$1" <<<"$2" && echo HIT || echo MISS; }

TOKEN=$(cd "$BACKEND" && go run ./cmd/mint-dev-token -tenant-id "$TENANT" -user-id "$USER" -ttl 2h 2>/dev/null)
A=(-H "Authorization: Bearer $TOKEN")
STAMP=$(date +%s); RFID="CHAINPROOF-$STAMP"
ENTRY_DATE=$(date -u +%F)
if DOB_DAY21=$(date -u -v-21d +%F 2>/dev/null); then
  :
else
  DOB_DAY21=$(date -u -d "$ENTRY_DATE - 21 days" +%F)
fi
echo "## vaccination-chain-proof stamp=$STAMP api=$API"

echo; echo "### 1. Herd Register create  POST /admin/goats"
CREATE=$(curl -s "${A[@]}" -H "Idempotency-Key: chain-$STAMP" -H "Content-Type: application/json" -X POST "$API/admin/goats" -d @- <<JSON
{"rfid":"$RFID","park_code":"CBE","shed_code":"CBE_SHED_MANDELA_1_PART_1","sex":"female","dob":"$DOB_DAY21","dob_estimated":true,"origin_type":"procured","entry_date":"$ENTRY_DATE","management_stage":"K1","evidence_refs":[{"evidence_type":"source_record","evidence_id":"chain-$STAMP"}]}
JSON
)
GOAT=$(echo "$CREATE" | jqp 'd["goat"]["goat_id"]'); SHED=$(echo "$CREATE" | jqp 'd["goat"]["location_path"]["shed_id"]')
[ -n "$GOAT" ] || { echo "FAIL step1: $CREATE"; exit 1; }
echo "GOAT=$GOAT SHED=$SHED"

echo; echo "### 2. goat.created outbox row"
psql "$PGURL" -c "select event_id,event_type,status from outbox_messages where aggregate_id='$GOAT'"
EVENT=$(psqlq "select event_id from outbox_messages where aggregate_id='$GOAT' limit 1")

echo; echo "### 3. outbox-relay eventbus delivery -> generation"
( cd "$BACKEND" && GOATOS_OUTBOX_PUBLISHER=eventbus go run ./cmd/outbox-relay -limit 50 2>&1 | tail -1 )
OBL=$(psqlq "select obligation_id from obligation_instances where target_id='$GOAT' and protocol_version_id='$VERSION' and rule_id='$RULE' limit 1")
[ -n "$OBL" ] || { echo "FAIL step3 (no obligation generated)"; exit 1; }
psql "$PGURL" -c "select obligation_id,status,due_at from obligation_instances where target_id='$GOAT' and protocol_version_id='$VERSION' and rule_id='$RULE'"

echo; echo "### 4. obligation-sweeper -> batch + SOP task"
( cd "$BACKEND" && GOATOS_TENANT_ID=$TENANT go run ./cmd/obligation-sweeper -tenant-id "$TENANT" -version-id "$VERSION" -sop-version-id "$SOPVER" -vaccine-item-id "$ITEM" -actor-id "$USER" 2>&1 | tail -1 )
BATCH=$(psqlq "select batch_id from obligation_instances where obligation_id='$OBL'")
TASK=$(psqlq "select sop_task_id from obligation_batches where batch_id='$BATCH'")
[ -n "$TASK" ] || { echo "FAIL step4 (no SOP task)"; exit 1; }
echo "BATCH=$BATCH TASK=$TASK"

echo; echo "### 5. 3 video proofs (scope=task) shed/vial_lot/administration"
mkproof(){ local subj=$1
  local r=$(curl -s "${A[@]}" -H "Content-Type: application/json" -X POST "$API/app/proofs/uploads" \
    -d "{\"proof_type\":\"video\",\"mime_type\":\"video/mp4\",\"scope_type\":\"task\",\"scope_id\":\"$TASK\",\"subject_type\":\"$subj\"}")
  local pid=$(echo "$r" | jqp 'd["proof"]["proof_id"]'); local url=$(echo "$r" | jqp 'd["upload_url"]')
  [ -n "$pid" ] || { echo "  proof FAIL ($subj): $r" >&2; return 1; }
  case "$url" in http*) full=$url;; /*) full="$API$url";; *) full="$API/$url";; esac
  printf 'chain-%s-%s' "$subj" "$STAMP" >/tmp/cp_$subj.bin
  curl -s "${A[@]}" -H "Content-Type: video/mp4" -X PUT --data-binary @/tmp/cp_$subj.bin "$full" >/dev/null
  echo "$pid"; }
P_SHED=$(mkproof shed); P_VIAL=$(mkproof vial_lot); P_ADMIN=$(mkproof administration)
echo "PROOFS shed=$P_SHED vial_lot=$P_VIAL administration=$P_ADMIN"

echo; echo "### 6. SOP task submission  POST /app/tasks/{task}/submissions"
SUB=$(curl -s "${A[@]}" -H "Content-Type: application/json" -X POST "$API/app/tasks/$TASK/submissions" -d @- <<JSON
{"sop_version_id":"$SOPVER","idempotency_key":"sub-$STAMP","answers":{"vaccine_lot_id":"$LOT","cold_chain_verified":true,"shed_video":"$P_SHED","vial_lot_video":"$P_VIAL","administration_video":"$P_ADMIN","goat_ids":["$GOAT"],"dose_ml_given":0.5,"route_site":"subcutaneous","administered_at":"${ENTRY_DATE}T08:00:00Z","adverse_reaction":false},"proof_refs":[{"proof_id":"$P_SHED","proof_type":"video","subject_type":"shed","upload_state":"completed"},{"proof_id":"$P_VIAL","proof_type":"video","subject_type":"vial_lot","upload_state":"completed"},{"proof_id":"$P_ADMIN","proof_type":"video","subject_type":"administration","upload_state":"completed"}]}
JSON
)
SUBID=$(echo "$SUB" | jqp 'd.get("submission",{}).get("submission_id","")')
[ -n "$SUBID" ] || { echo "FAIL step6: $SUB"; exit 1; }
COMP=$(psqlq "select completion_id from vaccination_completions where goat_id='$GOAT' order by created_at desc limit 1")
[ -n "$COMP" ] || { echo "FAIL step6 (no completion recorded); fanout last_error:"; psqlq "select last_error from sop_task_submission_fanouts where submission_id='$SUBID'"; exit 1; }
echo "SUBMISSION=$SUBID COMPLETION=$COMP"
psql "$PGURL" -c "select completion_id,status,obligation_id from vaccination_completions where completion_id='$COMP'"

echo; echo "### 7. verification queue + accept"
VQ=$(curl -s "${A[@]}" "$API/vaccination/verification-queue?limit=100")
echo "verification-queue contains completion: $(has "$COMP" "$VQ")"
ACC=$(curl -s "${A[@]}" -H "Content-Type: application/json" -X POST "$API/vaccination/completions/$COMP/accept" -d "{\"idempotency_key\":\"acc-$STAMP\"}")
echo "accept: $ACC"
psql "$PGURL" -c "select c.completion_id,c.status comp,o.obligation_id,o.status obl,o.completed_at from vaccination_completions c join obligation_instances o on o.obligation_id=c.obligation_id where c.completion_id='$COMP'"

echo; echo "### 8. read models reflect the same Postgres truth"
RID=$(curl -s "${A[@]}" "$API/vaccination/action-center?limit=500" | python3 -c 'import sys,json;d=json.load(sys.stdin);print(next((r["row_id"] for r in d.get("items",[]) if r.get("batch_id")=="'$BATCH'"),""))')
AC=$(curl -s "${A[@]}" "$API/vaccination/action-center?limit=500")
WF=$(curl -s "${A[@]}" "$API/vaccination/workflows/$RID")
PP=$(curl -s "${A[@]}" "$API/goats/$GOAT/passport")
SD=$(curl -s "${A[@]}" "$API/vaccination/execution/sheds/$SHED")
printf '%-26s %s\n' "Action Center (API):"    "obl:$(has "$OBL" "$AC")"
printf '%-26s %s\n' "Workflows row (API):"    "row_id=$RID obl:$(has "$OBL" "$WF") comp:$(has "$COMP" "$WF")"
printf '%-26s %s\n' "Goat Passport (API):"    "goat:$(has "$GOAT" "$PP") comp:$(has "$COMP" "$PP") obl:$(has "$OBL" "$PP")"
printf '%-26s %s\n' "Shed drilldown (API):"   "batch-drive workState=$(echo "$SD" | python3 -c 'import sys,json;d=json.load(sys.stdin);print(next((x.get("workState") for x in d.get("drives",[]) if x.get("driveId")=="'$BATCH'"),"MISS"))')"
# Aggregate surfaces reflect the chain via scoped counts (only chain-proof completion is 'accepted' in a clean DB).
echo "Control Tower (API+SQL):  verification_backlog=$(echo "$(curl -s "${A[@]}" "$API/control-tower/vaccination")" | jqp 'd["summary"]["verification_backlog"]') (completed obligation is not a gap)"
echo "Adherence (API):          completed_count=$(curl -s "${A[@]}" "$API/vaccination/adherence" | jqp 'd["summary"]["completed_count"]')"
echo "Operations (API):         my-shed accepted=$(curl -s "${A[@]}" "$API/vaccination/operations" | python3 -c 'import sys,json;d=json.load(sys.stdin);print(next((c["counts"].get("accepted") for c in d.get("cohorts",[]) if c.get("shedId")=="'$SHED'"),"MISS"))')"

echo; echo "### 9. replay / idempotency (no duplicates)"
psql "$PGURL" -tAc "update outbox_messages set status='pending', published_at=null, next_attempt_at=null where aggregate_id='$GOAT'" >/dev/null
( cd "$BACKEND" && GOATOS_OUTBOX_PUBLISHER=eventbus go run ./cmd/outbox-relay -limit 50 2>&1 | tail -1 )
( cd "$BACKEND" && GOATOS_TENANT_ID=$TENANT go run ./cmd/obligation-sweeper -tenant-id "$TENANT" -version-id "$VERSION" -sop-version-id "$SOPVER" -vaccine-item-id "$ITEM" -actor-id "$USER" 2>&1 | tail -1 )
echo "obligations for goat after replay (expect 1): $(psqlq "select count(*) from obligation_instances where target_id='$GOAT' and protocol_version_id='$VERSION' and rule_id='$RULE'")"
echo "completions for goat after replay (expect 1): $(psqlq "select count(*) from vaccination_completions where goat_id='$GOAT'")"

echo; echo "## CLOSED  goat=$GOAT event=$EVENT obligation=$OBL batch=$BATCH task=$TASK submission=$SUBID completion=$COMP rule=$RULE version=$VERSION"
