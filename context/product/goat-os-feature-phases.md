# Goat OS Product Feature Phases

This is the product rollout in plain Goat OS language.

Engineering rails like repo setup, contracts, CI, dev/stg/prod, analytics, and
load testing run underneath every phase. They are not the product. The product
is the goat operating system described here.

For the mapping from older planning docs and `goatos-categories.md` into these
phases, read `context/product/goat-os-coverage-map.md`.

## Workforce Operations Is Cross-Cutting

This is not payroll HRMS. Goat OS needs an operations workforce engine from the
first real workflow.

It answers:

```text
Who is responsible for this goat/work today?
Which team is on shift?
Who is the park head/supervisor?
Who verifies the proof?
If the assigned operator is absent, who backfills?
If work is overdue, who gets escalated?
What proof shows the work was actually done?
```

This starts in Phase 2 for vaccination and becomes a full module in Phase 4.
The same engine later assigns feeding, health follow-ups, weighing, movement,
breeding checks, device inspections, and verification work.

Backfill is not "give it to anybody." Goat OS must pick a backup by skill,
park/shed scope, current load, and task risk. The new assignee and park head
must both be notified. If no qualified backup exists, the task escalates to
the park head instead of disappearing.

## Proof, Media, And Verification Are Cross-Cutting

Slack automation and the procurement mobile app already use photo/video proof in
many places: feed, health, shifting, procurement, death, attendance, and
verification. Goat OS should not rebuild proof upload separately for every
module.

The shared proof engine answers:

```text
What media is required for this action?
Who must verify it?
Is the proof attached to one goat, a batch, a task, or a load?
Can AI pre-check it?
When can raw video expire?
What audit trail proves the work was accepted, rejected, or reworked?
```

Phase 2 uses this engine for vaccination proof. Phase 3 uses it for health,
treatment, death, and abortion. Phase 4 uses it for attendance and operator
entry logs. Phase 7 uses it for procurement load, transit, and arrival proof.
Phase 8 uses it for dispatch/exit proof.

## Promise Safety Is A Cross-Phase Vertical Slice

Goat OS must eventually answer a simple customer-facing question:

```text
Can we safely promise this exact goat for this exact delivery/event date, and
can we prove the answer from source records?
```

This is not one module. It is a vertical slice across the product:

```text
Phase 1: stable goat passport, identifiers, merge redirects, source evidence,
         decision records, audit trail, and movement history.
Phase 3: health, treatment, quarantine, death, and medicine withdrawal periods.
Phase 5: trusted weight, movement/shifting history, feed records, and growth.
Phase 8: sellable/readiness policy, allocation, booking, double-book
         prevention, replacement/substitution, dispatch, and exit proof.
Phase 10: governed dashboards and promise-risk metrics.
```

The Phase 1 base does not itself perform festival/customer allocation. It makes
that later allocation safe by giving every later phase one immutable `goat_id`,
reviewable identity conflicts, source-backed decision records, and an audited
movement trail. At scale, the later booking invariant must be enforced by the
database, not by UI checks or in-memory locks.

This is stronger than a one-time report. Goat OS must keep the promise safe as
facts change: a goat may move, get sick, receive medicine, be exposed to a bad
feed batch, gain or lose weight, or be merged into another identity after the
initial booking. Those changes emit events, recalculate eligibility/risk, create
review tasks when needed, and notify the right people. A promise is not just
"safe when booked"; it must remain monitored until dispatch/exit.

Continuous monitoring means a real scheduled and event-triggered worker, not a
dashboard someone remembers to refresh. Later phases must scan open
bookings/allocations/promises until dispatch or exit, re-run the delivery-date
readiness policy after critical facts change, write decision evidence, and create
remediation/replacement tasks when the answer changes. The job must be
idempotent, paginated by scope/date, and indexed so it never scans the full herd
in memory.

Future promise-safety acceptance cases must include:

