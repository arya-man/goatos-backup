#!/usr/bin/env bash
# Local DATA-PLANE proof for imported/existing vaccination history.
#
# Proves the V1 Nuance Rules path the demo depends on:
#   published ET+TT Nuance config + SOP
#     -> goat already has accepted/verified 4-week ET+TT history
#     -> generator suppresses the old 4-week dose
#     -> generator creates the next 7-week ET+TT obligation only
#     -> sweeper batches it into a shed drive/SOP task
#     -> Calendar, Action Center, Workflow, and Passport read the same next-dose work
#
# Prerequisites:
#   - local stack up: api :8080 and docker PG 127.0.0.1:55432 (`make dev-local`)
#   - migrations applied
#   - source-backed Nuance Rules config already authored/published by
#     `apps/admin-web/scripts/smoke-vaccination-authoring-live.mjs`
set -euo pipefail

export PATH="/opt/homebrew/opt/postgresql@15/bin:$PATH"
export GOATOS_ENV=local GOATOS_AUTH_MODE=bearer
export GOATOS_AUTH_ISSUER=goatos-local GOATOS_AUTH_AUDIENCE=goatos-api
export GOATOS_AUTH_HS256_SECRET="${GOATOS_AUTH_HS256_SECRET:-goatos-local-dev-secret-32-bytes-min}" GOATOS_AUTH_MAX_TOKEN_TTL=24h
export DATABASE_URL="${DATABASE_URL:-postgres://postgres:goatos@127.0.0.1:55432/goatos?sslmode=disable}"

PGURL="$DATABASE_URL"
API="${GOATOS_API_BASE_URL:-http://127.0.0.1:8080}"
TENANT="${GOATOS_TENANT_ID:-00000000-0000-4000-8000-000000000001}"
USER="${GOATOS_USER_ID:-90000000-0000-4000-8000-000000000101}"
ITEM="${GOATOS_VACCINE_ITEM_ID:-00000000-0000-4000-8000-00000000b001}"
PARK="${GOATOS_PARK_ID:-00000000-0000-4000-8000-000000003001}"
BACKEND="$(cd "$(dirname "$0")/../../backend" && pwd)"

psqlq(){ psql "$PGURL" -q -v ON_ERROR_STOP=1 -tAc "$1"; }
jqp(){ python3 -c "import sys,json;d=json.load(sys.stdin);print($1)" 2>/dev/null; }
has(){ grep -q "$1" <<<"$2" && echo HIT || echo MISS; }
fail(){ echo "FAIL $*" >&2; exit 1; }
assert_eq(){ local label=$1 want=$2 got=$3; [ "$want" = "$got" ] || fail "$label: got=$got want=$want"; }
assert_hit(){ local label=$1 needle=$2 haystack=$3; [ "$(has "$needle" "$haystack")" = HIT ] || fail "$label missing $needle"; }
assert_no_hit(){ local label=$1 needle=$2 haystack=$3; [ "$(has "$needle" "$haystack")" = MISS ] || fail "$label unexpectedly contains $needle"; }

STAMP=$(date +%s)
RUN_SECOND=$((STAMP % 60))
AS_OF=$(printf '2026-07-01T10:00:%02dZ' "$RUN_SECOND")
DOB="2026-05-13"
FOUR_WEEK_DUE="2026-06-10T00:00:00Z"
SEVEN_WEEK_DUE_DATE="2026-07-01"
ADMINISTERED_AT="2026-06-10T08:00:00Z"
VERIFIED_AT="2026-06-10T09:00:00Z"
SHED_CODE="TRUST-HIST-$STAMP"

echo "## vaccination-trusted-history-proof stamp=$STAMP api=$API as_of=$AS_OF"

