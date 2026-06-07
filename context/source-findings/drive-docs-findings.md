# Drive Source Findings

Derived from private Drive/export source material reviewed locally. This
document records only sanitized findings needed for Goat OS planning. Do not
commit raw Drive files, raw chat exports, screenshots, contact details, media
URLs, or machine-local paths.

## Source Material Reviewed

```text
Goat Passport Q&A
Goats and Parks
Goat Health Symptoms
Slack Modules Training
Buying and Transporting
Health Reports
Birth Reports
Shifting Reports
Death Reports
```

## Phase 1 Goat Passport Answers

### Core Site Codes

```text
CBE = Coimbatore park/farm code
CPT = Channapatna park/farm code
CBE previously appeared as CJB in older data.
CPT previously appeared as BLR in older data.
```

CBE and CPT are physical park locations and responsible operating units in the
current farm process. Current data treats them as one site each. Future parks in
the same broader place, such as Hindupur, should get distinct codenames instead
of reusing one code for multiple physical sites.

### Old Tag Scope

Old ear-tag numbers are not globally unique by themselves. The confirmed legacy
identity key is:

```text
old_tag_number + park_code
```

Examples:

```text
826 CBE and 826 CPT can be two different goats.
435 CBE and 435 CPT can both exist in Coimbatore after historic movement.
435 CBE cannot exist twice for two different goats.
```

Goat OS must not merge goats by old tag number alone. It must normalize historic
park aliases and preserve the source code used as evidence.

### RFID And First Import

RFID tagging is recent and contains clean mappings of old tag, RFID, breed, and
gender. The first import should seed RFID DB goats only. Tagless/event-log rows
come later as a separate reviewed import pass after more goats are RFID-tagged.

When RFID DB shed and latest DB event shed disagree, the latest DB event is the
more current placement signal because goats shift constantly and RFID DB shed
association may be stale. The importer should still preserve both pieces of
evidence and create a review/audit note for the disagreement.

### Holding Farms

`HF` means Holding Farm. A holding farm is a facility at the source where goats
are kept after procurement and before dispatch to core parks. Current answer:

```text
holding period: 2 to 8 weeks post procurement before dispatch
purpose: initial selection, tagging, and health SOP
current HF values: Rajasthan Farms, Gokul Agronomics, Goat World, Bhopal Agro
current classification: vendor/source whose farms serve as holding farms
future: Mesha may have owned holding parks
```

During partner holding, ownership is shared/pending in business terms because
Mesha has paid an advance but not the full amount. Do not seed these rows as
plain Mesha-owned or partner-owned without source-specific evidence.

### Origin Farm

`Origin Farm` means source/provenance: where the goat was purchased, born, or
originally sourced. It is not current physical location by itself and must not
set owner/custodian automatically.

### Status And Stage Meanings

Kid and fattening stages:

```text
K0 = newborn kids with mother, maximum about 1 day
K1 = milk training, separated from mother, maximum about 7 days
K2 = milk drinking after training, about 42 days / 6 weeks
K3 = weaning, milk ration cut and solid grains encouraged
F2 = fattening animals after weaning, on high-nutrition diet for weight gain
F2-Male/F2-Female = legacy grouping labels; F2 is the stage, Gender is sex truth
F0 = not used
Warmup = transition diet for purchased animals, usually about 14 days at parks
```

Adult female stages from current docs include:

```text
Non Pregnant
Flushing
Breeding
Pregnant Early Gestation
Pregnant Late Gestation
Mother
Mother Milking Waiting
Milking Warmup
Milking
```

`M0` is a mother/post-delivery management stage, not a kid growth stage.

### Sale And Routine Work Seeds

These are later policy inputs, not Phase 1 identity blockers:

```text
ICU / serious illness: restrict sale/allocation.
Quarantine / viral disease such as ORF: restrict sale/allocation.
Medication withdrawal period: future sale blocker once medicine tracking exists.
Kids <= K3: do not sell while still milk-drinking.
K and F kids: weigh every Monday.
Adult goats: weigh once monthly, currently on the 15th.
Vaccinations: cover all goats according to their schedule.
Feed changes: experiment-dependent and should be managed as configurable work.
```