```text
identity:
  retagged/stale-tag evidence, duplicate records across systems, transferred
  goats, ambiguous references, and no-silent-merge review.
booking:
  double promises, phantom/unresolved promises, void promises that should not
  reserve, booked-dead goats, booked-ineligible goats, and ambiguous booked refs.
eligibility:
  delivery-date health/quarantine/withdrawal/age/weight rules, including exact
  boundary dates and no-trusted-weight review.
telemetry/pricing:
  double-occupancy spikes, implausible drops, stale readings, unresolved tag
  reads, trusted-weight pricing, promised-weight shortfall, and booked_at price
  audit.
feed:
  historical movement trace through contaminated shed/date windows, not current
  shed only; uncleared exposure blocks allocation/substitution; cleared exposure
  does not block forever.
field reports:
  report-only death/treatment/tag-correction facts enter proof/review, while
  ambiguous low-confidence text never becomes truth automatically.
listings:
  offered goats already booked, ineligible, or unresolved are blocked/reviewed.
lineage:
  impossible parentage is preserved as genetics/data-quality evidence.
```

## Phase 1: Goat Passport And Herd Registry

Build the identity layer for every goat.

What users get:

- Every goat has one permanent Goat OS passport.
- Old tag, RFID, visual ID, breed, age, sex, status, farm, shed, cohort, and
  current lifecycle state are visible in one place.
- Existing Sheet/XLSX goat records are imported.
- Duplicate/missing/dirty tag cases go to reconciliation instead of silently
  becoming wrong truth.
- Admin can search a goat and see its basic timeline.

In short:

```text
First we create the Aadhaar/passport system for goats.
No task, vaccine, sale, breeding, or genetics data is reliable until the goat
identity is reliable.
```

Two-dev split:

```text
Dev A builds the goat identity database, import logic, duplicate detection,
reconciliation rules, and passport APIs.

Dev B builds the admin search/profile screens, goat timeline UI, dirty-data
review screen, and mobile goat lookup/scan UI.
```

## Phase 2: SOP Tasks And Vaccination

Build the first real field workflow.

What users get:

- Admin creates vaccination SOP/schedule.
- System generates due vaccination tasks.
- Workforce/roster assigns vaccination work to operators for the week.
- If an operator is absent, the task backfills to a qualified backup by
  vaccinator skill, park/shed scope, and current load.
- Backfill notifies the new assignee and park head; if no eligible backup
  exists, it escalates instead of silently dropping the work.
- Park head can see incomplete/overdue vaccination work by operator/team/shed.
- Operator sees assigned tasks on Android.
- Operator scans/selects goat, fills the form, uploads photo/video proof.
- Park head verifies on ground.
- Central verifier verifies proof video/photo.
- Approved vaccination becomes canonical goat history.
- Missed/pending tasks move forward with reason.
- Task views preserve the current Slack mental model: "assigned to me", due
  today, open, completed, and verifier correction/rework.

In short:

```text
This replaces Slack vaccination forms with a real Goat OS task loop:
assign -> execute -> proof -> verify -> close.
```

Two-dev split:

```text
Dev A builds SOP config, task generation, assignment, vaccination submit API,
proof media APIs, idempotency, and verification records.

Dev B builds the admin SOP/task screens, Android task list, vaccination form
runner, camera proof capture, offline retry states, and verifier queue UI.
```

## Phase 3: Health, Treatment, Death, And Verification

Build the health incident system.

What users get:

- Sick goat reporting.
- Diagnosis form with symptom fields and proof video.
- Disease selection by health head/park head/central team before diagnosis closes.
- One open problem per diagnosed disease per goat.
- Treatment records and daily treatment sessions.
- Medicine dosage and withdrawal tracking.
- Recovery follow-ups.
- Death/abortion records.
- ICU/Quarantine follow-up and shifting-out workflow once no open problem remains.
- Legacy health rules preserved: diagnosis/follow-up form, disease selection by
  head/central, one problem per disease per goat, daily treatment sessions,
  close/extend decisions, and daily ICU follow-up symptom reports.
- Mortality views by breed, farm, shed, age group, source batch.
- Verification queue for high-risk events.

In short:

```text
Anything bad happening to a goat is recorded with proof, treatment, follow-up,
and accountability.
```

Two-dev split:

