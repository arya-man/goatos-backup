# Phase 1 Legacy Discovery Proposals

Status: derived proposal for business-owner review.

This file is the read-only discovery output for Phase 1. It does not contain
raw goat rows, raw Slack payloads, tokens, media URLs, or private sheet data.
It only records aggregate counts, source paths, column names, and decisions
that still need human approval before migrations/import logic.

## Summary

We can start Phase 1 contract work now. We should not write final database
migrations, import seeds, identifier policies, or canonical import logic until
the business owner confirms the few policy decisions at the end of this file.

The legacy data is not one clean goat table:

- `private herd workbook` `DB` looks like goat event history: purchase, birth, shifting,
  sale, death, abortion.
- `private herd workbook` ` RFID DB` is the best current RFID/old-tag mapping source
  found so far, but it covers fewer rows than the dashboard aggregate count.
- dashboard CSVs are aggregate reporting projections, not goat-level identity
  truth.
- Slack/App Script files hold important SOP/workflow behavior that later phases
  must preserve.

The safe plan is: import legacy sources into staging/reconciliation, never
silently merge goats from tag similarity, and require approval for risky
identity decisions.

## Inputs Inspected

```text
<mesha-workspace>/source-material/private-data/private herd workbook
<mesha-workspace>/dashboard/public/data/*.csv
<mesha-workspace>/vgoats-dashboard/public/data/*.csv
<mesha-workspace>/dashboard/app/api/**/*
<mesha-workspace>/dashboard/lib/**/*
<mesha-workspace>/vgoats-dashboard/app/api/**/*
<mesha-workspace>/vgoats-dashboard/lib/**/*
<mesha-workspace>/slack-automation-scripts/**/*.{js,ts}
<mesha-workspace>/procurement_app/src/**/*.{ts,tsx}
```

## Source Findings

### `private herd workbook` `DB`

Purpose observed: event/history table, not a clean unique goat identity table.

Rows: `12,419`

Columns:

```text
Date, Farm, Event, Goat ID, Parent ID, Shifting ID, Gender, Src Shed, Dst Shed,
Dst Shed Tag, Teeth/Age, Breed, Birth Weight, Birth Time, Shifting Type,
Shifting Category, Shifting Priority, Comments, Weight, Load ID
```

Event counts:

```text
Shifting 8744
Purchase 1701
Birth    957
Sale     661
Death    298
Abortion 58
```

Farm counts:

```text
CBE 7516
CPT 4608
holding/procurement sources 295
```

Identifier risk:

```text
Goat ID non-empty rows:       12,392
Goat ID unique values:        2,911
Goat ID duplicate values:     1,714
Rows in duplicate Goat IDs:   11,195
```

Conclusion: `Goat ID` in this sheet is not safe as global `goat_id`. Treat it
as a legacy identifier/source value with scope and evidence.

Additional cross-farm evidence:

```text
DB Goat ID values spanning more than one farm: 583
```

Conclusion: legacy numeric goat/tag values must not be globally unique.

### `private herd workbook` ` RFID DB`

Purpose observed: best RFID/old-tag mapping source found so far.

Rows: `1,349`

Columns:

```text
Farm, Origin Farm, Old Tag ID, RFID, Breed, Gender, Shed, Shed Tag, Age
```

Farm counts:

```text
CBE 755
CPT 594
```

Identifier risk:

```text
Old Tag ID non-empty rows:       1,167
Old Tag ID unique values:        1,100
Old Tag ID duplicate values:     65
Rows in duplicate Old Tag IDs:   132

RFID non-empty rows:             1,349
RFID unique values:              1,347
RFID duplicate values:           2
Rows in duplicate RFID values:   4
```

Conclusion: RFID should be globally unique as a policy, but the current data
already has conflicts. One duplicate is a full RFID-looking value; another is
a short value in the RFID field and should be handled by format validation.
Conflicts must go to review, not silent linking.

Old tags are not globally unique. Normalized old tags appear in both CBE and
CPT.

```text
Old Tag ID values spanning more than one farm in RFID DB: 54
```

They must be scoped by normalized park code, not by tag number alone.

### Dashboard CSVs

Files:

```text
<mesha-workspace>/dashboard/public/data/counting_db_with_holding_dev.csv
<mesha-workspace>/vgoats-dashboard/public/data/counting_db_with_holding_dev.csv
```

Rows: `121` each.

Columns:

```text
date, farm, shed, shed_tag, breed, age, goat_count, staff, shed_name
```

Aggregate count:

```text
total goat_count: 1952
CBE: 848
CPT: 649
Holding Farm: 455
```

