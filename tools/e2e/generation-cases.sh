#!/usr/bin/env bash
# Vaccination obligation generation — the data-level cases.
#
# These are separated from tools/e2e/suite.mjs on purpose. The browser suite is
# fast because it never generates; generation walks the whole in-care cohort and,
# against a remote database, takes tens of minutes. Mixing them would make the
# fast suite unrunnable.
#
#   ./tools/e2e/generation-cases.sh seed     reset + insert the fixture animals
#   ./tools/e2e/generation-cases.sh generate run the real generation service
#   ./tools/e2e/generation-cases.sh assert   check what generation produced
#
# The fixture animals are deliberately awkward:
#   G-900001  born 30 days ago  -- its 4-week dose came due 2 days ago
#   G-900002  NO date of birth  -- must not be given a birth-age schedule
set -euo pipefail

cd "$(dirname "$0")/../.."
: "${E2E_PG_PASSWORD:?set E2E_PG_PASSWORD (see tools/e2e/e2e.env.example)}"
export PGPASSWORD="$E2E_PG_PASSWORD"

HOST="${E2E_PG_HOST:-127.0.0.1}"
PORT="${E2E_PG_PORT:-15432}"
DB="${E2E_DB:-goatos_e2e}"
TENANT="${E2E_TENANT_ID:-00000000-0000-4000-8000-000000000001}"

q() { psql -h "$HOST" -p "$PORT" -U postgres -d "$DB" -v ON_ERROR_STOP=1 -tA -F'|' -c "$1"; }

case "${1:-}" in
  seed)
    ./tools/e2e/e2e-db.sh reset "$DB" >/dev/null
    q "
    with src as (select tenant_id, custodian_party_id, farm_id, park_id, shed_id, breed_id
                 from goats where lifecycle_status='alive' and shed_id is not null and species='goat' limit 1)
    insert into goats (goat_id, tenant_id, display_id, species, sex, dob, dob_estimated,
                       lifecycle_status, health_status, management_stage, custodian_party_id,
                       farm_id, park_id, shed_id, breed_id, entry_date, origin_type,
                       row_version, created_at, updated_at)
    select gen_random_uuid(), s.tenant_id, v.tag, 'goat', 'female', v.dob, false,
           'alive', 'healthy', 'F2', s.custodian_party_id, s.farm_id, s.park_id, s.shed_id, s.breed_id,
           current_date - 30, 'birth', 1, now(), now()
    from src s, (values ('G-900001', (current_date - 30)::date), ('G-900002', null::date)) as v(tag, dob)
    returning display_id;"
    q "select 'baseline obligations: '||count(*) from obligation_instances" ;;

  generate)
    # The timeout is generous because every animal costs a round trip; a short one
    # fails midway and leaves a half-generated cohort that reads like a bug.
    ( cd backend && DATABASE_URL="postgres://postgres:${E2E_PG_PASSWORD}@${HOST}:${PORT}/${DB}?sslmode=disable" \
      GOATOS_ENV=local GOATOS_PG_QUERY_TIMEOUT=3600s GOATOS_ALLOW_STALE_LOCAL_STACK=1 \
      go run ./cmd/generate-vaccination-obligations -tenant-id "$TENANT" -timeout 3600s ) ;;

  assert)
    echo "== G-900002 (no date of birth) must NOT get a birth-age schedule =="
    q "select r.dose_code, r.trigger_type, o.due_at::date
       from goats g join obligation_instances o on o.target_id=g.goat_id
       join protocol_rules r using (rule_id)
       where g.display_id='G-900002' order by o.due_at;"
    echo
    echo "== G-900001 (born 30 days ago) =="
    q "select r.dose_code, r.trigger_type, o.due_at::date, (o.due_at::date - g.dob) as days_after_dob, o.status
       from goats g join obligation_instances o on o.target_id=g.goat_id
       join protocol_rules r using (rule_id)
       where g.display_id='G-900001' order by o.due_at;"
    echo
    echo "== a future birth-age dose is never due EARLIER than its rule's offset =="
    # Dates are read in Asia/Kolkata, the tenant's operating calendar. Reading these
    # timestamps in UTC shifts a due day across midnight and manufactures ~50 false
    # positives -- the failure is in the lens, not the data.
    # Spacing can only push a dose LATER, so "earlier" is the real invariant; equality
    # is not, because live-to-live gaps, course gaps and capacity splitting legitimately
    # move a date out. A DOB corrected after generation also explains a mismatch, and is
    # excluded here so the check shows only what is genuinely unexplained.
    q "select count(*) as unexplained_early_doses
       from goats g join obligation_instances o on o.target_id=g.goat_id
       join protocol_rules r using (rule_id)
       where r.trigger_type='birth_age' and g.dob is not null and o.status='scheduled'
         and ((o.due_at at time zone 'Asia/Kolkata')::date - g.dob) < r.offset_days
         and g.updated_at <= o.created_at;"
    echo
    echo "== the deadline lands on the calendar day the rule says (baseline 2597) =="
    # What an operator experiences is the DAY a dose stops being accepted, so the
    # comparison is on India calendar days.
    #
    # Neither a date cast nor an interval comparison is clean here: stored spans carry
    # odd time components (09:30, 15:00), so the interval check reports 4240 and the
    # calendar check 2597. Both numbers are IDENTICAL in the untouched staging baseline,
    # so this is pre-existing behaviour, not something the plan console introduced. It is
    # asserted as a baseline so it cannot grow unnoticed while the real semantics are
    # settled with Preventive Care.
    q "select count(*) as deadlines_on_the_wrong_day
       from obligation_instances o join protocol_rules r using (rule_id)
       where o.window_end is not null
         and (o.window_end at time zone 'Asia/Kolkata')::date
             <> ((o.due_at at time zone 'Asia/Kolkata')::date + r.due_window_days);"
    echo
    echo "== no animal may hold future work under two different plan versions =="
    q "select count(*) as animals_with_work_under_two_versions from (
         select o.target_id, r.dose_code
         from obligation_instances o join protocol_rules r using (rule_id)
         where o.status='scheduled' and o.due_at > now()
         group by 1,2 having count(distinct o.protocol_version_id) > 1) d;"
    echo
    echo "== nor the SAME dose twice under ONE version =="
    # 222 in the staging baseline before any change of ours: a pre-existing defect,
    # tracked in docs/decisions/ADR-vaccination-obligation-identity.md. Asserted here so
    # the number cannot grow unnoticed.
    q "select count(*) as same_dose_duplicated_within_one_version from (
         select target_id, rule_id, protocol_version_id from obligation_instances
         where status='scheduled' group by 1,2,3 having count(*) > 1) d;"
    echo
    echo "== counts =="
    q "select status, count(*) from obligation_instances group by status order by 1;" ;;

  *) sed -n '2,16p' "$0"; exit 1 ;;
esac
