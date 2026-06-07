# Phase 1 Legacy Discovery Proposals

Status: derived proposal for business-owner review.

This file is the read-only discovery output for Phase 1. It does not contain
raw goat rows, raw Slack payloads, tokens, media URLs, or private sheet data.
It only records aggregate counts, source paths, column names, and decisions
that still need human approval before migrations/import logic.

## Plain-English Summary

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

They must be farm-scoped.

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

Proposal: keep `display_id` separate from immutable `goat_id`. Generate it
server-side after the first import policy is approved.

Still needs business approval:

```text
display_id format:
  example options: GOAT-000001, CBE-000001, CPT-000001, or another business format
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
old tag scope: farm
auto-link: only within the approved scope and only when supporting evidence agrees
conflict: duplicate old tag in same scope goes to review
unknown scope: review, never silently global
```

Reason: old tags are proven to repeat across farms. Global old-tag uniqueness
would merge real goats incorrectly.

Source-system and load/source should stay as evidence columns, but they should
not replace farm as the first identity scope unless business rules later prove
farm is too broad.

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
  pregnant, non_pregnant, mother, milking, buck, warmup, unknown

growth_class / cohort_tag
  K0, K1, K2, K3, M0, F0, F2-Male, F2-Female, unknown

health_overlay
  ICU, ICU-Kid, ICU-Non-Pregnant, quarantine, none/unknown
```

Reason: dashboard/status labels are operationally useful, but they are not one
clean lifecycle enum.

Still needs business/ops approval:

```text
whether K0/K1/K2/K3/F2/M0/F0 are official growth classes, shed tags, or both
official canonical labels for UI/reporting
which labels are allowed to block sale or trigger SOPs
```

Normalizer rule for identifiers:

```text
strip numeric .0 suffixes
collapse No Tag / No tag / none / blank into missing_identifier
do not treat missing_identifier as a shared goat identity
```

## Owning Farm / Location Proposal

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

Proposal:

```text
owning_farm_id:
  CBE and CPT map to core farm owners
  Holding Farm / HF rows map to staging/procurement source context until approved

current_location:
  Farm + Shed + Shed Tag when available
  source rows missing location go to explicit unknown/staging location, not blank

location precedence for first import:
  RFID DB Shed/Shed Tag for RFID-linked goats
  latest DB event Dst Shed/Dst Shed Tag for event-derived current position
  dashboard CSV only for aggregate reconciliation
```

Still needs business approval:

```text
is Origin Farm the stable owning farm, the source/vendor farm, or both by case?
is Holding Farm an owning farm, a procurement staging bucket, or vendor/source context?
for conflicts, should RFID DB current shed outrank latest DB event current shed?
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

3. Old tags are farm-scoped, not globally unique.
   Evidence: 54 normalized Old Tag IDs span CBE and CPT in RFID DB;
   583 DB Goat ID values span more than one farm.
   Proposal: scope old_tag by farm.

4. Dashboard CSV is reporting output, not canonical import source.
   Proposal: use for reconciliation only.

5. DB sheet is event history, not current goat table.
   Proposal: import to event staging/reconciliation, not direct canonical identity.

6. RFID DB is the best first source for tag/RFID identity staging.
   Proposal: use it as first identity mapping sample, then reconcile against DB + dashboards.
```

## Decisions Still Needed From Business Owner

These cannot be safely derived from the files alone:

```text
1. First identity migration source
   Approve: RFID DB first, then DB/dashboard reconciliation?
   Or name another source/tab as current active herd truth.

2. Display ID format
   Choose the human-readable format operators/admins will see.

3. Old tag scope
   Approve proposal: farm.
   If old tag reuse happens by load/vendor instead, say that now.

4. Origin Farm and Holding Farm / HF meaning
   Is Origin Farm the stable owner, source/vendor origin, or both by case?
   Is Holding Farm an owning farm, procurement staging, vendor/source context, or all of these by case?

5. Merge approval authority
   Proposal: central admin can approve merges; park head can request/recommend.

6. Temporary goat minimum evidence
   Proposal: source row/load + owning/staging farm + location/unknown location
   + created_by + reason + photo/proof if available.

7. Current location precedence
   Approve which wins when RFID DB and latest DB event disagree.

8. Status model
   Approve keeping lifecycle, reproductive status, growth/cohort tag, and health overlay separate.

9. First-import scope
   Seed only the RFID DB goats first, or also mint temporary identities from
   the larger DB event population in the first import?
```

## Phase 1 Start Decision

```text
Contracts can start now.

Migrations, import seeds, identifier policies, and canonical import logic should
wait until the business owner confirms the decision list above.
```

This is not a blocker to Phase 1. It is the first Phase 1 step before the build
session writes schema defaults that would be expensive to unwind.