Conclusion: this is a reporting/count projection. It is useful for reconciliation
and dashboard validation, but it is not a goat-level identity import source.

### `Active-Goats-List`

Rows: `1,259`.

Purpose observed: derived active-goat summary across CBE/CPT sections.

Conclusion: useful as a reconciliation/reference source, not canonical identity
truth by itself.

### Validation Sheet

Rows: `94`.

Purpose observed: allowed values/reference lists for farms, events, genders,
breeds, and sheds.

Conclusion: useful as a seed/reference candidate for controlled vocabularies,
but the final canonical list still needs approval.

## Proposed Phase 1 Source Strategy

Use each legacy source for the thing it is strongest at:

```text
RFID DB
  primary candidate for RFID + old tag mapping import staging

DB
  event/history evidence for purchase, birth, shifting, sale, death, abortion
  not a unique goat identity table

dashboard CSVs
  aggregate reconciliation target for counts by farm/shed/shed_tag/breed

Active-Goats-List
  secondary reconciliation/reference source

Validation
  controlled-vocabulary seed candidate
```

Do not import straight into canonical goat identities. First import into staging,
normalize, diff, match deterministically, then route uncertain records to
reconciliation queues.

## Identifier Proposals

### Immutable Goat ID

Proposal: Goat OS generates immutable internal `goat_id` values. Legacy tags,
RFIDs, old tags, and sheet values attach as identifiers/evidence.

Reason: legacy `Goat ID` and `Old Tag ID` are not clean unique primary keys.

### Display ID

Locked answer: keep `display_id` separate from immutable `goat_id`. Generate it
server-side as a global, easy-to-search code.

```text
display_id format:
  default G-000001 style global code
  never encode current farm/location because goats can move
  RFID, old tag, QR, visual tag, shed, and breed remain searchable aliases
```

### RFID Policy

Proposal:

```text
active RFID scope: global
auto-link: only if no conflict exists
conflict: any duplicate active RFID opens RFID conflict review
format validation: reject or review non-RFID-looking values stored in the RFID field
```

Reason: RFID should behave globally unique in the future, but current data has
a small number of duplicate RFID values.

### Old Tag Policy

Proposal:

```text
old tag scope: old_tag_number + normalized park_code
auto-link: only within the approved scope and only when supporting evidence agrees
conflict: duplicate old tag in same scope goes to review
unknown scope: review, never silently global
historic aliases: CJB -> CBE, BLR -> CPT; preserve original source code as evidence
```

Reason: ops confirmed old tags were effectively `Number + Park`. `826 CBE` and
`826 CPT` can be two different goats, while `826 CBE` cannot occur twice for two
different goats. Global old-tag uniqueness would merge real goats incorrectly.

## Lifecycle / Status / Cohort Proposal

Current values blend lifecycle, breeding status, growth class, health overlay,
sex-specific fattening class, and operating cohort. `Shed Tag` / `Dst Shed Tag`
is not an identifier field. Do not import `Shed Tag` as `old_tag` or any goat
identity value.

Do not force all of these values into one `lifecycle_status`.

Observed high-volume labels include:

```text
Pregnant, Non-Pregnant, Mother, Warmup, Milking, Buck
K0, K1, K2, K3, M0, F0, F2-Male, F2-Female
ICU, ICU-Non-Pregnant, ICU-Kid, Quarantine Kids
```

Proposal:

```text
lifecycle_status
  alive, dead, sold, unknown

reproductive_status
  pregnant, non_pregnant, mother, milking, buck, unknown

growth_cohort_tag
  K0, K1, K2, K3, F2, unknown

management_stage
  warmup, unknown

health_status
  ICU, quarantine, none/unknown
```

Reason: dashboard/status labels are operationally useful, but they are not one
clean lifecycle enum.

Display/import rule:

```text
legacy labels remain searchable, but canonical status fields are structured
F2-Male maps to growth_cohort_tag=F2 only; sex comes from the source Gender column
F2-Female maps to growth_cohort_tag=F2 only; sex comes from the source Gender column
Fattening maps to growth_cohort_tag=F2
F0 is not used; if found, preserve raw label and route to mapping review
if F2-Male/F2-Female disagrees with the source Gender column, preserve both values and route to review
if Gender is blank/unknown and only the F2 label implies sex, keep sex needs-review
ICU-Non-Pregnant maps to health_status=ICU and reproductive_status=non_pregnant
ICU-Kid maps to health_status=ICU plus the known kid/growth stage when available
Warmup maps to management_stage=warmup
M0 maps to reproductive_status=mother and management_stage=m0_post_delivery
UI should show friendly display labels such as "K0 - Newborn" or "F2 - Fattening"
and may show the short legacy code as a chip
unknown labels stay as raw_label in review until mapped
```