## Slack Workflow Findings For Later Phases

### Health Flow

Current Slack health flow is disease/problem/treatment driven:

```text
Diagnosis form -> diagnosis video -> head/central diagnosis -> disease selection
-> one problem per disease per goat -> treatment schedule -> daily treatment
sessions -> video proof -> completion -> follow-up/close/extend.
```

Important rules:

```text
Diagnosis status must not become Completed before disease is selected.
One goat can have multiple diseases; create one problem per disease.
Problem due date follows Health SOP.
At due date, close if resolved or extend if disease persists.
Extending creates a new open problem and marks the old one Extended.
Treatment messages post daily until all problems are closed.
Some treatments have morning and evening sessions.
Evening session posts only if morning video proof is complete.
Every treatment action needs video proof.
All ICU goats should receive daily Follow Up symptom reports.
If a goat has no other open problem, it may shift out of ICU/Quarantine via a
shifting request/direction.
```

The health symptom form is rich and should become a first-class Goat OS form
schema. It covers goat status, rectal temperature, eyes, FAMACHA, nasal
discharge, ORF scabs, frothy mouth, eartag condition, skin coat, wounds, rashes,
lumps, left stomach, diarrhea, flystrike, udder, lactation, mastitis test, head
position, activity, leg injury, miscellaneous symptoms, and eating.

### Birth / Abortion Flow

Birth/abortion reports capture farm, type, mother ID, source shed, destination
shed, breed, time, kid count, kid gender, and comments. Birth actions are
generated separately for the mother and each kid.

Mother and kid actions are sequential:

```text
answer question or complete action -> upload proof video -> next action posts
```

Key birth actions include mother checks, ORS/water, mother medicine, kid cleaning,
iodine dipping, teeth check, suck reflex, first colostrum, and kid weight.

Colostrum:

```text
first colostrum happens in the birth action thread
sessions 2+ happen in the colostrum channel
daily session times: 7:00, 11:00, 15:00, 18:30, 22:00
eligibility depends on birth time
late-night births may get fewer Day 1 sessions and all sessions on Day 2
```

Next morning:

```text
kids shift to K1 before 9 AM
mothers shift to Mother or Mother Milking Waiting before 9 AM
central team raises shifting directions
milk team starts 300ml/session bottle feeding for newly shifted K1 kids
```

Legacy "delete wrong row/message" behavior must become void/reversal/audit in
Goat OS.

### Shifting Flow

Shifting reports are required whenever goats move between sheds so counts and
feed packing remain accurate. Fields include farm, type, category, priority,
goat IDs, breed, source shed, destination shed, and comments.

Types:

```text
Shifting Request = raised by ground team; requires park head/central authorization
Shifting Direction = raised by park head/central; automatically authorized
```

Categories:

```text
Health
Growth
Delivery
Breeding
```

Priority/due rules:

```text
High priority: due same day / urgent.
Low priority before 13:30: due tomorrow 09:00.
Low priority after 13:30: due day-after-tomorrow 09:00.
All scheduled shiftings must complete before 09:00 feeding.
```

Proof rule: upload video showing each shifted goat ID at the destination shed.
Central verifies request/direction, assignment, proof, and completion/cancelled
state.

### Death Flow

Death reports capture farm, goat ID, gender, breed, shed, and reason. Deceased
goat proof video must show goat ID, the goat from all directions, reproductive
tract for gender confirmation, and shed. Post-mortem video is requested by
central when needed and follows a specific organ checklist.

Wrong death reports are deleted in legacy; Goat OS must use void/reversal/audit.
Reports created yesterday should be completed at worst today.

### Workforce And Daily Control

Current operation uses vertical heads, assistant managers, park heads, and
central team. Vertical responsibilities include breeding, health, birth, and
infra. Feed packer and feed distributor are distinct duties.

Slack already provides an "Assigned to you" style task view across trackers.
Goat OS should preserve that operator-first mental model on Android.

Feed operations:

```text
08:30  feed directions for tomorrow sent
09:00  morning feed distribution starts
14:00  updated feed directions sent to account for shiftings
15:00  packed feed distributed/placed outside sheds for tomorrow
two feeding sessions: 09:00 and 15:00
current feed examples: Masoor Bhusa and Concentrate
```

