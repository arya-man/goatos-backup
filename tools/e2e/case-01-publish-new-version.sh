#!/usr/bin/env bash
# E2E case 01 — publish a new protocol version over a live cohort.
#
# Question under test (the one that matters operationally):
#   When a new vaccination plan version is published with a CHANGED schedule,
#     a. is already-completed work left alone?
#     b. is canceled work left alone?
#     c. is in-flight / deferred work left alone?
#     d. do FUTURE scheduled obligations move to the new dates?
#     e. or do old-version obligations survive alongside the new ones (duplicates)?
#
# Baseline is a clone of goatos-stg (see oci-db.sh). Every run resets first, so the
# case is repeatable and order-independent.
set -euo pipefail
cd "$(dirname "$0")/../.."

DB=${DB:-goatos_e2e}
TENANT=00000000-0000-4000-8000-000000000001
BASE_VERSION=7d5c2ccc-dca4-59ba-881a-267f61433df3   # V2, published, in the stg clone
NEW_VERSION=$(uuidgen | tr 'A-Z' 'a-z')
AS_OF=${AS_OF:-2026-08-20T00:00:00+05:30}

PW=$(grep -o 'postgres://postgres:[^@]*@' /Users/ravi/mesha/local-data/goatos-stg-to-oci/oci-goatos-db.env | head -1 | sed 's|postgres://postgres:||; s|@$||')
export PGPASSWORD="$PW"
URL="postgres://postgres:${PW}@127.0.0.1:15432/${DB}?sslmode=disable"
Q(){ psql -h 127.0.0.1 -p 15432 -U postgres -d "$DB" -v ON_ERROR_STOP=1 -tA -F'|' -c "$1"; }

say(){ printf '\n\033[1m%s\033[0m\n' "$*"; }

say "0. reset $DB from the stg-clone template"
./tools/e2e/oci-db.sh reset "$DB" >/dev/null

say "1. BASELINE — obligations by version and status"
Q "select coalesce(protocol_version_id::text,'(none)'), status, count(*)
   from obligation_instances group by 1,2 order by 1,2" | tee /tmp/e2e-before.txt

# capture identities so we can prove specific rows were untouched, not just counts
Q "select obligation_id||'|'||status||'|'||coalesce(due_at::text,'')
   from obligation_instances where status in ('completed','canceled')
   order by obligation_id" > /tmp/e2e-terminal-before.txt
echo "terminal rows captured: $(wc -l < /tmp/e2e-terminal-before.txt)"

Q "select obligation_id||'|'||coalesce(due_at::text,'')
   from obligation_instances where status='scheduled' order by obligation_id" > /tmp/e2e-sched-before.txt
echo "scheduled rows captured: $(wc -l < /tmp/e2e-sched-before.txt)"

say "2. retire V2 first — the DB refuses two live plans for the same scope
#    (exclusion constraint protocol_versions_published_no_overlap)"
# only `status` may change on a published row — effective_to is frozen by
# ensure_published_protocol_version_is_immutable(). The no-overlap exclusion is
# partial on status='published', so retiring frees the date range.
Q "update protocol_versions set status='retired', retired_at=now()
   where protocol_version_id='$BASE_VERSION'::uuid returning 'retired '||protocol_version_id"

say "3. create V3 — same plan, ET+TT revaccination 182d -> 91d (twice as often)"
Q "insert into protocol_versions (
     protocol_version_id, tenant_id, protocol_id, scope_type, scope_id, version, version_label,
     status, effective_from, rule_dsl, proof_policy, sop_version_id, drafted_by, published_by, published_at)
   select '$NEW_VERSION'::uuid, tenant_id, protocol_id, scope_type, scope_id, 3, 'V3 E2E faster ET+TT',
     'draft', current_date, rule_dsl, proof_policy, sop_version_id, drafted_by, published_by, now()
   from protocol_versions where protocol_version_id='$BASE_VERSION'::uuid
   returning 'created '||protocol_version_id"

Q "insert into protocol_rules (
     rule_id, tenant_id, protocol_version_id, dose_code, sequence, trigger_type, offset_days,
     due_window_days, min_gap_days, repeat, repeat_until_after_age, catch_up, eligibility_json,
     sop_version_id, proof_policy, withdrawal_days, sort_order)
   select gen_random_uuid(), tenant_id, '$NEW_VERSION'::uuid, dose_code, sequence, trigger_type,
     case when dose_code='et_tt_revac' then 91 else offset_days end,
     due_window_days,
     case when dose_code='et_tt_revac' then 91 else min_gap_days end,
     repeat, repeat_until_after_age, catch_up, eligibility_json, sop_version_id, proof_policy,
     withdrawal_days, sort_order
   from protocol_rules where protocol_version_id='$BASE_VERSION'::uuid"
Q "select 'rules copied: '||count(*) from protocol_rules where protocol_version_id='$NEW_VERSION'::uuid"

# rules may only be attached while the version is a DRAFT
# (ensure_protocol_child_version_is_draft). Publish is the last step, as in the app.
Q "update protocol_versions set status='published', published_at=now()
   where protocol_version_id='$NEW_VERSION'::uuid returning 'published '||protocol_version_id"

say "4. run the REAL generation service (same code path as the publish handler)"
cd backend
# GOATOS_PG_QUERY_TIMEOUT defaults to 3s, which is fine on a LAN and hopeless over
# an SSH tunnel to Mumbai. Raise it for the remote dev DB only.
DATABASE_URL="$URL" GOATOS_PG_QUERY_TIMEOUT=120s \
  go run ./cmd/generate-vaccination-obligations \
  -tenant-id "$TENANT" -as-of "$AS_OF" -timeout 45m 2>&1 | tail -25
cd ..

say "5. AFTER — obligations by version and status"
Q "select coalesce(protocol_version_id::text,'(none)'), status, count(*)
   from obligation_instances group by 1,2 order by 1,2" | tee /tmp/e2e-after.txt

say "6. ASSERTIONS"
Q "select obligation_id||'|'||status||'|'||coalesce(due_at::text,'')
   from obligation_instances where status in ('completed','canceled')
   order by obligation_id" > /tmp/e2e-terminal-after.txt

if diff -q /tmp/e2e-terminal-before.txt /tmp/e2e-terminal-after.txt >/dev/null; then
  echo "PASS  completed + canceled work is byte-identical (not one row moved)"
else
  echo "FAIL  terminal work changed:"; diff /tmp/e2e-terminal-before.txt /tmp/e2e-terminal-after.txt | head -20
fi

echo
echo "old-version scheduled work still open (duplicate risk):"
Q "select count(*) from obligation_instances
   where protocol_version_id='$BASE_VERSION'::uuid and status in ('scheduled','due','deferred')"

echo "new-version obligations created:"
Q "select count(*) from obligation_instances where protocol_version_id='$NEW_VERSION'::uuid"

echo
echo "same goat holding BOTH versions open at once:"
Q "select count(*) from (
     select target_id from obligation_instances
     where status in ('scheduled','due','deferred')
       and protocol_version_id in ('$BASE_VERSION'::uuid,'$NEW_VERSION'::uuid)
     group by target_id having count(distinct protocol_version_id) > 1) x"