Ops-confirmed label meanings:

```text
K0: newborn stage with mother, maximum about one day
K1: milk training after separation from mother, maximum about seven days
K2: milk drinking after training, about 42 days / six weeks
K3: weaning to solid feed
M0: mother post-delivery / mother shed stage
F2-Male / F2-Female: F2 fattening group labels; F2 is the stage, while
Male/Female is a legacy grouping suffix and not authoritative sex evidence
Warmup: 2-8 week source holding period before dispatch, plus about 14-day park
transition diet after arrival
```

Later policy/data enrichment, not Phase 1 blockers:

```text
official canonical labels for UI/reporting
sale/allocation rules are seeded from Drive docs but become configurable later policies
non-health status/stage changes that create routine tasks become configurable later SOP policies
```

Normalizer rule for identifiers:

```text
strip numeric .0 suffixes
collapse No Tag / No tag / none / blank into missing_identifier
do not treat missing_identifier as a shared goat identity
```

## Ownership / Custody / Location Proposal

Observed farm values:

```text
CBE
CPT
Holding Farm
HF - Rajasthan Farms
HF - Gokul Agronomics
HF - Goat World
HF - Bhopal Agro
```

Ops-confirmed meanings:

```text
CBE: Coimbatore farm/location code
CPT: Channapatna farm/location code
Historic aliases: CJB -> CBE, BLR -> CPT
HF: Holding Farm, external vendor/source site used 2-8 weeks after purchase and before dispatch to core parks
Origin Farm: where the goat was purchased from, born, or originally sourced
```

Proposal:

```text
tenant / owner / custodian:
  seed one Mesha tenant
  seed Mesha as the first org party
  RFID-first goats in CBE/CPT can seed Mesha ownership/custody when source evidence agrees
  Holding Farm / HF rows map to procurement/source/holding context first
  advance-paid but not fully-paid partner-held goats are shared/pending ownership review
  do not treat HF labels as full owner/custodian truth automatically

current_location:
  Farm + Shed + Shed Tag when available
  source rows missing location go to explicit unknown/staging location, not blank

location precedence for first import:
  RFID DB Shed/Shed Tag for RFID-linked goats
  latest DB event Dst Shed/Dst Shed Tag for event-derived current position
  dashboard CSV only for aggregate reconciliation
  if RFID DB and latest DB event disagree for the same goat, latest DB event is
  current placement because RFID shed can be stale; preserve both pieces of
  evidence and create reconciliation/review
```

Later data enrichment, not Phase 1 blockers:

```text
official addresses/geo details for CBE and CPT
official addresses/geo details for any HF partner represented as a physical holding location
future same-city parks must receive distinct codes/location rows
```

## SOP / Workflow Inventory From Slack and Mobile

This is not all Phase 1 implementation, but it proves the current operating
system has many workflows that later phases must preserve.

### Slack/App Script

Detected workflow families:

```text
counting_db_automation.js
  Goats DB event projection, shifts, births, next-day counts, unreported shifting detection

shifting_death_automation.js
  shifting reports, death reports, shed lookup, scheduled shifting dates

health_manager_attendance.js
  health manager daily schedule, attendance, farm-specific lists/channels,
  media proof, auto-assignment

health_db_automation.js
  adult/kid health forms, diagnosis, treatment SOPs, follow-up, problem rows

feed_automation.js
  feed packing, feed direction, transport, consumption and wastage, consolidated reports

video_verification_system.js
  feed/video verification, rejection lists, stock/packing checks

procurement_db.js
  procurement response sheet, Slack message thread, video prompt, video link write-back

unified_automation.js / unified_automation_val_town.ts
  action/task engine behavior, Redis-backed state, video verification,
  feed, milk, shifting, reports, pending/completed status, modal flows

farmer_crops_automation.js / farming_unified_val_town.ts
  action/task workflow pattern, forms, reschedule, head approve/reject,
  auto video modal, Redis-backed state

history_Automation.js
  goat history PDF/report generation from BigQuery and Slack
```

Health follow-up behavior already found in legacy:

```text
health_db_automation.js uses these sheets:
  DB, Diagnosis Form, Problem, Follow Up, Adults SOP, Kids SOP

diagnosis/health forms:
  abnormal symptoms create a Diagnosis Report and request video in Slack
  goat_status is captured/displayed, but it is not the main task trigger
  for female goats, mother/pregnant/lactating status controls lactation fields

problem creation:
  diagnosis or abnormal follow-up creates a Problem row
  problem is marked Open and linked to goat_id, diagnosis/follow-up evidence,
  farm, age group, assignee, due date, and Slack message

treatment/follow-up schedule:
  Adults SOP / Kids SOP drive the plan by disease, adult/kid age group,
  day number, and session such as Morning/Afternoon/Evening
  generated rows include treatment/action/medicine/follow-up style work

follow-up completion:
  when a follow-up form is submitted for the goat/date, matching Scheduled
  Follow Up rows are marked Completed

problem extension:
  unresolved/extended open problems can create a later extension problem
  rather than silently disappearing
```

Meaning for Goat OS:

```text
health diagnosis follow-up rules are legacy-derived enough for later Health/SOP phases
we do not need ops to explain the basic health follow-up engine again
Drive source docs also seed routine work:
  K/F kids weigh every Monday
  adult goats weigh monthly on the 15th
  vaccinations run by schedule
  feed changes are experiment-driven configuration
remaining later work is converting these into configurable SOP/status policies,
not rediscovering the current practice from scratch
```

Implication for Goat OS:

```text
SOP/task engine, workforce assignment, proof capture, video verification,
follow-up scheduling, and Slack bridge are real product surfaces, not optional extras.
```

### Procurement Mobile App

Observed stack:

```text
React Native 0.85
Firebase Auth / Firestore / Messaging / Storage
MMKV upload queue
Vision Camera
React Navigation
Zustand
SOP editor screen
video recorder/playback
role checks for procurement head / manager / assistant manager
```

Useful patterns to carry forward:

```text
offline-ish upload queue
role-gated load/goat actions
SOP overlay and video capture flow
phone OTP login
manual ID duplicate checks
```

Not canonical for Goat OS:

```text
Firestore data model
Firebase as operational truth
hardcoded procurement-only roles
```

## Decisions That Are Evidence-Backed Enough To Propose

These should be approved/corrected by the business owner, not rediscovered from
scratch:

```text
1. DB Goat ID is not globally unique canonical identity.
   Proposal: import as scoped legacy identifier/evidence.

2. RFID should be globally unique going forward.
   Proposal: conflicts open review; no silent overwrite.

3. Old tags are park-scoped, not globally unique by number alone.
   Evidence: Drive Q&A confirms old tags were Number + Park; source discovery
   also found repeated numbers across CBE/CPT.
   Proposal: scope old_tag by normalized park code and preserve historic alias evidence.

4. Dashboard CSV is reporting output, not canonical import source.
   Proposal: use for reconciliation only.

5. DB sheet is event history, not current goat table.
   Proposal: import to event staging/reconciliation, not direct canonical identity.

6. RFID DB is the best first source for tag/RFID identity staging.
   Proposal: use it as first identity mapping sample, then reconcile against DB + dashboards.
```

## Locked Answers And Remaining Ops Clarifications

These decisions decide how old Sheets/dashboard meanings become clean Goat OS
truth. Some answers are now locked; the remaining items need operational meaning
from the current farm process.

### Terms Used Below

Canonical term definitions live in:

```text
context/product/glossary.md
```

Important for this discovery:

- `CBE` is the Coimbatore farm/location code; `CJB` is its older code in source data. `CPT` is the Channapatna farm/location code; `BLR` is its older code in source data. Official addresses and geo details can be backfilled later.
- `HF` / `Holding Farm` means an external vendor/source holding place used after purchase and before dispatch to core parks. Advance-paid partner-held goats can be shared/pending ownership until fully settled.
- `Origin Farm` means where the goat was purchased from, born, or originally sourced.
- `source row`, `load`, `tenant`, `party`, `owner`, `custodian`, `staging`, `current location`, `display ID`, and `merge` are defined in the glossary.

### Locked Answers