```text
Dev A builds diagnosis/problem/treatment/death event tables, disease mapper
integration seams, medicine/withdrawal rules, treatment-session task generation,
close/extend problem rules, verification gates, and health APIs.

Dev B builds sick-goat reporting forms, treatment follow-up screens, death/
abortion proof flows, health symptom form UI, verifier review screens, and
mortality dashboard views.
```

## Phase 4: Workforce, Park Operations, And Daily Control

Build the operating layer for people managing goats. This is Goat OS workforce
ops, not payroll HRMS.

What users get:

- Park -> head/supervisor -> team -> operator structure.
- Role mapping: operator, feeder, vaccinator, vet, verifier, park head, admin.
- Weekly roster and daily shift schedule.
- Team ownership by park, shed, cohort, task type, or campaign.
- Absence marking and automatic fallback/backfill.
- Backfill selection by skill match, park/shed scope, current load, and task risk.
- Notifications to the new assignee and park head when ownership changes.
- No-eligible-backup escalation to park head instead of silent reassignment.
- Overdue escalation based on due time even when absence was never marked.
- Backup owner rules for each duty: feeding, vaccination, health follow-up, weighing, movement, breeding check, verification.
- Task queues by operator, team, park head, vet, verifier.
- Handovers when work shifts from one operator/team to another.
- Escalations for overdue tasks by SLA and risk.
- Operator entry log and attendance/check-in proof where required.
- Completion, rejection, rework, and missed-task history.
- Park-level work completion dashboard.
- Current operating roles supported: vertical head, assistant manager, health
  manager, farm/park head, central team, feed packer, feed distributor, verifier.

In short:

```text
This tells who is responsible for which goats, who is on duty today, who covers
if someone is absent, and which goat-care work is still pending.
```

Two-dev split:

```text
Dev A builds workforce tables, role/scope rules, roster, shifts, absence,
skill/scope/load-aware fallback/backfill, task assignment, handover,
notifications, no-backup escalation, and audit history.

Dev B builds roster management screens, operator workload views, park-head task
queues, absence/backfill screens, overdue/escalation views, and operator entry logs.
```

## Phase 5: Growth, Feed, Weight, And Movement

Build day-to-day goat performance tracking.

What users get:

- Live weight history.
- ADG/growth trend.
- Body condition.
- Feed plans, feed consumption, and feeding task assignment.
- Feed conversion.
- Feed supply linkage to inventory, procurement, and crop/fodder outputs.
- Daily feeding completion by operator/team/shed.
- Shed/park movements and shifting history.
- Shifting request/direction authorization flow.
- Shifting due rules: high priority same day; low priority tomorrow 9 AM if
  raised before 1:30 PM, day-after-tomorrow 9 AM if raised after 1:30 PM.
- Legacy shifting form fields preserved: farm, request/direction type, health/
  growth/breeding/delivery category, priority, goat IDs, breed, source shed,
  destination shed, comments, authorization, destination proof video, and
  correction audit.
- Overdue stage alerts like K1/K2/K3.
- Weighing rhythms: K/F kids every Monday; adults monthly, currently 15th.
- Feed rhythm: 8:30 AM directions, 9 AM morning feed, 2 PM revisions, 3 PM
  next-day feed staging.

In short:

```text
We start tracking whether goats are growing properly, eating properly, and
moving through the right stages at the right time.
```

Two-dev split:

```text
Dev A builds growth/feed/movement event models, shifting authorization/due rules,
feeding task generation, stage rules, feed-stock linkage, device weight ingestion,
and performance calculations.

Dev B builds Android weight/feed/movement forms, feeding task completion UI,
stage-overdue views, growth charts, feed dashboards, and shed/park movement UI.
```

## Phase 5B: Crop, Fodder, Farmer Network, And Feed Supply

Build the crop/fodder operating layer if Goat OS owns feed production and farmer
coordination. Legacy Slack automation already has farmer onboarding, crop
planning, sowing, daily crop tasks, harvest capture, and crop expenditure. This
must not be invisible just because the first goat loop is vaccination.

What users get:

- Farmer/source records for crop and fodder production.
- Crop season planning.
- Seed/sowing records.
- Daily crop/fodder tasks and proof.
- Crop status, harvest, wastage, and expenditure.
- Feed-stock handoff into inventory.
- Cost linkage from crop/fodder production into feed cost and goat economics.

In short:

```text
If the farm grows or coordinates fodder/feed through farmers, Goat OS tracks
that supply chain instead of treating feed as magic stock appearing in inventory.
```

Boundary rule:

```text
If crop/fodder farming stays outside Goat OS, keep a clear integration boundary:
external crop system -> feed inventory intake -> feed cost metrics.
Do not let dashboards depend on hidden Slack/App Script crop data forever.
```

Two-dev split:

```text
Dev A builds crop/farmer/source records, crop task/event models, harvest and
expenditure capture, and inventory handoff.

Dev B builds farmer/crop admin screens, crop task/proof screens, harvest entry,
and feed-supply dashboard views.
```

## Phase 6: Breeding, Pregnancy, Kidding, And Genetics

Build the genetics engine around family history and outcomes.

What users get:

- Parent stock records.
- Mating records.
- Pregnancy detection records.
- Ultrasound proof/observations.
- Delivery/kidding records.
- Birth/abortion form capture.
- Sequential mother/kid birth actions with answer/video proof before next action.
- Colostrum schedule and proof flow.
- Next-morning K1/mother shifting directions.
- Kid survival tracking.
- Pedigree and family tree.
- Imported embryo/semen line tracking.
- Breeder performance score.
- Inbreeding risk flags.
- Source reproduction parameters captured for Phase 6 design: estrus 12-48
  hours, goat cycle around 21 days, progesterone sponge synchronization around
  14 days, natural breeding planning ratio around one buck to five females,
  buck rest, AI/semen-batch protocol, ultrasound from about day 45, gestation
  around 150 days, and anestrus review after delivery/off-season.
- Legacy birth form fields preserved: mother ID, source/destination shed,
  breed, delivery time, kid count, kid gender breakdown, mother/kid action
  proofs, colostrum sessions, next-morning K1/mother shifts, and rectified-video
  audit.

In short:

```text
This is where Goat OS becomes serious: which bloodline produces strong kids,
which mothers perform, which imported embryo lines are worth scaling.
```

Two-dev split:

```text
Dev A builds breeding, pregnancy, kidding, birth-action, colostrum, pedigree,
embryo/semen line records, genetics scoring inputs, and inbreeding/performance rules.

Dev B builds breeding forms, pregnancy/ultrasound capture screens, birth/kid
action screens, colostrum task screens, family-tree views, genetics dashboards,
and breeder performance screens.
```

## Phase 7: Procurement, Inventory, And Cost

Build purchase and stock control.

What users get:

- Goat purchase/load records.
- Vendor/source history.
- Holding farm records for partner/source farms used between procurement and
  dispatch to main parks.
- Advance-paid / shared-pending ownership state until settlement/intake evidence
  makes ownership clear.
- Transport/transit plan, handoff proof, and route/event history.
- Arrival gate at core farm: count, identity, health, weight, media proof, and
  discrepancy review before goats become clean herd records.
- Holding-farm warmup and arrival/warmup state.
- Intake quarantine/health SOP where required.
- Landing cost per kg.
- Load-level economics: purchase, transport, holding, mortality/shrinkage,
  intake discrepancy, and realized cost per accepted goat/kg.
- Feed stock.
- Medicine/vaccine stock.
- Batch/expiry tracking.
- Procurement-to-herd intake reconciliation.

In short:

```text
This tracks what came into the system, from where, at what cost, and whether it
became real goats or usable stock.
```

Two-dev split:

```text
Dev A builds procurement, vendor/source/holding-farm records, advance/payment
state, transit/arrival gates, discrepancy review, intake reconciliation,
inventory, batch/expiry, and landing-cost calculations.

Dev B builds purchase/load entry screens, inventory screens, import/reconcile
screens, transit/arrival proof screens, cost dashboards, and stock warning views.
```

## Phase 8: Sales, Allocation, Meat Yield, And Exit

Build the commerce-facing operational handoff.

What users get:

- Sellable goat readiness.
- Sale/blocking policy.
- Initial sale blockers from current operating docs: ICU/serious illness,
  Quarantine/viral disease, milk-drinking kids up to K3, and medication
  withdrawal periods once medicine tracking is live.
- Feed contamination that is not cleared before the delivery/event date blocks
  sale/allocation and replacement/substitution.
- Promised-weight fulfillment risk: if trusted current/projected weight is below
  the promised or minimum delivery weight, the booking becomes a review item
  before dispatch.
- Phantom, unresolved, ambiguous, duplicated, and void promise rows are
  reconciled before availability is trusted.
- Exchange/listing availability is computed from identity, eligibility,
  booking, and feed/weight truth; copied listing status is not authoritative.
- Price audit: existing bookings are checked against the pricing policy/rate
  that applied on `booked_at`, not only against today's rate.
- Allocation of goat to buyer/order/occasion.
- Replacement/substitution must re-run the full sellable/readiness policy before
  assigning a substitute goat.
- Open bookings are continuously rechecked by a scheduled/event-triggered
  promise-monitoring worker until dispatch/exit; any new risk creates a
  remediation or replacement review task instead of waiting for a customer to
  notice on delivery day.
- Dispatch/exit proof.
- Slaughter/meat-yield feedback where applicable.
- Meat yield data flows back to genetics performance.

In short:

```text
When a goat leaves the farm, the system knows why, to whom, with what proof,
and what the real output was.
```

Two-dev split:

```text
Dev A builds sellable/readiness policy, allocation APIs, the promise-monitoring
sweeper/remediation queue, exit events, dispatch/slaughter proof records, and
meat-yield feedback into genetics.

Dev B builds allocation/dispatch screens, sale-blocked reason UI, open-promise
risk/replacement review screens, proof upload flows, meat-yield entry screens,
and buyer/investor-safe views.
```

## Phase 9: Devices, R&D, And AI Assistance

Connect hardware and AI as observations, not truth.

What users get:

- RFID/QR scanning.
- Weighing scale integrations.
- Camera booth/photo observations.
- Ultrasound device records.
- Collar/sensor pilots where useful.
- AI face/identity suggestion.
- AI proof pre-check.
- AI age/weight/pregnancy/disease/gait/genetics models.
- Human review gate before risky changes become truth.

In short:

```text
Devices and AI help operators move faster, but Goat OS still validates and
decides what becomes official goat truth.
```

Two-dev split:

```text
Dev A builds device gateway contracts, observation models, AI proposal records,
review gates, model/version tracking, and telemetry ingestion.

Dev B builds device status screens, scan/camera/scale mobile adapters,
AI-suggestion review UI, and R&D experiment dashboards.
```

## Phase 10: Command Center, Analytics, And AI Analyst

Build the control room.

What users get:

- CEO/admin dashboards.
- Investor-safe dashboards.
- Park health view.
- Vaccination compliance.
- Mortality analysis.
- Feed/cost analysis.
- Unit economics: landing cost, feed cost, health/treatment cost, mortality
  loss, realized margin, and cost per kg/goat/load/source.
- Genetics performance.
- Verification backlog.
- Operator performance.
- AI analyst over governed metrics only.
- Legacy BigQuery/dashboard table catalog is seed material for parity checks,
  dbt marts, and Cube metric inventory; it is not operational truth.

In short:

```text
This is the brain/dashboard layer: what is happening, what is risky, what is
profitable, and where action is needed.
```

Two-dev split:

```text
Dev A builds analytics event export, Cube metric definitions, BigQuery/Tinybird
pipelines, AI analyst guardrails, and observability/alerting.

Dev B rewires dashboards to analytics APIs, builds role-aware CEO/admin/investor
views, AI analyst UI, and operational alert views.
```

## First Product Finish Line

The first real Goat OS feature loop is:

```text
goat passport exists
-> vaccination SOP creates task
-> operator does it on Android
-> proof is uploaded
-> verifier approves
-> goat passport/timeline updates
-> dashboard shows compliance
```

After that loop works, the same engine expands to health, breeding, feed,
growth, sales, genetics, devices, and analytics.
