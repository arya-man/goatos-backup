# CPT Local DB Reseed Validation Contract

This file is the exact validation contract for the CPT vaccination rehearsal.
Use it before any local/staging seed proof. Do not replace it with a verbal
interpretation, screenshots, or a throwaway override.

## Source Files

The source bundle is this fixture directory:

- `raw/CPT-Adult-goats.json`
- `raw/CPT-Adult-vaccination.json`
- `raw/CPT_Nuanced Timetable.xlsx`
- `cpt-operator-roster.json`
- `expected-drive-schedules.json`

The live wiki copies may be used only after proving they are the intended latest
source and copying/normalizing them into this same contract shape. A validation
run that uses only the goat/vaccination JSON files and omits
`cpt-operator-roster.json` is invalid.

## Canonical Reseed Command

```bash
DATABASE_URL='postgres://.../goatos?sslmode=disable' \
GOATOS_ENV=local \
GOATOS_TENANT_ID='00000000-0000-4000-8000-000000000001' \
make seed-vaccination-cpt-operator-drive
```

The raw filenames above are NOT what the validator and seed commands read: they
require a normalized root-level bundle (`goats.json`, `vaccination.json`,
`attendance-jun-26.json`, `timetable-goats-team-v1.json`,
`roster-name-mapping.jun26-review.csv`,
`shed-manager-mapping.jul11-vaccination.csv`). `materialize-source.mjs`
performs that transformation deterministically from `raw/` plus
`cpt-operator-roster.json` into `build/cpt-operator-drive-source` (gitignored,
outside `fixtures/`, because it carries reviewed runtime staff names and receives
the seed's audit output). The HRMS files are DERIVED from the roster contract, so
the contract stays the single source of truth for operators, week-offs, and caps.
Do not hand-author them.

The target refuses to mutate the database from a checkout that is not clean
`origin/main` (`make seed-checkout-staleness-gate`), and its closeout runs the
DB-proving `check-expected-drive-schedules.mjs` gate against
`expected-drive-schedules.json`.

## Required HRMS Seed Shape

Seed all of these people exactly:

| Person | Required role | Executes vaccination | Week off | Cap |
|---|---|---:|---|---:|
| Amit Kumar | vaccination operator | yes | Friday | 200 |
| Darshan Talwar | vaccination operator | yes | Sunday | 200 |
| Sagar Mahoor | vaccination operator | yes | Saturday | 200 |
| Chandrakant | preventive care director | no | none | 0 |

Each vaccination operator must also be provisioned for Android login after the
database seed finishes:

| Person | Android login email source | Password rule |
|---|---|---|
| Amit Kumar | `operators[].email_hint` in `cpt-operator-roster.json` | unique temporary password or individual reset flow |
| Darshan Talwar | `operators[].email_hint` in `cpt-operator-roster.json` | unique temporary password or individual reset flow |
| Sagar Mahoor | `operators[].email_hint` in `cpt-operator-roster.json` | unique temporary password or individual reset flow |

Do not seed a common operator email, a shared field password, or a CEO/CXO login
for Android execution. Plaintext passwords must never be committed to this
fixture, runbook, screenshots, shell history, or seed evidence. The valid cloud
path is to create/verify one Firebase/Auth email-password account per operator
and send/record an individual reset flow; the valid local proof is to show each
operator identity reaches Android bootstrap with its own email.

Founder/CXO tenant grants, leadership profiles, and STG login paths must also
exist for:

- `ravi@mesha.sg`
- `manohark@mesha.sg`
- `manju@mesha.sg`
- `abhishek@mesha.sg`
- `aryaman@mesha.sg`

For STG, these 5 leadership users are not SSO-only. Each one must have Google
SSO and Firebase email/password available, active `ceo_internal` grant, active
leadership profile, admin-web bootstrap, mobile bootstrap, and CEO AI access.
They never add vaccination operator capacity.

Forbidden HRMS shapes:

- Do not remove Amit.
- Do not seed a two-operator override.
- Do not change Sagar week-off to Monday.
- Do not leave any vaccination operator without `vaccination_daily_animal_cap`.
- Do not leave any vaccination operator without `vaccination_operator_shift_config`.
- Do not finish seed validation before unique Android login provisioning is
  complete for Amit, Darshan, and Sagar.
- Do not reuse one shared operator password across Amit, Darshan, and Sagar.
- Do not turn Chandrakant into vaccination execution capacity.
- Do not finish STG validation with only pending grants, only Firebase users, or
  only admin-web login. All 9 users must pass the canonical login verification.
- Do not mark leadership complete with Google SSO only; the 5 leadership users
  also need Firebase email/password for STG.

## Capacity Rules

- `operator_capacity.default_animals_per_day = 200`.
- Capacity unit is distinct animals per operator per business date.
- Dose count is not capacity.
- With `active_operators_per_day = 1`, final drive capacity is 200 animals/day.
- The CPT `2026-07-25` ET+TT seed catch-up is the only exception in this
  packet: Darshan may be scheduled for 210 remaining ET+TT animals because 114
  ET+TT animals were already completed on `2026-07-24`. This is a seed/proof
  fixture exception, not a production cap change.
- No future drive may use 210 unless its source fixture explicitly declares a
  `seed_catchup_overrides` row.
- HRMS raw availability can still be 400 or 600:
  - 2 available operators * 200 = 400 raw available field capacity.
  - 3 available operators * 200 = 600 raw available field capacity.
- Raw availability does not override the final N=1 drive assignment.

## Drive Assignment Rules

`cpt-operator-roster.json.default_operator_assignment` is mandatory:

- `active_operators_per_day = 1`
- default operator = Darshan
- primary fallback = Sagar only when Darshan is unavailable/off
- secondary fallback = Amit only when Darshan and Sagar cannot cover
- drive grain = business date
- shift labels are fallback identity only, not vaccine timing

Darshan is available on Friday 2026-07-24 and Saturday 2026-07-25. Sagar must
not be selected on those dates just because he is also available; N=1 means
Darshan gets the drive while available.

## Animal Source Validation

Expected source shape:

| Check | Expected |
|---|---:|
| total source rows | 324 |
| CPT/Channapatna animals imported | 324 |
| excluded from import | 0 |
| source health deferred animals | 0 |
| DB `recovering` goats | 0 |
| DB `under_treatment` goats | 0 |

Expected physical shed totals:

| Shed | Animals |
|---|---:|
| Gandhi | 114 |
| Godel 1 | 120 |
| Godel 2 | 32 |
| Mandela 2 | 47 |
| Old Yashoda | 11 |

Health normalization:

- source `Closed` means resolved/healthy.
- source `Fine` means healthy.
- neither `Closed` nor `Fine` may become `recovering`.
- historical diagnosis text in `disease_*` fields is not current health gating.

## Expected Due Counts To Report

The validation report must include every seeded vaccine family generated from
the source/rules. PPR is intentionally excluded from this CPT seed packet for
now; this is not a global vaccination protocol rule change.

Minimum expected due/history counts from the current CPT source discussion:

| Dose code | Expected shape |
|---|---|
| `et_tt_adult_w1` | 324 completed history |
| `et_tt_adult_w2` | 114 completed history dated 2026-07-24; 210 remaining due by 2026-07-24 |
| Blue Tongue | report separately by planned date/operator/animal count |
| FMD | report separately by planned date/operator/animal count |
| HS | report separately by planned date/operator/animal count |
| Goat pox | report separately by planned date/operator/animal count |
| Sheep pox | report separately by planned date/operator/animal count |

Do not hide non-PPR obligations. Do not mix them into the ET+TT final proof
table.

## Final Expected ET+TT Drive Schedule

This is the final validation scenario.

| Date | Vaccine | Operator | Animals | Remaining |
|---|---|---|---:|---:|
| 2026-07-25 | ET+TT | Darshan Talwar | 210 | 0 |

The 210 row is allowed only by the packet's explicit one-time
`seed_catchup_overrides` entry. The configured operator cap remains 200 for all
normal planning and subsequent drives.

PPR must not be planned anywhere by this seed packet for now.

## Known Reseed Failure Modes

These are not theoretical. They were the failure modes found while landing this
packet and must stay guarded before any STG seed:

- **Midnight/as-of drift:** closeout must use the reviewed as-of
  `2026-07-24T00:00:00+05:30` for generation and sweeping. A command run after
  midnight must not silently shift the packet to `2026-07-25`.
- **210 is not the normal cap:** standing operator cap remains 200. The only
  allowed 210 is Darshan on `2026-07-25` for the ET+TT seed catch-up row declared
  in `seed_catchup_overrides`.
- **False-green exact-row checks:** a proof that only finds
  `2026-07-25 / Darshan / ET+TT / 210` is insufficient. It must also prove there
  are no extra ET+TT W2 rows on later dates and no duplicate open goat+dose
  assignment rows.
- **Adult entry-date singleton trap:** adult campaign rows must not split into
  one-animal fragments only because adults arrived on different source entry
  dates. Adult blank-history animals join the normal physical shed/partition
  campaign. Adult same-vaccine history remains authoritative. Kid/young DOB
  timing stays strict.
- **PPR packet boundary:** PPR is excluded only from this CPT seed packet. Do not
  delete or mutate the global vaccination rules to make the packet pass.
- **History visibility:** `2026-07-24` ET+TT rows are completed history, not
  pending work. They must be visible through goat passport/register reads for the
  114 completed animals.
- **STG identity/platform wiring:** 5 leadership users need both Google SSO and
  Firebase email/password, 3 operators plus 1 director need Firebase
  email/password, all 9 need active backend grants/profiles/bootstrap, CEO AI
  must be gated to leadership, and GCS/evidence storage must point to
  `goatos-stg`.
- **Clean proof isolation:** do not run leave/cap/date-move cascade experiments
  on the same database before claiming clean reseed proof.

## Required DB Proof Queries

Run read-only SQL after reseed and before any mutation/cascade test:

```sql
select count(*) from goats;

select coalesce(health_status::text, '<NULL>') as health_status, count(*)
from goats
group by 1
order by 1;

select wm.display_name, wp.position_code, wp.week_off_weekday,
       wp.vaccination_daily_animal_cap, v.shift_label, v.week_off_weekday
from workforce_members wm
join workforce_positions wp on wp.workforce_member_id = wm.workforce_member_id
left join vaccination_operator_shift_config v on v.operator_id = wm.workforce_member_id
where wp.position_code like '%vaccination%'
order by wm.display_name;

select c.active_operators_per_day, wm.display_name as default_operator
from vaccination_operator_assignment_config c
left join workforce_members wm on wm.workforce_member_id = c.default_operator_id;

with rows as (
  select b.planned_date, wm.display_name as operator, pr.dose_code,
         oi.target_id, b.batch_id, b.status
  from obligation_batches b
  join obligation_instances oi on oi.batch_id = b.batch_id
  join protocol_rules pr on pr.rule_id = oi.rule_id
  left join workforce_members wm on wm.workforce_member_id = b.conducted_by
  where oi.target_type = 'goat'
)
select planned_date, operator,
       string_agg(distinct dose_code, ',' order by dose_code) as dose_codes,
       count(distinct target_id) as unique_animals,
       count(*) as obligations,
       count(distinct batch_id) as batches,
       string_agg(distinct status, ',' order by status) as statuses
from rows
group by planned_date, operator
order by planned_date, operator;

with rows as (
  select b.planned_date, b.conducted_by, oi.target_id
  from obligation_batches b
  join obligation_instances oi on oi.batch_id = b.batch_id
  where b.status = 'planned' and oi.target_type = 'goat'
)
select planned_date, conducted_by, count(distinct target_id) as unique_animals
from rows
group by planned_date, conducted_by
having count(distinct target_id) > 200
order by planned_date;

-- ET+TT W2 history/open shape. Expected:
-- completed on 2026-07-24 = 114
-- scheduled/open remaining = 210
select pr.dose_code, oi.status, oi.due_at::date, count(distinct oi.target_id)
from obligation_instances oi
join protocol_rules pr on pr.rule_id = oi.rule_id
where pr.dose_code = 'et_tt_adult_w2'
group by 1, 2, 3
order by 3, 2;

-- Final ET+TT assignment shape. Expected exactly one row:
-- 2026-07-25 | Darshan Talwar | et_tt_adult_w2 | 210
select vda.planned_date::date,
       coalesce(wm.display_name, '(unassigned)') as operator,
       pr.dose_code,
       count(distinct m.goat_id) as animals
from vaccination_drive_assignments vda
join obligation_batches ob on ob.tenant_id = vda.tenant_id and ob.batch_id = vda.batch_id
join vaccination_drive_assignment_members m on m.tenant_id = vda.tenant_id and m.assignment_id = vda.assignment_id
join obligation_instances oi on oi.tenant_id = m.tenant_id and oi.obligation_id = m.obligation_id
join protocol_rules pr on pr.rule_id = oi.rule_id
left join workforce_members wm on wm.workforce_member_id = vda.operator_id
where ob.status <> 'superseded'
  and pr.dose_code = 'et_tt_adult_w2'
group by 1, 2, 3
order by 1, 2, 3;

-- Duplicate open goat+dose assignment guard. Expected: 0 rows.
select pr.dose_code,
       m.goat_id,
       count(distinct m.assignment_id) as open_assignment_count
from vaccination_drive_assignment_members m
join obligation_instances oi on oi.tenant_id = m.tenant_id and oi.obligation_id = m.obligation_id
join protocol_rules pr on pr.tenant_id = oi.tenant_id and pr.rule_id = oi.rule_id
join obligation_batches ob on ob.tenant_id = oi.tenant_id and ob.batch_id = oi.batch_id
where oi.status in ('scheduled', 'due', 'missed')
  and ob.status <> 'superseded'
group by 1, 2
having count(distinct m.assignment_id) > 1
order by 1, 2;

-- Passport/register history spot checks. Expected:
-- completed-114 sample 901007000504553 has accepted rows on 2026-06-30 and 2026-07-24.
-- pending-210 samples 901007000503935 and 901007000504370 have prior accepted history
-- and ET+TT W2 scheduled on 2026-07-25.
select gi.identifier_value,
       g.display_id,
       vc.administered_at::date,
       vc.status,
       vc.obligation_id
from goat_identifiers gi
join goats g on g.tenant_id = gi.tenant_id and g.goat_id = gi.goat_id
left join vaccination_completions vc on vc.tenant_id = g.tenant_id and vc.goat_id = g.goat_id
where gi.identifier_value in ('901007000504553', '901007000503935', '901007000504370')
order by gi.identifier_value, vc.administered_at;
```

The cap-breach query and duplicate-open-assignment query must return zero rows.
The final ET+TT assignment query must return only the one Darshan/210 row.

## Automatic Failures

Treat the validation as failed if any of these happen:

- the checkout SHA is not latest `origin/main` (now machine-enforced by
  `make seed-checkout-staleness-gate`, which runs before any DB mutation;
  `GOATOS_ALLOW_STALE_SEED_CHECKOUT=1` bypasses it and voids the proof);
- a throwaway roster override is used;
- Amit is missing, uncapped, or has no shift config;
- Sagar week-off is anything other than Saturday;
- any clean planned date/operator has more than 200 distinct animals;
  except the explicit Darshan `2026-07-25` ET+TT seed catch-up override of 210;
- ET+TT W2 appears as more than one open assignment per goat;
- ET+TT W2 appears in extra planned rows after the final `2026-07-25` Darshan
  210 row;
- the 114 completed ET+TT W2 rows dated `2026-07-24` are not visible as accepted
  vaccination history through goat passport/register reads;
- PPR appears anywhere in this CPT seed packet output;
- non-PPR vaccines are omitted from the report;
- superseded empty batches are used as schedule proof;
  (the cap, operator fan-out, contract-operator, pre-business-date, forbidden-park,
  shell-batch, and operator-shift-config failures above are now enforced in
  `seed-closeout` by `check-expected-drive-schedules.mjs` and FAIL the closeout;
  the prohibited-PPR and vaccine-family report rows are checked when
  `GOATOS_EXPECTED_DRIVE_VARIANT` names the applied variant);
- leave/cascade mutation runs on the same DB before clean reseed proof;
- a test says PASS but executed zero tests;
- operators share one Android login or one shared password;
- any operator can execute vaccination in HRMS but cannot log into Android with
  their own provisioned identity after seed;
- Chandrakant cannot log in with his own Firebase email/password identity or
  appears as vaccination execution capacity instead of director capacity;
- any of the 5 leadership users lacks either Google SSO, Firebase email/password,
  active `ceo_internal` grant, active profile, admin-web bootstrap, mobile
  bootstrap, or CEO AI access;
- CEO AI/chatbot is visible to non-leadership users, hidden from leadership
  users, or wired to local/dev/prod dependencies instead of STG;
- GCS/evidence storage uses local/dev/prod placeholders, missing buckets, or
  service accounts without STG object permissions;
- the report relies on UI screenshots before DB rows.

## Separation Of Proofs

Run these as separate steps:

1. Clean reseed proof: seed, sweep, query, compare to this file.
2. Mutation proof: leave/default/cap cascade tests in a separate disposable DB.
3. UI proof: only after DB proof is green.

Never run a leave/cascade mutation on the same DB and then claim the resulting
rows are the clean seed schedule.