```text
1. Display ID
   Locked answer: use an easy global human code, defaulting to G-000001 style.
   It must not include farm/location because goats can move.

2. First identity migration source
   Meaning: RFID DB is the cleanest goat registry; DB/dashboard files are history or derived views.
   Locked answer: use RFID DB first, then DB/dashboard reconciliation.
   First import scope: RFID DB only. Tagless/event rows come in a later pass as
   temporary/review identities.

3. Old tag scope
   Meaning: old tag numbers repeat across parks and historic code aliases.
   Locked answer: old tags are scoped by old_tag_number + normalized park_code,
   not globally unique by number alone.

4. Owner / custodian / location separation
   Meaning: this is about the goat, not where a person lives.
   Locked answer: owner, custodian, and physical goat location are separate concepts.

5. Merge approval
   Meaning: merge mistakes corrupt identity history.
   Locked answer: central admin can approve and assign approval roles. Farm admins
   can approve only if granted role/scope. Park heads/operators can request or
   recommend. Mass merge approval needs preview, evidence sampling, and dry-run.

6. Temporary goat
   Meaning: temporary goats are for before tag/RFID, or when tags are missing/lost/dirty.
   Locked answer: field-created temporary goats require photo/proof; import-created
   temporary goats require source row evidence. All temporary goats remain
   needs_review until linked to stronger evidence, and stale unresolved temp
   goats escalate to review.

7. Status structure
   Meaning: legacy status strings mix multiple meanings.
   Locked answer: separate lifecycle, reproductive status, growth/cohort tag, and health status.

8. CBE / CPT / HF meanings
   Meaning: current data uses short site/source codes.
   Locked answer: CBE is Coimbatore farm/location. CPT is Channapatna farm/location.
   HF means Holding Farm: external vendor/source holding places used 2-8 weeks
   after procurement and before dispatch to main parks.
   Modeling answer: create minimal external party records for known HF partners in
   Phase 1. Create physical location records only where the source indicates a
   real holding location. Do not assume full goat ownership from the HF label;
   advance-paid partner-held goats stay shared/pending review.

9. Origin Farm
   Meaning: RFID DB has Origin Farm values such as BLR, CBE, CJB, CPT, and Gokul.
   Locked answer: Origin Farm means where the goat was purchased from, born, or
   originally sourced. Store it as source/origin evidence, not current location
   or owner/custodian by itself.

10. Current location conflict
   Meaning: RFID DB has Shed/Shed Tag; DB event log has latest Dst Shed/Dst Shed Tag.
   Locked answer: latest DB event is the current placement signal because RFID
   DB shed can be stale. Preserve both values and create reconciliation/review.

11. K-stage, M0, F2, and Warmup meanings
   Meaning: these are operating stages, not identity tags.
   Locked answer: K0 is newborn with mother, max about one day; K1 is milk
   training, max about seven days; K2 is milk drinking, about 42 days / six
   weeks; K3 is weaning to solid feed; M0 is mother post-delivery / mother shed;
   F2-Male and F2-Female are post-weaning fattening group labels; F2 is the
   stage and source Gender remains sex evidence. F0 is not used. Warmup can be
   source holding before dispatch and a park transition diet after arrival.
```

### Future Ops Inputs Not Blocking Phase 1

```text
1. Status label semantics
   Current legacy state: labels include K0/K1/K2/K3, Pregnant, Non-Pregnant,
   Mother, Milking, Buck, F2-Male, F2-Female, ICU, ICU-Non-Pregnant,
   Quarantine kids, and others.
   Known from Drive source docs: K0/K1/K2/K3/M0/F2/Warmup meanings are captured
   above. ICU/serious illness, Quarantine/viral disease, medication withdrawal
   periods, and milk-drinking kids up to K3 are sale/allocation blockers.
   K/F kids weigh every Monday; adult goats weigh monthly on the 15th; all goats
   receive vaccinations by schedule; feed changes are experiment-driven.
   Known from legacy code: health diagnosis/follow-up tasks are driven by
   Diagnosis Form, Problem, Follow Up, Adults SOP, and Kids SOP. They are
   disease, age-group, day, and session based. They are not primarily driven
   by status labels like K0/K1/K2/Pregnant.
   Phase 1 handling:
     store the raw legacy label
     map known labels into lifecycle/reproductive/growth/management/health axes
     keep unknown or dirty labels reviewable
     do not hardcode sale-blocking or task-trigger behavior in Phase 1
   Needed for later policy phases:
     which labels are official reporting labels versus old/dirty labels
     convert known sale blockers, weighing rhythms, vaccination schedules,
       medicine withdrawal, feed experiments, and any future stage-triggered work
       into configurable status_rule_policies / SOP rules

2. Geo details
   Current legacy state: CBE is Coimbatore and CPT is Channapatna. HF values are
   external holding/source places. Known current sites are one physical site each
   for now; future same-city parks need distinct codes/location rows.
   Phase 1 handling:
     create location rows with known code/city/state
     keep exact address, pincode, and coordinates nullable
     do not block import on missing GPS
   Needed later:
     official address, district, pincode, coordinates, and timezone for CBE/CPT
     and any HF location that should become a physical location.
```

## Phase 1 Start Decision

```text
Contracts can start now.
Migrations can start now.
Import seeds, identifier policies, and canonical import logic can start now
using the locked Phase 1 decisions.

Unanswered sale/allocation rules, non-health routine task triggers, and exact
GPS/address details are later policy/data enrichment inputs. They must not be
hardcoded as guesses, but they do not block Phase 1 identity import.
```