### Procurement / Transport

Procurement docs contain seller discovery and transport practices. They support
later procurement/inventory/logistics phases. Do not commit private transport
contacts into public docs; summarize routes and policy only.

Operational hints:

```text
prefer nearby sellers with good service
intra-city movement uses local delivery apps
inter-city movement prefers bus parcel where routes exist, else courier
park-to-city and city-to-park handoffs depend on local pickup points and heads
```

The legacy procurement app and dashboards also show that procurement is more
than "create a goat row." It includes load creation, role-gated load management,
video upload/offline queue behavior, purchase cost, transport cost, landing
cost per kg, source/vendor comparison, and load-level sale comparison.

Goat OS implication:

```text
procurement load -> holding farm/source context -> transit/handoff proof
-> arrival gate -> discrepancy review -> accepted herd intake
-> inventory/cost/analytics
```

Do not skip the arrival gate. A purchased load should not become clean canonical
herd truth until count, identity, health, weight, media proof, and source/cost
evidence are reconciled.

### Crop / Fodder / Farmer Network

Legacy code contains a crop/fodder workflow family, not just goat-care forms.

Evidence:

```text
slack-automation-scripts/farmer_crops_automation.js
  farmer lookup and onboarding
  crop and seed details
  sowing records
  daily farmer/crop tasks
  crop status updates
  harvest fields
  crop expenditure
  proof/action workflow patterns

dashboard and vgoats-dashboard
  crop season / feed season data
  feed dashboards that depend on season/cost context
```

Goat OS implication:

```text
If Goat OS owns feed production or farmer coordination, add a crop/fodder module.
If crop/fodder stays outside Goat OS, define an explicit integration boundary
that feeds inventory and cost metrics.
```

This is not part of the first vaccination loop, but it is real scope for a full
farm operating system because feed availability and feed cost drive goat growth,
mortality, and unit economics.

### Cross-Cutting Proof / Verification

Legacy workflows repeatedly use proof media:

```text
procurement_app
  mobile video recorder and upload queue

feed_automation.js
  feed proof videos, transport proof, discrepancy handling

health_manager_attendance.js
  attendance/check-in media and assignment evidence

farmer_crops_automation.js
  crop/farmer action proof

procurement_db.js
  procurement response thread and video proof write-back

video_verification_system.js
  feed/video verification, stock/packing checks, rejection lists
```

Goat OS implication:

```text
Build proof capture, media metadata, verification routing, rejection/rework,
retention, and audit once as a platform capability.
Each module configures the proof policy; no module owns a one-off uploader.
```

## Build Implications

```text
Phase 1:
  import RFID DB only
  old_tag uniqueness = Number + Park/historic park alias
  latest DB event wins placement recency when RFID shed is stale, but preserve both
  HF rows create source/partner evidence and review ownership where needed

Phase 2:
  SOP/task engine must support generated tasks, due times, assignees, proof,
  verifier correction, backfill, and carry-forward.

Phase 3:
  health must model diagnosis, disease selection, problem, treatment sessions,
  follow-ups, proof-gated completion, close/extend, and ICU/Quarantine shifting.

Phase 5:
  movement/shifting and feed are tightly linked; shifting due rules exist because
  feed packing depends on the next day's shed counts.

Phase 5B:
  crop/fodder/farmer workflows are real legacy scope. Either build them as a
  module or define a clean external integration into feed inventory and feed cost.

Phase 6:
  birth/kidding is a real workflow with sequential proof-gated actions and
  colostrum scheduling.

Phase 7:
  procurement should model holding farms, vendor/source, advance-paid/shared
  ownership state, transit/handoff proof, arrival gate, discrepancy review,
  intake health SOP, and landing cost.

Phase 8:
  sale/allocation blocking should use health, quarantine, milk-drinking kid
  stage, and later medication withdrawal rules.

Phase 10:
  analytics should include unit economics as governed metrics: landing cost,
  feed cost, health/treatment cost, mortality loss, realized margin, and cost
  per goat/kg/load/source. Do not leave these as per-dashboard formulas.
```