echo; echo "### 0. find latest published ET+TT Nuance matrix row"
MATRIX_ROW=$(psqlq "
WITH latest AS (
  SELECT pv.protocol_version_id, pv.sop_version_id, pv.effective_from, pv.created_at
  FROM protocol_versions pv
  JOIN protocol_definitions pd
    ON pd.tenant_id = pv.tenant_id
   AND pd.protocol_id = pv.protocol_id
  WHERE pv.tenant_id = '$TENANT'
    AND pd.category = 'vaccination'
    AND pv.status = 'published'
    AND pv.rule_dsl->'vaccine'->>'code' = 'ET+TT'
    AND pv.rule_dsl #>> '{source,review_status}' = 'approved'
    AND pv.rule_dsl #>> '{source,source_system}' = 'vaccinations_db'
    AND lower(coalesce(pv.rule_dsl #>> '{source,source_ref}', '')) LIKE '%nuance%'
  ORDER BY pv.created_at DESC
  LIMIT 1
)
SELECT concat_ws('|',
       latest.protocol_version_id::text,
       latest.sop_version_id::text,
       pr4.rule_id::text,
       pr7.rule_id::text,
       pr4.dose_code,
       pr7.dose_code,
       pr4.offset_days::text,
       pr7.offset_days::text,
       latest.effective_from::text)
FROM latest
JOIN protocol_rules pr4
  ON pr4.tenant_id = '$TENANT'
 AND pr4.protocol_version_id = latest.protocol_version_id
 AND pr4.dose_code = 'et_tt_4w'
JOIN protocol_rules pr7
  ON pr7.tenant_id = '$TENANT'
 AND pr7.protocol_version_id = latest.protocol_version_id
 AND pr7.dose_code = 'et_tt_7w'
")
[ -n "$MATRIX_ROW" ] || fail "published Nuance ET+TT config not found; run the vaccination authoring smoke first"
IFS='|' read -r VERSION SOPVER RULE4 RULE7 DOSE4 DOSE7 OFFSET4 OFFSET7 EFFECTIVE_FROM <<<"$MATRIX_ROW"
[ -n "$VERSION" ] && [ -n "$SOPVER" ] && [ -n "$RULE4" ] && [ -n "$RULE7" ] || fail "incomplete Nuance ET+TT row: $MATRIX_ROW"
assert_eq "4-week dose code" "et_tt_4w" "$DOSE4"
assert_eq "7-week dose code" "et_tt_7w" "$DOSE7"
assert_eq "4-week offset" "28" "$OFFSET4"
assert_eq "7-week offset" "49" "$OFFSET7"
echo "VERSION=$VERSION SOPVER=$SOPVER RULE4=$RULE4 RULE7=$RULE7 effective_from=$EFFECTIVE_FROM"

echo; echo "### 1. seed K0 goat + accepted/verified 4-week vaccination history"
SHED=$(psqlq "
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, country, state_region, timezone, status)
VALUES (gen_random_uuid(), '$TENANT', 'shed', '$SHED_CODE', 'Trusted History Shed $STAMP', '$PARK', 'IN', 'Tamil Nadu', 'Asia/Kolkata', 'active')
RETURNING location_id
")
GOAT=$(psqlq "
INSERT INTO goats (
  goat_id, tenant_id, lifecycle_status, identity_state, custodian_party_id,
  current_location_id, park_id, shed_id, management_stage, sex, breed,
  dob, approx_dob, entry_date, health_status, reproductive_status, origin_type
) VALUES (
  gen_random_uuid(), '$TENANT', 'alive', 'clean', '00000000-0000-4000-8000-000000001001',
  '$SHED', '$PARK', '$SHED', 'K0', 'female', 'all',
  DATE '$DOB', DATE '$DOB', DATE '$DOB', 'healthy', 'open', 'birth'
) RETURNING goat_id
")
HISTORY_OBL=$(psqlq "
INSERT INTO obligation_instances (
  tenant_id, protocol_version_id, rule_id, target_type, target_id,
  scope_type, scope_id, due_at, status, idempotency_key, sequence, completed_at
) VALUES (
  '$TENANT', '$VERSION', '$RULE4', 'goat', '$GOAT',
  'shed', '$SHED', '$FOUR_WEEK_DUE'::timestamptz, 'completed',
  'trusted-history:$STAMP:et_tt_4w', 1, '$ADMINISTERED_AT'::timestamptz
) RETURNING obligation_id
")
COMPLETION=$(psqlq "
INSERT INTO vaccination_completions (
  tenant_id, obligation_id, goat_id, doses, dose_ml_given, route_site,
  adverse_reaction, cold_chain_verified, administered_at, status, verified_by,
  verified_at, recorded_by, idempotency_key
) VALUES (
  '$TENANT', '$HISTORY_OBL', '$GOAT', 1, 2, 'subcutaneous',
  false, true, '$ADMINISTERED_AT'::timestamptz, 'accepted', '$USER',
  '$VERIFIED_AT'::timestamptz, '$USER', 'trusted-history:$STAMP:completion:et_tt_4w'
) RETURNING completion_id
")
echo "GOAT=$GOAT SHED=$SHED_CODE/$SHED accepted_history_obligation=$HISTORY_OBL completion=$COMPLETION"

echo; echo "### 2. run real generation: suppress 4w, create next 7w"
GEN_OUT=$(cd "$BACKEND" && GOATOS_TENANT_ID="$TENANT" go run ./cmd/generate-vaccination-obligations \
  -tenant-id "$TENANT" \
  -version-id "$VERSION" \
  -unsafe-version-id-bypass-effective-resolution \
  -as-of "$AS_OF")
echo "$GEN_OUT"
assert_hit "generation suppression count" "suppressed_trusted=" "$GEN_OUT"

FOUR_ACTIVE_COUNT=$(psqlq "
SELECT count(*)
FROM obligation_instances
WHERE tenant_id = '$TENANT'
  AND target_id = '$GOAT'
  AND protocol_version_id = '$VERSION'
  AND rule_id = '$RULE4'
  AND obligation_id <> '$HISTORY_OBL'
")
assert_eq "4w active reissue count" "0" "$FOUR_ACTIVE_COUNT"

SEVEN_OBL=$(psqlq "
SELECT obligation_id
FROM obligation_instances
WHERE tenant_id = '$TENANT'
  AND target_id = '$GOAT'
  AND protocol_version_id = '$VERSION'
  AND rule_id = '$RULE7'
  AND status IN ('scheduled','due','deferred','in_progress')
ORDER BY created_at DESC
LIMIT 1
")
[ -n "$SEVEN_OBL" ] || fail "7-week next-dose obligation was not generated for goat=$GOAT"
SEVEN_DUE_DATE=$(psqlq "select due_at::date::text from obligation_instances where obligation_id='$SEVEN_OBL'")
assert_eq "7w due date" "$SEVEN_WEEK_DUE_DATE" "$SEVEN_DUE_DATE"
echo "SEVEN_OBLIGATION=$SEVEN_OBL due=$SEVEN_DUE_DATE"

echo; echo "### 3. sweeper batches the next dose into a shed drive/SOP task and Calendar projection"
( cd "$BACKEND" && GOATOS_TENANT_ID="$TENANT" go run ./cmd/obligation-sweeper \
  -tenant-id "$TENANT" \
  -version-id "$VERSION" \
  -sop-version-id "$SOPVER" \
  -vaccine-item-id "$ITEM" \
  -actor-id "$USER" \
  -calendar-limit 2000 2>&1 | tail -1 )
BATCH=$(psqlq "select batch_id from obligation_instances where obligation_id='$SEVEN_OBL'")
TASK=$(psqlq "select sop_task_id from obligation_batches where batch_id='$BATCH'")
[ -n "$BATCH" ] || fail "7-week obligation did not get a batch"
[ -n "$TASK" ] || fail "7-week batch did not get a SOP task"
echo "BATCH=$BATCH TASK=$TASK"

TOKEN=$(cd "$BACKEND" && go run ./cmd/mint-dev-token -tenant-id "$TENANT" -user-id "$USER" -ttl 2h 2>/dev/null)
A=(-H "Authorization: Bearer $TOKEN")

echo; echo "### 4. command/read surfaces show next-dose work, not old 4w flood"
AC=$(curl -s "${A[@]}" "$API/vaccination/action-center?shed_id=$SHED&protocol_version_id=$VERSION&due_after=2026-06-30T00:00:00Z&due_before=2026-07-02T00:00:00Z&limit=500")
RID=$(echo "$AC" | python3 -c 'import sys,json;d=json.load(sys.stdin);print(next((r.get("row_id","") for r in d.get("items",[]) if r.get("batch_id")=="'$BATCH'"),""))')
[ -n "$RID" ] || fail "Action Center row not found for batch=$BATCH"
WF=$(curl -s "${A[@]}" "$API/vaccination/workflows/$RID")
PP=$(curl -s "${A[@]}" "$API/goats/$GOAT/passport")
CAL=$(curl -s "${A[@]}" "$API/calendar/vaccination/events?shed_id=$SHED&date_from=$SEVEN_WEEK_DUE_DATE&date_to=$SEVEN_WEEK_DUE_DATE&limit=200")
CT=$(curl -s "${A[@]}" "$API/control-tower/vaccination")
PA=$(curl -s "${A[@]}" "$API/vaccination/adherence")

assert_hit "Action Center next-dose batch" "$BATCH" "$AC"
assert_hit "Action Center next-dose obligation" "$SEVEN_OBL" "$AC"
assert_no_hit "Action Center old trusted-history obligation" "$HISTORY_OBL" "$AC"
assert_hit "Workflow next-dose obligation" "$SEVEN_OBL" "$WF"
assert_hit "Workflow next-dose task" "$TASK" "$WF"
assert_hit "Passport goat" "$GOAT" "$PP"
assert_hit "Passport trusted history completion" "$COMPLETION" "$PP"
assert_hit "Passport next-dose obligation" "$SEVEN_OBL" "$PP"
assert_hit "Calendar next-dose batch" "$BATCH" "$CAL"
assert_no_hit "Calendar old trusted-history obligation" "$HISTORY_OBL" "$CAL"

printf '%-28s %s\n' "Calendar:" "shed_drive_batch=$(has "$BATCH" "$CAL") per_goat_next_obl=$(has "$SEVEN_OBL" "$CAL") old_4w=$(has "$HISTORY_OBL" "$CAL")"
printf '%-28s %s\n' "Action Center:" "row=$RID batch=$(has "$BATCH" "$AC") next_obl=$(has "$SEVEN_OBL" "$AC") old_4w=$(has "$HISTORY_OBL" "$AC")"
printf '%-28s %s\n' "Workflow:" "next_obl=$(has "$SEVEN_OBL" "$WF") task=$(has "$TASK" "$WF")"
printf '%-28s %s\n' "Passport:" "goat=$(has "$GOAT" "$PP") history=$(has "$COMPLETION" "$PP") next_obl=$(has "$SEVEN_OBL" "$PP")"
printf '%-28s %s\n' "Control Tower:" "summary_present=$(echo "$CT" | python3 -c 'import sys,json;d=json.load(sys.stdin);print("HIT" if d.get("summary") else "MISS")')"
printf '%-28s %s\n' "Protocol Adherence:" "summary_present=$(echo "$PA" | python3 -c 'import sys,json;d=json.load(sys.stdin);print("HIT" if d.get("summary") else "MISS")')"

echo; echo "## CLOSED trusted_history_goat=$GOAT suppressed_4w_obligation=$HISTORY_OBL next_7w_obligation=$SEVEN_OBL batch=$BATCH task=$TASK completion=$COMPLETION version=$VERSION"
