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

Founder/CXO tenant grants must also exist for:

- `ravi@mesha.sg`
- `manohark@mesha.sg`
- `manju@mesha.sg`
- `abhishek@mesha.sg`
- `aryaman@mesha.sg`

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

## Capacity Rules

- `operator_capacity.default_animals_per_day = 200`.
- Capacity unit is distinct animals per operator per business date.
- Dose count is not capacity.
- With `active_operators_per_day = 1`, final drive capacity is 200 animals/day.
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
| 2026-07-24 | ET+TT | Darshan Talwar | 200 | 10 |
| 2026-07-25 | ET+TT | Darshan Talwar | 10 | 0 |

PPR must not be planned anywhere by this seed packet for now.

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
```

The last query must return zero rows.

## Automatic Failures

Treat the validation as failed if any of these happen:

- the checkout SHA is not latest `origin/main` (now machine-enforced by
  `make seed-checkout-staleness-gate`, which runs before any DB mutation;
  `GOATOS_ALLOW_STALE_SEED_CHECKOUT=1` bypasses it and voids the proof);
- a throwaway roster override is used;
- Amit is missing, uncapped, or has no shift config;
- Sagar week-off is anything other than Saturday;
- any clean planned date/operator has more than 200 distinct animals;
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
- the report relies on UI screenshots before DB rows.

## Separation Of Proofs

Run these as separate steps:

1. Clean reseed proof: seed, sweep, query, compare to this file.
2. Mutation proof: leave/default/cap cascade tests in a separate disposable DB.
3. UI proof: only after DB proof is green.

Never run a leave/cascade mutation on the same DB and then claim the resulting
rows are the clean seed schedule.
