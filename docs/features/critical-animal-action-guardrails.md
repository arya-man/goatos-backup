# Critical Animal Action Guardrails

Date: 2026-06-28

Status: Product/kernel requirement. Not yet implemented as a complete Goat OS
module.

Purpose: make high-risk animal actions impossible to perform casually. Goat OS
must not treat quarantine, ICU, death, contagious-disease isolation, or other
animal-risk transitions as ordinary data edits. These are kernel-level actions:
they need source-backed reason, validation, proof, approval where required,
deadlines, alert waterfalls, audit, and visible process-integrity read models.

This document records the animal-operations policy packs and gaps surfaced from
the current wiki/legacy workflow review. It must be used when designing future
Movement, Health, Quarantine, ICU, Birth, Death, Feed, Procurement, Sale,
Incident, and Goat Passport work.

Goat OS is still under development. The current live workflow is legacy
Slack/Sheets/dashboard/app behavior plus the business docs in the wiki. This
document is the Goat OS target contract for closing those gaps.

This file is not the whole guardrail architecture. The generic, feature-agnostic
guardrail engine contract is defined in
`context/architecture/operational-kernel-system-design.md`. This file is the
animal-operations policy pack family that plugs into that engine.

## Current Goat OS Code Reality

Current Goat OS backend code already has foundation mutation primitives for
moving goats, changing goat health status, and marking lifecycle exit/death.
Those primitives are useful building blocks for identity, location, lifecycle,
and admin workflows, but they are not guardrail-compliant critical-action paths
by themselves.

These primitives are not blank CRUD. Current code already requires non-empty
typed `evidence_refs` on admin move, exit, stage, and health mutation commands;
health changes emit `goat.health.changed`; and the obligation/vaccination layer
already has defer/reopen/cancel pathways for sick, ICU, quarantine, recovery,
and exit cases. Guardrail work should reuse those capabilities instead of
rebuilding them from scratch.

Until the policy packs in this document ship, product surfaces that call those
primitives for quarantine, ICU, death, high-risk movement, or sale/allocation
blockers must be restricted, feature-gated, or wrapped with explicit operational
controls. Once the guardrail engine ships, critical state transitions must flow
through guardrail evaluation before calling lower-level mutation primitives.
Evidence-gated admin mutation, generic CRUD, or a generic movement write cannot
be counted as satisfying this contract unless it also passes the full policy-pack
guardrail: source-backed reason validation, location fitness, authority/exception
rules, proof/privacy policy, SLA waterfall, audit, outbox, and read-model
visibility.

Interim enforcement must be explicit. Until a policy pack owns a critical
transition, routes/UI/actions that would mutate quarantine, ICU, death,
high-risk movement, or sale/allocation blocker state must either:

- be unavailable outside migration/admin break-glass scope
- return a deterministic `critical_action_guardrail_required` disabled reason
- route through a temporary wrapper that records evidence, authority, audit,
  outbox, and a process exception
- be limited to import/backfill paths that keep raw-source references without
  claiming guardrail-compliant canonical state

Missing segregation-of-duties inputs are fail-closed. If a policy needs
destination shed owner/manager mapping, requester/approver separation, or a
second approver and those mappings are absent or conflicting, the action must
block or create a process exception; it must not silently rely on the requester,
direction author, or a guessed shed owner as the approval authority.

A lower-level primitive being evidence-gated is useful, but it is not by itself
permission to expose a critical workflow as live product behavior.

## Generic Guardrail Boundary

Guardrails are a kernel capability, not a quarantine-specific hack. The same
engine must protect any critical state transition:

```text
critical action request
  -> policy pack selection
  -> scoped evidence fetch
  -> deterministic decision
  -> approval / exception / obligation / proof / escalation plan
  -> audit + outbox + process read models
```

The kernel must stay use-case agnostic. It should not contain hardcoded branches
like `if quarantine then ...` or `if death then ...`. Quarantine, ICU, death,
birth, feed, procurement, sale/allocation, and future high-risk workflows each
register policy packs with the same contract.

The practical rule:

```text
No critical feature may directly mutate canonical state without a guardrail
evaluation record explaining why the transition was allowed, blocked, approved,
or escalated.
```

The same pattern can also protect non-animal high-risk domains later, such as
medicine stock issue/return, feed stock discrepancy, workforce backfill for
critical tasks, buyer allocation promises, dispatch, finance approvals, or
device/tag correction. Those domains should get their own policy packs instead
of changing the kernel engine.

## Relationship To Vaccination

This is not a vaccination module. It is a critical operations guardrail family
that reuses the same foundations vaccination is proving:

- goat identity and current location
- locations, shed profiles, and quarantine/ICU attributes
- SOP task engine
- proof and verification
- obligations, deadlines, reminders, and escalations
- audit, outbox, and process-integrity read models

Vaccination treats quarantine mostly as a defer/eligibility state. This document
defines the higher-risk question: whether an animal is allowed to enter or leave
quarantine/ICU/death state at all, and what must happen when it does.

## Vertical And Module Ownership

Critical guardrails are enforced by the shared kernel, but each rule must still
have a clear business owner. The kernel owns evaluation mechanics; verticals own
the source truth, policy packs, and module-specific read models.

Command lenses such as Action Center, Control Tower, Calendar, Protocol
Adherence, and Workflows are not vertical owners. They display blocked actions,
missed checks, escalations, and next actions emitted by the owning vertical's
policy pack.

| Guardrail / workflow | Owning vertical | Owning module or source of truth |
| --- | --- | --- |
| Generic critical-action engine, policy-pack runtime, immutable decision records, idempotency, audit/outbox, approval/exception mechanics, SLA waterfall | Kernel / shared platform | Critical Action Guardrails / Operational Kernel |
| Process visibility for blocked actions, overdue obligations, missed checks, escalations, and incident next actions | Kernel / shared platform | Process Integrity: Action Center, Control Tower, Calendar, Protocol Adherence, Workflows |
| Current goat location, source/destination shed truth, shed counts, movement history, and movement proof target | Counts | Shifting / Movement, Census / Shed Count |
| Location profile, shed capability, capacity, active/review/retired state, `is_quarantine`, `is_icu`, `is_holding`, owner/manager mapping | Shared Locations master data; Counts consumes it for occupancy/current-location truth | Locations / Shed Profiles |
| Routine movement, high-risk movement, quarantine entry/exit movement, and operational separation such as keeping adult males away from females | Counts primary; Breeding/Preventive Care (PC) / Procurement may supply the reason evidence | Shifting / Operational Movement |
| Feed-count impact from movement timing, especially before/after feed cutoffs | Feed + Counts | Feed Direction with Shifting bridge |
| Biological/process quarantine for contagious/viral risk such as ORF | Preventive Care (PC) | Biosecurity / Quarantine |
| ICU, sick animal tracking, diagnosis, treatment, follow-up, and release from health restriction | Preventive Care (PC) | Health, Treatment, ICU |
| Quarantine health, weight, and follow-up checks after a Preventive Care (PC) quarantine episode starts | Preventive Care (PC) | Quarantine Checks / Health Follow-up |
| Vaccination protocol obligations, missed-dose/proof gaps, defer/reopen on health/quarantine recovery, lab/titer/sample-test confidence evidence | Preventive Care (PC) | Vaccination |
| Deworming, sanitization, water/feed testing, biosecurity checks, SOP-video verification, and Preventive Care (PC) inventory anti-misuse checks | Preventive Care (PC) | Preventive Care (PC) protocol modules |
| Incoming purchased animals before accepted herd intake, including holding farm, warmup, source records, vendor/load evidence, transit, and intake checks | Procurement | Source Entry, Intake, Holding / Warmup |
| Procurement health/weight/vaccination source evidence used by Preventive Care (PC) or Counts | Procurement owns the source record; Preventive Care (PC) / Counts consume it | Procurement Intake Evidence |
| Accepted-herd intake gate after procurement, health/weight/proof discrepancy resolution, and canonical goat creation/update | Procurement primary with Preventive Care (PC) + Counts gates | Procurement Acceptance |
| Death report, post-mortem, unexpected death, disease cluster, incident investigation, and preventive action | Preventive Care (PC) owns medical investigation; Counts consumes canonical death/lifecycle state for active counts; Kernel owns workflow mechanics | Death / Incident |
| Sale/allocation blockers caused by quarantine, ICU, death, kid stage, or withdrawal rules | Sales consumes; Preventive Care (PC) / Counts provide source truth | Sale / Allocation Blockers |

Ownership rule:

```text
Locations owns shed/location master data.
Counts owns where the goat is and the derived count/occupancy truth.
Preventive Care (PC) owns medical/preventive restriction and recovery truth.
Procurement owns incoming-animal truth before accepted herd.
Feed owns ration/feed-direction consequences of movement and counts.
The kernel owns guardrail enforcement, evidence evaluation, audit, obligations,
and escalation mechanics.
```

Legacy dashboard tabs and metrics are reporting surfaces, not ownership
authority. Counts, Shiftings, Feed, Mortality, and related dashboard views help
verify source signals and parity needs, but they do not decide which vertical
owns the canonical policy. Product ownership follows the source-of-truth module
above.

For the quarantine incident class specifically:

- Healthy existing males kept away from females -> Counts / Shifting /
  Operational Movement, with breeding reason evidence where needed.
- ORF, contagious risk, ICU, or medical isolation -> Preventive Care (PC) / Biosecurity /
  Quarantine or Preventive Care (PC) / Health.
- Newly purchased goats under incoming quarantine/warmup -> Procurement /
  Intake / Holding, with Preventive Care (PC) checks before accepted herd.
- A shed named "quarantine" but not used as biological/process quarantine ->
  Locations/Counts label or capability only; do not create a Preventive Care (PC) quarantine
  episode unless evidence-derived classification says so.

## Source Evidence Reviewed

Sanitized committed findings:

- `context/source-findings/goats-and-parks-source-findings.md`
- `context/source-findings/drive-docs-findings.md`
- `context/source-findings/live-legacy-critical-guardrails-2026-06-28.md`
- `context/product/goat-os-feature-phases.md`
- `context/architecture/operational-kernel.md`
- `docs/features/locations/TRD.md`

Maintainer-local wiki/legacy sources reviewed:

- `wiki/graphify-out/converted/Goat Passport Q&A_26b0ac60.md`
- `wiki/graphify-out/converted/Shifting Reports_27be55d1.md`
- `wiki/graphify-out/converted/Health Reports_886d914e.md`
- `wiki/graphify-out/converted/Birth Reports_10089bdd.md`
- `wiki/graphify-out/converted/Death Reports_77cc2f42.md`
- `wiki/graphify-out/converted/Feed, Shiftings and Count_27cef16d.md`
- `wiki/graphify-out/converted/Mesha-dept-directors_3e7ab556.md`
- `Preventive Care Director handbook source` graph nodes for 21-day quarantine, daily
  Park Head coordination, EOD reporting, SOP video verification, and Preventive Care (PC)
  daily/weekly execution
- `Handbooks/Health_Director.pdf` and `Handbooks/Mesha-dept-directors.pdf`
  graph nodes for the 10-minute report response standard
- `wiki/graphify-out/converted/Procurement DB [Goats]_dda03a25.md`
- `slack-automation-scripts/shifting_death_automation.js`
- legacy dashboard repo read-only checks for Counts, Shiftings, Feed, Mortality,
  and ICU/quarantine label display behavior, including
  `dashboard/lib/data/counts.ts`, `dashboard/lib/display-utils.ts`,
  `dashboard/lib/bigquery.ts`, and related CSV-backed reporting surfaces

Read-only live legacy checks on 2026-06-28:

- `goatos-sheets` BigQuery schemas for `Shiftings`, `goatsDB`, `healthDB`,
  `weights`, and `procurement_farm`
- shared Google Sheets metadata/headers for Health DB, Goats DB, Sheds DB, and
  Procurement DB
- no raw private chat, media, goat-level incident rows, or PII are copied here

No raw screenshots, chats, private media URLs, or raw source rows belong in this
repo.

## Existing Business Rules From Docs

Quarantine has two documented business meanings:

1. Incoming/purchased animals must follow quarantine protocols for a minimum 21
   days.
2. Animals with contagious or viral risk, such as ORF, are separated so disease
   does not spread.

Incoming loads also have warmup/load-management controls before entering the
breeding or fattening program. Goat OS should model quarantine and warmup as
related but separate policies unless business explicitly merges them.

Quarantine is different from ICU:

- ICU is for serious illness or urgent conditions.
- Quarantine is for biosecurity/isolation risk and may include animals that are
  not on the verge of death.

Current docs also say:

- Quarantine status blocks sale/allocation.
- Quarantine should have health and weight checks.
- Preventive Care (PC) reporting tracks incoming animals in quarantine and days remaining by
  batch.
- If a goat has no other open health problem, it may shift out of ICU or
  quarantine through a shifting request/direction.
- Disease suspicion, unexpected death cluster, or biosecurity breach escalates
  to leadership within 4 hours.

Other docs show the same control pattern across workflows:

- Shifting requires request/direction, category, priority, source/destination,
  authorization where required, assignee, destination video proof, and central
  verification.
- Shifting is tied to feed counts. Scheduled moves must complete before 09:00
  feeding, and late high-priority moves need manual bridge handling so feed is
  not missed at the destination.
- Health diagnosis cannot complete before disease selection. One goat can have
  multiple disease problems, treatment is scheduled from SOP, each treatment
  action needs video proof, and open problems must close or extend.
- ICU goats require daily follow-up symptom reports.
- Birth/abortion creates sequential mother and kid actions. Each question/action
  needs proof before the next step posts.
- Death requires reason, location, deceased-goat proof, central verification,
  possible post-mortem proof, and completion by the next day at worst.
- Procurement is a journey from source/holding farm to transit/handoff to
  arrival gate and accepted herd intake, not just a goat row.
- ICU/quarantine, milk-drinking kids, and later medication withdrawal must block
  sale/allocation.
- Director handbooks require daily SOP/video verification and fast escalation:
  report response within 10 minutes where applicable, and disease/death/
  biosecurity escalation within 4 hours.

## Guardrail Families From Wiki And Legacy

| Workflow family | Existing source signal | Goat OS guardrail implication |
| --- | --- | --- |
| Movement/shifting | Request vs Direction, category, priority, authorization, destination proof, completion before feed cutoff. | Every movement is a critical-action candidate. Special destinations or animal states add policy checks before movement applies. Feed-impact obligations must be created when counts change. |
| Quarantine/ICU | Quarantine for viral/contagious risk such as ORF; ICU for serious illness; both block sale/allocation and require follow-up. | Entry/exit needs source-backed reason, evidence, location fitness, checks, release criteria, and visible exceptions. |
| Health diagnosis/treatment | Disease selection before completion, one problem per disease, treatment schedule, daily proof, close/extend decision. | Health state changes need proof-gated obligations, missed-treatment escalation, and closure guardrails before release or sale. |
| Death | Death reason, exact shed, deceased-goat video, central verification, post-mortem when requested, completion by next day. | Death is terminal and must create investigation, proof verification, insurance/post-mortem assessment, cluster checks, and reversal/audit for wrong reports. |
| Birth/abortion | Mother/kid actions are sequential; proof gates each next step; K1/mother shifts happen next morning. | Birth creates linked mother/kid tasks, proof gates, colostrum schedule, movement obligations, and blocked states when steps are missed. |
| Procurement/arrival | Holding farm/source, health SOP, transport/handoff proof, arrival gate, discrepancy review, intake evidence. | Purchased goats cannot become clean herd truth until identity, count, weight, health, media, source, and cost evidence are reconciled. |
| Feed and stock | Feed directions depend on current shed counts; late shiftings need bridge handling; proof and discrepancy workflows exist. | Movement guardrails must emit feed-impact events. Feed issue/variance can use the same critical-action guardrail engine. |
| Sale/allocation | ICU/quarantine, young milk-drinking kids, and medication withdrawal are blockers. | Promise/sale eligibility must re-evaluate after health, movement, medicine, death, and quarantine events. |
| Proof/video verification | Proof media appears in feed, health, shifting, procurement, death, attendance, and verification workflows. | Proof capture, verifier routing, rejection, rework, retention, and audit must be shared platform capabilities. |
| Leadership escalation | SOP violations, disease/death clusters, biosecurity breach, and delayed responses have explicit escalation expectations. | SLA waterfall is product state with acknowledgement and resolution, not a Slack-only side effect. |

Proof media can include staff faces, voices, names, or other human PII. Access,
retention, redaction, and export rules must protect people as well as animal and
farm records.

## Existing Legacy Shifting Rules

Legacy shifting reports are required whenever goats move between sheds so counts
and feed packing remain accurate.

Legacy shifting fields:

- farm
- type: Shifting Request or Shifting Direction
- category: Health, Growth, Breeding, Delivery
- priority: Low or High
- goat IDs
- breed
- source shed
- destination shed
- comments
- destination-shed video proof

Legacy approval shape:

- Shifting Request is raised by ground team and requires park head/central
  authorization.
- Shifting Direction is raised by park head/central team and is automatically
  authorized.

Legacy timing shape:

- High priority is due the same day.
- Low priority before 13:30 is due tomorrow 09:00.
- Low priority after 13:30 is due day-after-tomorrow 09:00.
- Scheduled shiftings must complete before 09:00 feeding.

Legacy proof shape:

- Video must show each shifted goat ID at the destination shed.
- Central verifies request/direction, assignment, proof, and completed/cancelled
  state.

## Legacy Parity Floor

Goat OS must be better than legacy, but it must first preserve the useful
control surface legacy already has. Replacing Slack/Sheets/App Script with a
kernel must not remove operational friction that currently prevents mistakes.
This is capability parity, not bug-for-bug behavior parity. Goat OS preserves
legacy signals, workflow intent, proof points, and audit context while closing
legacy gaps as part of the same implementation.

Do not carry forward legacy weaknesses as acceptable behavior:

- comments-only reasons
- sheet names or shed labels acting as process state
- manual remembering for timetable, quarantine, health, or vaccination follow-up
- approval status without source-backed evidence validation
- proof links without subject, verifier, privacy, rejection, and rework state
- cleaned/derived tables dropping important source fields such as vaccination
  evidence
- Slack-only alerts without durable acknowledgement/resolution state
- dashboard-visible rows without canonical action, obligation, and policy
  decision records

Non-negotiable legacy capabilities to preserve:

| Legacy capability | Goat OS minimum behavior | Goat OS improvement |
| --- | --- | --- |
| Shifting Request vs Shifting Direction | Preserve who initiated the move and whether it was a request needing approval or a direction from authority. | Add policy evaluation before apply, not just approval status after submission. |
| Category and priority | Preserve Health/Growth/Breeding/Delivery-style category and Low/High urgency. | Replace free-form category dependence with typed action classification and SLA policy. |
| Source shed, destination shed, destination tag | Preserve exact movement path and destination proof target. | Resolve to canonical location profile with capabilities, capacity, risk, and timetable requirements. |
| Comments | Preserve raw human context for audit and migration. | Do not let comments be the only reason; require structured reason codes and linked evidence. |
| Approval status and workflow status | Preserve pending/authorised/rejected/cancelled/scheduled/completed style states. | Model decisions, approval, proof, verification, exception, blocked, overdue, and rework as first-class state. |
| Destination proof video | Preserve proof capture and central verification. | Add proof subject, verifier role, rejection/rework, privacy, retention, and access controls. |
| Feeding cutoff and movement timing | Preserve due-date logic and the 09:00 feed-impact concern. | Auto-create feed bridge obligations when movement timing can make feed counts wrong. |
| Overdue/open movement visibility | Preserve scheduled/open/overdue movement visibility. | Project overdue critical actions into Action Center, Control Tower, Calendar, Protocol Adherence, and Goat Passport. |
| Health diagnosis, treatment, follow-up sheets | Preserve problem, diagnosis, medicine, status, treatment schedule, follow-up, and active-problem tracking. | Convert health flows into state machines with proof, missed-check escalation, release blockers, and recovery events. |
| Procurement quarantine/holding sheet | Preserve incoming load, holding/quarantine center, weight/health check, and procurement vaccination fields. | Separate procurement warmup/holding from biological quarantine and require accepted-herd intake gates. |
| Sheds DB | Preserve existing shed labels/tags/capacity-like values. | Resolve to canonical location records owned by Locations TRD, then use those records for fitness, active-state, isolation, and timetable policy evaluation. |
| Vaccination recorded in source sheets | Preserve imported vaccination facts and source references. | Add protocol-version coverage, evidence/proof confidence, missed-dose exceptions, and recheck triggers. |
| Death reporting and verification | Preserve reason/location/proof/verification and next-day completion expectation. | Add incident investigation, cluster detection, preventive action, escalation waterfall, and void/reversal audit. |

For migration and audit, Goat OS must keep a raw-source reference for imported
legacy rows: source system, source table/sheet/tab, source row id where
available, source timestamp, raw status/category/priority, raw comments, and
raw proof links. These are not canonical truth by themselves, but they are
important for reconciliation and for proving that Goat OS did not lose legacy
context during cutover.

## Kernel Improvements Above Legacy

The kernel upgrade is not "add a few mandatory fields." The upgrade is a generic
critical-action engine that every high-risk workflow can plug into.

Every legacy replacement must be designed in two passes:

1. Preserve useful legacy capabilities and raw source context so operators do
   not lose current working controls.
2. Close known gaps with typed policy, deterministic validation, obligations,
   proof, escalation, reconciliation, and scalable projections.

Compared with legacy, Goat OS must add:

- typed action classification: quarantine episode, procurement holding/warmup,
  operational separation, ICU, death, treatment, vaccination, birth, sale blocker
- source-backed reason validation instead of comments-only reasons
- explicit evidence refs to procurement, health, movement, proof, location,
  protocol, vaccination, or approval records
- location fitness checks before critical movement applies
- automatic obligation creation for checks, rounds, proof, release, feed bridge,
  investigation, and preventive action
- durable alerts/escalations with acknowledgement and resolution state
- proof confidence and privacy controls
- immutable decision records with policy version and replay-safe idempotency
- read models that answer where the process broke, not just where a row exists
- batch-safe evaluation that works at one million-plus goat operations

Production completion requires both passes. A feature that only preserves
legacy behavior without closing known gaps is incomplete. A feature that adds
new guardrails but drops useful legacy fields, proof, statuses, or operational
visibility is also incomplete.

## Current Legacy / Docs Gap

The legacy workflow appears to enforce generic form/process steps, but not a
generic policy engine that proves a critical action is allowed for this goat, in
this context, at this time.

Important distinction:

- The wiki/SOP docs tell humans what should happen.
- Legacy Slack/Sheets/App Script flows capture fields, proof, authorization, and
  status for some workflows.
- Neither automatically means the current live system validates the domain rule
  from source evidence before allowing the state transition.

For quarantine specifically, the observed gap is:

- Destination shed can be recorded as a normal `Dst Shed` value in shifting.
- `Comments` can describe context, but there is no evidence of a mandatory
  structured `quarantine_reason`.
- There is no evidence that `Dst Shed = quarantine` is validated against
  procurement records, active health problems, ORF/viral symptoms, or vet
  instruction.
- There is no evidence that moving into quarantine automatically creates the
  quarantine health/weight checks.
- There is no evidence that moving a healthy existing farm goat into quarantine
  is blocked, warned, or escalated.

The 2026-06-28 read-only live check adds these concrete legacy observations:

- `Shiftings.shiftings_fact` carries source shed, destination shed, destination
  tag, comments, requested-by, approval status, status, overdue flags, and
  counts. It does not carry structured quarantine reason, exception type,
  linked source evidence, quarantine episode status, shed-fitness decision, or
  follow-up task linkage.
- Quarantine-like movements exist in live history as free-text comments or tags,
  for example ORF/fever/recovered animals moving into a quarantine-tagged
  destination. This is useful context, but it is not source-backed policy
  validation.
- Sheds DB lists shed identities such as Q1/Q2 as buck sheds with small
  capacity-like values; it does not model "this animal is in quarantine" as a
  biological/process state, nor does it expose sanitation, isolation class,
  fit-for-purpose, or timetable coverage fields.
- Procurement DB has a separate incoming/holding/quarantine sheet flow, including
  repeated "Weight and health Check" columns and a procurement-level
  `Vaccination` column. That is separate from an existing farm animal being
  moved into a shed whose name or tag looks like quarantine.
- The cleaned BigQuery procurement tables inspected do not carry the
  procurement sheet's `Vaccination` column forward, so downstream systems must
  not assume a sheet-level vaccination value is enough proof of vaccination
  coverage.

Sanitized incident lesson: if a shed named Q1/Q2 or "quarantine" is used only
as an operational separation shed for male/female breeding management, Goat OS
must not blindly open a quarantine episode. It must classify the action as
`operational_separation`, validate the reason, shed fitness, capacity, timetable
coverage, and animal criticality, and keep biological quarantine rules separate.

For other critical workflows, the same risk appears in different forms:

- a death can be reported, but wrong-report correction must become
  void/reversal/audit instead of delete-and-recreate
- a health diagnosis can move forward only after disease selection, but Goat OS
  must enforce that as a state machine, not as a reminder in a doc
- a treatment can be proof-gated, but missed proof must create a visible
  exception and escalation
- a birth can require sequential proof tasks, but the next action must be
  generated and blocked by the platform, not manually remembered
- a procurement load can be present in data, but accepted herd intake must wait
  for arrival-gate evidence and discrepancy resolution
- a sale/allocation can be blocked by quarantine/ICU/kid/withdrawal status, but
  that block must be recalculated after every relevant event

Therefore Goat OS must not copy legacy behavior as-is. Generic shifting approval
is not enough for a critical destination such as quarantine or ICU, and generic
form submission is not enough for any high-risk state transition.

The correct stance is: legacy parity is the floor, policy-backed process
integrity is the upgrade.

## Kernel Rule

Critical animal actions must follow the operational kernel:

```text
business event
  -> canonical transaction
  -> audit + outbox
  -> trigger evaluation
  -> obligation / work item / batch
  -> sweeper / scheduler / reminder
  -> notification / escalation
  -> proof / verification / completion
  -> process read models
```

For this feature family, the leadership answer must always be visible:

```text
Why did this goat enter quarantine/ICU/death state?
Was that reason allowed by policy?
Who requested it?
Who approved or directed it?
What evidence was attached?
What checks were automatically created?
Which checks are due, missed, or verified?
Who owns the next action?
Which alert/escalation fired when an SLA was crossed?
```

## Critical Animal Actions

These actions are high-risk and must never be plain CRUD:

- move goat into quarantine
- move goat out of quarantine
- move goat into ICU
- move goat out of ICU
- mark contagious/viral risk
- mark animal death
- mark post-mortem requested/completed
- move high-risk animal between sheds
- bulk, back-dated, or imported movement correction that changes shed/status
  history
- raise or complete high-priority movement that affects feed before cutoff
- diagnose disease and create health problems
- close or extend a health problem
- miss, skip, or override a treatment/follow-up obligation
- report birth or abortion
- apply birth/kid/mother sequential actions
- accept purchased goats into canonical herd after arrival
- mark goat eligible/ineligible for sale/allocation/dispatch
- override sale/allocation blockers
- issue or vary feed/medicine stock where animal health or cost risk is high
- override quarantine/ICU/death guardrails
- close or extend a health problem that controls quarantine/ICU release

High-risk animal movement includes, at minimum:

- breeding animals
- pregnant animals
- milking/mother animals
- bucks
- expensive/high-value animals
- sick, recovering, under-treatment, or vulnerable animals
- newly purchased/incoming animals still inside quarantine/warmup windows
- animals linked to contagious disease suspicion

The exact high-value and vulnerability rules must be configurable and
source-backed. Do not hardcode only a breed name or ad hoc chat instruction.

## Reusable Policy Pack Contract

Animal-operation policy packs plug into the generic guardrail engine. Each pack
must provide these pieces:

| Piece | Meaning |
| --- | --- |
| Action types | Stable names such as `movement.shift`, `health.close_problem`, `lifecycle.death_report`, `procurement.arrival_accept`, or `sale.allocate`. |
| Subject resolver | How the pack resolves goats, batches, sheds, loads, source farms, tasks, treatments, stock items, or bookings from the command. |
| Evidence reader | The scoped records needed for evaluation: current goat state, open health problems, procurement load, location profile, proof status, treatment history, stock state, or sale promise. |
| Classification authority | What evidence computes the final classification and how conflicts with user-entered classification are handled. |
| Allowed reasons | Versioned reason codes and which evidence makes each reason valid. |
| Decision rules | Deterministic rules that return allow, block, require approval, require exception, defer, or create process exception. |
| Primitive exposure plan | How lower-level mutation primitives are blocked, wrapped, or restricted until the pack owns the critical transition. |
| Approval plan | Who can approve routine action, who can approve exception, expiry, and whether approval is required before or after execution. |
| Segregation rules | Which requester, direction-author, approver, verifier, shed owner, or park owner combinations are disallowed or require second approval. |
| Obligation plan | Tasks/checks/follow-ups created automatically after the decision. |
| Proof policy | Media/form/signature requirements, proof subject, verifier role, rejection/rework behavior, human-PII classification, access/export/redaction rules, and retention. |
| SLA waterfall | Reminder, missed-SLA, escalation, acknowledgement, and resolution rules. |
| Projections | What Action Center, Control Tower, Calendar, Protocol Adherence, Workflow, and Goat Passport must show. |
| Tests | Allow, block, approval, exception, replay, idempotency, missing evidence, stale state, and scale cases. |

Policy packs must be open for extension and closed for kernel modification. New
critical workflows should add a pack and adapters; they should not edit a
central switch statement or bypass the evaluation record.

## Policy Pack Depth And Sequencing

This document uses quarantine and operational separation as the deepest
reference pack because that is where the live incident and legacy evidence are
strongest. Death, birth, procurement arrival, feed bridge, health treatment,
vaccination confidence, and sale/allocation blockers are included as kernel
requirements, but each still needs its own PRD/TRD or pack spec before build.

The first real guardrail engine implementation should prove the contract with at
least two policy packs. A single quarantine-only implementation can accidentally
shape the engine around one use case and would not prove the open/closed policy
pack contract.

## Quarantine Entry Guardrail

When destination location is quarantine, Goat OS must evaluate the entry before
the move can complete.

Location name is not enough. Goat OS must distinguish:

- `quarantine_episode`: the animal is entering biological/process quarantine.
- `warmup_or_procurement_holding`: incoming load management before accepted herd
  intake.
- `operational_separation`: an ordinary farm-management move into a shed whose
  name/tag may say Q1/Q2/quarantine, but the animal is not being treated as
  quarantine.

Only `quarantine_episode` should create quarantine status and quarantine exit
requirements. `warmup_or_procurement_holding` belongs to the procurement/intake
policy pack. `operational_separation` belongs to movement/park-management
guardrails and still needs controls when animal value, breeding purpose,
location risk, or missed-check impact is high.

## Classification Authority

The requested action classification is only an operator/system proposal. The
authoritative classification must be computed by the guardrail policy pack from
source-backed evidence. Free text, a dropdown choice, or a shed name cannot
decide the classification alone.

Classification rules must follow this order:

- linked procurement load/intake/arrival evidence inside the configured holding
  window -> `warmup_or_procurement_holding`
- active contagious/viral health problem, ORF evidence, configured contagious
  symptom, or vet isolation instruction -> `quarantine_episode`
- active ICU/serious-illness status or vet ICU instruction -> `icu_episode`
- explicit operational reason, compatible location profile, and no quarantine/
  ICU/procurement evidence -> `operational_separation`
- no source-backed reason, conflicting evidence, or missing location profile ->
  block or require approved exception

An actor cannot downgrade a true quarantine/ICU/procurement case to
`operational_separation` to avoid checks. If the requested classification and
evidence-derived classification disagree, the evaluation must block, require
authorized exception, or create a process exception for review.

Bulk, back-dated, or imported movement classification must be computed per
subject and source event timestamp. A mega-shift can share one import/replay
batch, but it cannot share one free-text classification across all goats, and it
cannot let historical comments silently rewrite quarantine, ICU, death, or
operational-separation truth.

Allowed source-backed reasons:

- `incoming_21_day`: goat is from procurement/intake/arrival and still inside
  the required quarantine period.
- `contagious_or_viral_risk`: goat has an active health problem/symptom/evidence
  such as ORF or other configured contagious-risk condition.
- `vet_isolation`: vet/health authority explicitly ordered isolation, with
  linked evidence.
- `approved_exception`: exception from authorized leadership, with reason,
  evidence, and short expiry.

Required fields:

- goat IDs
- source location
- destination quarantine location
- reason code
- linked evidence reference:
  - procurement load/intake/arrival record, or
  - active health problem/diagnosis/follow-up, or
  - vet instruction, or
  - approved exception decision
- requested by
- approving/directing authority
- target quarantine start date
- expected release/checkpoint date
- proof policy
- idempotency key

Hard blocks:

- no destination location
- destination is quarantine and no reason code
- reason is incoming but goat has no linked procurement/intake/arrival evidence
- reason is contagious risk but goat has no active health evidence or explicit
  vet/health authority instruction
- goat is already dead, sold, transferred, merged, or otherwise unavailable
- goat is already in another active quarantine/ICU episode that conflicts
- destination quarantine location is inactive, over capacity, or not fit for
  the requested isolation class

Exception path:

- Healthy existing farm goats are not normal quarantine candidates.
- If a healthy existing farm goat must be moved to quarantine for an operational
  reason and the destination is truly being used as quarantine, it must use
  `approved_exception`.
- If the destination is only a quarantine-named shed and the animal is not meant
  to enter quarantine, the action must be classified as `operational_separation`
  instead of mutating quarantine status.
- `approved_exception` must require a stronger approval level than routine
  shifting and must record why no normal destination was suitable.

## Quarantine Location Fitness

The canonical location schema is owned by `docs/features/locations/TRD.md`.
Guardrail packs must consume that model rather than re-specifying location
tables here. The Locations TRD defines active/review/retired state, effective
capacity records such as `capacity_kind = quarantine`, and operational
attributes such as `is_holding`, `is_quarantine`, and `is_icu`.

Quarantine is not a label on a shed alone. `is_quarantine = true` means a
location is quarantine-capable or quarantine-labelled; it does not mean every
goat placed there has an active `quarantine_episode`. Episode state is produced
by guardrail classification plus evidence, not by the shed attribute alone.
Goat OS must know whether the specific destination is fit for the specific
animal/action.

Location fitness evaluation should consume:

- active/review/retired location state
- quarantine/ICU/holding attributes
- capacity and current occupancy
- species/age/stage compatibility
- contagious cohort compatibility
- sanitation/deep-clean status
- proof of last sanitization where required
- isolation level
- staff owner/park head
- incompatible animal classes

If the destination is not fit, the system must block or escalate before the move
is completed.

Location owner and park-head mapping are hard dependencies for segregation of
duties. If a location profile does not identify who owns/manages the shed,
routine self-authored movement into that shed cannot be auto-approved. The
system must require second approval, block, or create a process exception until
the owner mapping is configured.

## Operational Separation Guardrail

Some high-risk movements are not quarantine, but still cannot be treated as
ordinary shed edits. Examples include separating adult males from adult females
for breeding control, moving high-value bucks, moving vulnerable animals, or
placing animals in small/isolated sheds for operational reasons.

Required controls:

- structured movement reason such as `breeding_separation`, `space_management`,
  `behavioral_separation`, `feed_management`, or `approved_exception`
- source-backed or authority-backed evidence for the reason
- destination shed profile and capacity check
- incompatibility check for gender, age/stage, breed, breeding purpose, disease
  cohort, and vulnerable/high-value status
- explicit "not quarantine status" classification when using a quarantine-named
  shed for ordinary separation
- segregation-of-duties check when the requester, directing authority, or
  verifier also owns or manages the destination shed; self-authored directions
  into a user's own shed need a second approver or stronger evidence
- timetable/round coverage obligation for the destination shed
- escalation if a required health, water/feed, or visual check is missed

This guardrail prevents the system from solving the wrong problem. A free-text
reason or shed name should not decide whether the animal is in quarantine,
operational separation, ICU, warmup, or routine movement.

## Quarantine Obligations

Entering quarantine must create work automatically. It must not rely on someone
tracking a separate manual follow-up outside the workflow.

At minimum, create:

- quarantine episode record
- health/weight check obligations
- shed timetable/round obligations when the destination requires routine visual
  checks independent of quarantine status
- due dates and owner assignments
- proof policy for each check
- reminders and escalation deadlines
- release review obligation

The check frequency should come from configurable protocol/SOP rules. If the
rule is missing, the move should create a process exception instead of silently
doing nothing.

Source trail for timetable/round requirements:

- `Preventive Care Director handbook source` graph nodes cover daily Park Head coordination,
  EOD reporting, SOP video verification, and Preventive Care (PC) daily/weekly execution.
- `Handbooks/Health_Director.pdf` and `Handbooks/Mesha-dept-directors.pdf`
  graph nodes cover the 10-minute report response standard where applicable.
- `context/source-findings/live-legacy-critical-guardrails-2026-06-28.md`
  records that current legacy sheets do not create durable timetable obligations
  automatically.

Implementation must cite the exact protocol/SOP version that supplies the
frequency, proof policy, owner, and escalation rule.

## Quarantine Exit Guardrail

Moving out of quarantine requires:

- active quarantine episode
- exit reason
- required quarantine period completed, or explicit override
- active contagious/health problem resolved or approved for transfer
- no open blocking health problem
- required health/weight checks completed or reviewed
- destination location fitness check
- shifting request/direction with proof
- audit and outbox event
- recovery/eligibility-change event so obligations deferred while the goat was in
  quarantine/ICU (such as deferred vaccination) are re-evaluated downstream
  instead of staying silently parked

The contract requires a durable recovery or eligibility-change signal. Existing
events such as `goat.location.changed` and `goat.health.changed` can satisfy
this only if their payload and consumers can prove the old and new states needed
to reopen deferred obligations. If they cannot, the pack must add an explicit
recovery/eligibility-change event instead of relying on implicit status strings
or read-time inference.

Current code already points in the right direction:
`backend/internal/identity/adapters/postgres/goat_lifecycle.go` emits
`goat.health.changed`, `backend/internal/vaccination/app/generation_handler.go`
consumes it for recovery rechecks, and
`backend/internal/obligation/adapters/postgres/repository.go` manages
deferred-obligation transitions by idempotency key. Policy packs should reuse
that path when it satisfies critical-action old/new-state requirements instead
of rebuilding a parallel recovery channel.

No one should be able to "just move back" by editing current shed.

## Death Guardrail

Death is one of the highest criticality events in Goat OS.

Death report must capture:

- goat ID
- farm/park/shed
- current location context
- reason/cause as reported
- reporter
- timestamp
- proof video/photo
- whether post-mortem is required
- whether insurance assessment is required
- whether the death happened in quarantine/ICU/high-risk context
- whether there is a cluster or pattern

Death report must immediately:

- freeze the goat's lifecycle transition through a canonical death event
- write business audit/history
- emit outbox/domain event
- create investigation work
- create proof verification work
- check cluster/pattern rules
- notify owners

Wrong death reports must be corrected by void/reversal/audit, never deletion.

There must be one canonical death path. Existing lifecycle exit/death mutation
primitives are lower-level building blocks and must be wrapped or subsumed by
the death guardrail before live critical-action use. Deaths recorded through old
paths before the pack ships must be imported/backfilled with legacy source
references, then reconciled into the death guardrail read models without
rewriting history.

## Incident Investigation And Preventive Action

Any death in quarantine, ICU, or high-risk movement context must create an
incident investigation.

Investigation must answer:

- Why was the animal in that location?
- Which rule allowed the placement?
- Who requested and approved/directed it?
- Was proof present?
- Were quarantine/health/weight checks due?
- Were any checks missed or late?
- Was the shed fit?
- Was the animal high-value, pregnant, breeding, sick, or vulnerable?
- Was this part of a cluster or pattern?
- What process failed?
- What preventive action prevents recurrence?

Preventive action must have:

- owner
- deadline
- severity
- required proof
- verifier
- escalation path
- completion/rework states

If the investigation shows the animal was not actually in quarantine but in an
operational separation shed, the incident still stays critical. The investigation
must then focus on whether the shed was correctly classified, whether timetable
checks were assigned and completed, whether vaccination/prophylaxis evidence was
trustworthy, and whether the chosen location was appropriate for the animal
class and business value.

## Vaccination Confidence Evidence

Vaccination evidence is part of the critical-action guardrail surface whenever
an incident, quarantine, procurement intake, sale blocker, or health decision
depends on whether the goat was actually protected.

Accepted evidence types may include administered-dose proof, imported source
sheet row, supplier/procurement record, Preventive Care (PC) / vet attestation, lab result, titer,
sample-test, or approved exception. Each evidence type needs source reference,
actor, timestamp, verifier/reviewer where required, confidence level, and
recheck/exception state.

A recorded vaccination date that may have been missed is not enough to silently
close a high-risk decision. It must create a confidence gap, recheck/sample-test
task, exception, or incident investigation input according to policy.

## Approval And Authority

Do not create a separate approval maze for every action. Use the operational
kernel's assignment/approval model, but make the reason and guardrail evaluation
part of the same workflow.

Rules:

- Routine shifting approval is not enough when destination is quarantine/ICU.
- The same shifting request can carry quarantine-specific validation fields.
- Shifting Direction by park head/central team must still pass guardrail checks.
  Authority can approve; it cannot bypass evidence silently.
- A self-authored direction into a shed, park, or managed scope owned by the same
  actor is not enough by itself; policy must require segregation-of-duties,
  stronger evidence, second approval, or process-exception state.
- Override/exception decisions need explicit authority, reason, expiry, and
  audit.
- AI may propose risk and missing evidence. AI must never approve the action.

## Alert Waterfall

Critical animal actions require a short SLA waterfall. Exact times should be
configurable by severity, but the default posture is: critical incidents get
fast local response and leadership visibility before the outer 4-hour
escalation limit from current docs.

Illustrative default SLA levels, pending final business confirmation:

| Trigger | Immediate owner | Escalation 1 | Escalation 2 | Hard outer limit |
| --- | --- | --- | --- | --- |
| Quarantine move missing reason/evidence | requester blocked instantly | Park Head / Health owner within 10 minutes | configured Preventive Care (PC) or Health Director role within 30 minutes | leadership before 4 hours |
| Quarantine check overdue | assigned operator immediately | Park Head within 30 minutes | configured Preventive Care (PC) or Health Director role within 2 hours | leadership before 4 hours if critical |
| Critical shed timetable/round missed | assigned operator immediately | Park Head within 30 minutes | configured Preventive Care (PC) or Health Director role within 2 hours | leadership before 4 hours if death/high-value/high-risk |
| Vaccination coverage/proof confidence gap for high-risk animal | Preventive Care (PC) owner immediately | configured Preventive Care (PC) or Health Director role within 30 minutes | COO/CEO if linked to death/cluster | before investigation closes |
| Death in quarantine/ICU/high-risk context | Park Head + Health owner immediately | configured Preventive Care (PC) or Health Director role within 10 minutes | COO/CEO within 30 minutes | never later than 4 hours |
| Biosecurity breach / contagious cluster | Park Head + Preventive Care (PC) owner immediately | configured Preventive Care (PC) or Health Director role within 10 minutes | COO/CEO within 30 minutes | never later than 4 hours |
| Missing proof for critical action | assignee immediately | verifier/Park Head within 30 minutes | configured Preventive Care (PC) or Health Director role within 2 hours | leadership if still open |

Role names in this table are human-readable labels. Policy packs must resolve
them to configured role or capability codes for the active deployment; do not
persist labels such as "Director" as authorization truth.

Alerts must be durable rows/events, not only Slack messages. Notification
adapters can send Slack/FCM/email, but Postgres state is the truth.

Missed/overdue state must also be durable. Read-time "overdue" calculations are
useful for display, but they cannot be the only source for Action Center,
Control Tower, SLA escalation, or compliance metrics. A deadline crossing must
create or update durable missed/escalation state and emit the outbox event that
downstream projections consume.

## Read Models And Surfaces

Goat OS must expose critical guardrails in the same command lenses as other
kernel workflows.

Goat Passport:

- current quarantine/ICU/death status
- active episode and reason
- evidence links
- approval/directing authority
- pending checks
- missed checks
- shed timetable/round coverage status
- vaccination coverage/proof confidence for the relevant protocol window
- incident/investigation state
- sale/allocation blockers and the source event that created each blocker
- movement and high-risk action timeline

Action Center:

- pending approvals
- blocked quarantine moves
- overdue checks
- missing proof
- missed treatment/follow-up work
- birth/kid/mother proof-gate tasks
- procurement arrival/discrepancy tasks
- feed-impact bridge tasks after urgent movement
- sale/allocation exception reviews
- incident investigation tasks

Control Tower:

- critical exceptions by park/shed/owner/severity
- death in quarantine/ICU count
- missing reason/evidence count
- overdue quarantine checks
- missed critical shed timetable/round checks
- vaccination coverage/proof confidence gaps for high-risk animals
- missed health treatment or ICU follow-up count
- birth/abortion blocked-sequence count
- procurement arrival gate blocked count
- feed-impact exceptions after movement
- sale/allocation blocked-by-health/quarantine count
- unresolved preventive actions

Calendar:

- quarantine checks
- shed timetable/round checks for critical sheds
- release reviews
- treatment sessions
- ICU follow-ups
- birth/colostrum/kid actions
- procurement arrival gate deadlines
- feed bridge deadlines
- investigation deadlines
- preventive action deadlines

Protocol Adherence:

- quarantine entry compliance
- check completion compliance
- critical shed timetable compliance
- vaccination proof/coverage confidence compliance
- release compliance
- death investigation compliance
- treatment proof compliance
- shifting proof and authorization compliance
- birth/kid sequence compliance
- procurement arrival compliance
- sale/allocation blocker compliance

Workflow drilldown:

- event timeline from request to approval to movement to checks to proof to
  verification to release/investigation
- policy decision timeline showing allowed, blocked, approved, exception,
  deferred, escalated, acknowledged, and resolved states

## Host Module Dependencies

Some guardrail outputs depend on host modules that may not exist yet. The
guardrail engine must still record the risk, but it must not pretend an
integration is complete when the host module is missing.

Examples:

- feed-impact bridge obligations require the feed-direction/feed execution host
  module before they can create executable feed tasks
- sale/allocation blockers require the sale/allocation host module before they
  can enforce promise or booking decisions
- procurement arrival gates require procurement/source-entry records before they
  can accept or reject animals into clean herd truth
- vaccination confidence and recheck outputs require protocol/vaccination
  coverage records before they can close a dose-confidence gap

Before the host module ships, the guardrail should emit a durable process
exception or deferred integration marker with owner, SLA, and read-model
visibility. Silent no-op integration is not allowed.

## Data Model Implications

Future PRD/TRD work should decide exact table names, but the shared model must
support these generic records:

- legacy source reference for migrated/imported rows, including source system,
  sheet/table/tab, row identity where available, raw status/category/priority,
  raw comments, raw proof links, and import/reconciliation state
- critical action request with action type, subject refs, transition, scope,
  actor, source, idempotency key, and requested reason
- critical-action exposure gate state for lower-level primitives that are not
  yet wrapped by a policy pack
- batch/import guardrail request with source event timestamp, import/apply
  timestamp, bounded subject set, per-subject evaluation results, partial
  failure state, and replay key
- requested classification, evidence-derived classification, conflict state,
  and final authoritative classification that separates biological/process
  status from location names or tags
- location profile reference owned by the Locations module/TRD, including
  canonical location ID, display name/tag, capabilities, capacity, fitness
  attributes, isolation class, active state, and timetable requirements
- policy pack and immutable policy version used for the decision
- guardrail evaluation result with decision, disabled reasons, evidence summary,
  risk/severity, and replay metadata
- linked evidence refs to source records, proof media, forms, health problems,
  procurement loads, location profiles, stock records, bookings, or approvals
- approval, rejection, override, and exception records with authority, reason,
  expiry, and audit
- obligation/task plan created by the decision
- timetable/round obligation and coverage records for critical sheds and animal
  cohorts
- vaccination coverage/proof-confidence records linked to protocol version,
  subject, evidence type such as administered dose, supplier record, lab result,
  titer, sample-test, or exception, actor, and exception state
- proof and verification state, including reject/rework/accept paths
- proof privacy state, including human-PII classification, access class,
  redaction requirement/status, export permission, retention policy, delete/hold
  status, and audit for access/export/redaction decisions
- SLA escalation state with reminder, missed, acknowledgement, resolution, and
  delivery attempts
- durable missed/deadline-crossed state and outbox events for obligations that
  drive Action Center, Control Tower, Calendar, and Protocol Adherence
- process exception and preventive action records
- full business audit/history and outbox/domain events

Animal policy packs then add or reference domain records:

- movement request/direction with critical destination policy evaluation
- operational separation episode/reason when a non-quarantine animal is placed
  in a quarantine-named or otherwise critical shed
- quarantine episode and release review
- ICU episode and follow-up requirements
- health diagnosis/problem/treatment/follow-up state
- birth/abortion, mother/kid actions, and colostrum schedule
- death report/event, post-mortem, insurance assessment, and investigation
- procurement load, holding/source context, arrival gate, discrepancy review,
  and accepted herd intake
- feed-impact task or bridge ration obligation after movement
- sale/allocation eligibility decision and active blocker records

All mutating routes must be idempotent and replay-safe.

## Scale Model For 1M+ Goat Operations

Guardrails must work when Goat OS has more than one million goat operations and
hot parks/sheds are unevenly loaded.

Rules:

- Evaluation input must be explicit and bounded: subject IDs, scope, action
  type, date/time, actor, and evidence refs.
- Policy packs may not scan the full herd inside an API request. They read by
  indexed goat/load/shed/task/booking IDs or by bounded tenant/park/shed/date
  windows.
- Cross-herd risk such as cluster deaths, treatment backlog, feed variance, or
  promise-risk must come from maintained projections and incremental workers.
- Batch actions use preflight jobs with progress and partial failure state. The
  apply step is idempotent per subject/action/policy version.
- Guardrail evaluations are immutable audit facts. Later repair/re-evaluation
  creates a new evaluation record rather than rewriting the old one.
- High-volume audit, event, media, proof, notification, and evaluation tables
  must be partition-aware where volume requires it.
- Read models power Control Tower, Action Center, Calendar, Protocol Adherence,
  and Goat Passport. UI pages must not assemble critical state from ad hoc
  multi-table scans.
- Load tests must include skewed hot sheds, duplicate submissions, replay,
  stale projections, missing owners, missing evidence, slow verifiers, DLQ
  poison events, and long-running batch preflights.

## Acceptance Cases

Goat OS is not acceptable until generic and domain cases are covered by
tests/E2E seeds for the relevant module.

Generic engine cases:

1. A new policy pack can be registered without editing the kernel engine.
2. The first production implementation proves the engine with at least two
   policy packs, so the contract is not accidentally shaped around quarantine
   only.
3. Every critical action request records policy version, evidence refs,
   decision, actor, scope, audit, and outbox.
4. Missing evidence returns a deterministic block/disabled reason instead of a
   permissive fallback.
5. Retry/replay of the same action (same idempotency key, same payload) returns
   the original result and does not duplicate obligations, proof tasks,
   notifications, approvals, or audit rows. A same-key, different-payload replay
   is rejected or returns the original result with no new side effects.
6. Batch preflight evaluates bounded subjects and exposes partial failures.
7. A policy version change does not rewrite old evaluation history.
8. Action Center, Control Tower, Calendar, Protocol Adherence, Workflow, and
   entity detail surfaces all read projection state created by the kernel.
9. Load tests prove the widest allowed evaluation/list/worker paths do not scan
   the full herd.
10. AI/automation can attach risk signals and missing-evidence warnings to a
   critical action, but cannot approve, override, or complete it. Only an
   authorized human decision closes the guardrail.
11. Proof media that may include staff faces, voices, names, or other human PII
    records privacy classification and enforces configured access, export,
    redaction, and retention rules.
12. Legacy import/replay preserves request/direction, category, priority,
    source/destination, raw comments, raw status, proof reference, and source row
    reference while producing canonical guardrail state separately.

Animal policy cases:

1. Legacy shifting behavior is not regressed: request vs direction, approval
   status, movement status, proof, due timing, and feed-impact timing remain
   visible after migration/cutover.
2. Operator requests `operational_separation` for a goat with active ORF,
   contagious-risk evidence, or procurement holding evidence; the guardrail
   rejects the operator classification and computes the quarantine/procurement
   classification from evidence.
3. An unwrapped low-level move, health, stage, or death primitive is unavailable
   for live critical transitions, returns `critical_action_guardrail_required`,
   or routes through a wrapper that records process exception state.
4. Incoming purchased goat can enter quarantine with linked intake evidence and
   automatically receives quarantine checks.
5. Goat with active ORF/contagious-risk health problem can enter quarantine with
   linked health evidence.
6. Healthy existing goat cannot enter quarantine without approved exception.
7. Healthy existing goat moved into a quarantine-named shed for breeding or
   operational separation does not mutate quarantine status; it requires an
   operational-separation reason, shed fitness/capacity check, timetable
   obligation, and audit.
8. Bulk, back-dated, or imported mega-shift affecting many goats runs bounded
   per-goat batch preflight, preserves source/event time separately from
   import/apply time, exposes partial failures, and cannot silently rewrite
   quarantine/ICU/death/shed truth from historical comments.
9. Quarantine move with free-text-only reason is blocked.
10. Quarantine destination over capacity or inactive is blocked.
11. Shifting Direction by authorized user still fails if guardrail evidence is
   missing.
12. Quarantine check overdue creates visible Action Center and Control Tower
   exception plus escalation.
13. Deadline crossing creates durable missed/escalation state and outbox; Action
    Center and Control Tower do not rely only on read-time overdue filters.
14. Critical shed timetable/round missed creates Action Center, Control Tower,
   escalation, and investigation linkage if there is death or deterioration.
15. Vaccination record without trusted evidence/proof is flagged as a confidence
    gap and cannot silently satisfy high-risk eligibility or incident review.
16. Goat cannot exit quarantine while required checks or blocking health problems
    remain open.
17. Quarantine/ICU/health recovery emits a durable signal that re-evaluates
    deferred obligations such as vaccination; implicit read-time inference is
    not enough.
18. Health diagnosis cannot complete before disease selection and required proof.
19. Missed treatment proof creates rework/escalation and does not silently close
    the treatment.
20. Birth/kid/mother sequence does not post or complete the next action until
    the prior proof gate is accepted.
21. Procurement arrival cannot accept goats into clean herd truth until intake,
    health, weight/count, media, source, and discrepancy checks are resolved.
22. Urgent movement after feed cutoff creates a feed-impact/bridge obligation
    only when the feed host module can own it; otherwise it creates a durable
    deferred integration/process exception.
23. Sale/allocation is blocked when goat is in ICU/quarantine, milk-drinking kid
    stage, dead, or under a configured withdrawal blocker.
24. Sale/allocation blocker emits a durable process exception/deferred marker if
    the sale/allocation host module is not yet available.
25. Death in quarantine creates death event, investigation, proof verification,
    preventive action, and escalation.
26. Existing lifecycle exit/death primitives cannot create a second canonical
    death path; live death actions route through the guardrail, and pre-pack
    legacy deaths are backfilled/reconciled with source references.
27. Wrong death report is voided/reversed with audit, not deleted.
28. High-value/pregnant/breeding/vulnerable animal movement requires stricter
    approval and evidence.
29. A self-authored direction into a shed with missing or conflicting owner
    mapping blocks, requires second approval, or creates process exception state.
30. All critical actions show a timeline on Goat Passport.

## Business Inputs Still Needed

These must be confirmed before implementation:

- criticality matrix by action type and animal state
- mapping from legacy shifting categories/priorities/statuses/proof fields to
  canonical Goat OS action classification and state
- bulk/back-dated movement cutover rules, source-vs-import timestamps, and
  partial-failure policy
- location owner/manager mapping for shed-level segregation-of-duties
- exact quarantine health/weight check frequency by animal class and reason
- exact role that can approve routine quarantine moves
- exact role that can approve exceptions
- segregation-of-duties matrix for self-authored directions into a user's own
  shed, park, or managed scope
- high-value animal definition
- quarantine shed fitness dimensions and thresholds
- critical shed timetable/round frequency, owner, proof policy, and escalation
- whether quarantine-named Q1/Q2 style sheds should be modeled as quarantine
  capable locations, ordinary sheds, or both depending on action classification
- vaccination evidence/proof standard, lab/titer/sample-test evidence types,
  confidence scoring, and what to do when a vaccination is recorded but may have
  been missed
- when a death is "unexpected", "cluster", or "high severity"
- default SLA minutes for each escalation level
- whether post-mortem is mandatory for every quarantine/ICU death or only by
  severity
- who verifies quarantine proof and who verifies death/investigation proof
- proof/media retention policy: routine vaccination/SOP proof uses
  `operational_90d`; standard non-critical operational proof uses
  `standard_1y`; death, quarantine, ICU, contagious-isolation, high-value animal,
  legal, and incident/investigation proof uses `critical_7y`; active
  investigation/legal-hold proof uses `legal_hold` until the hold is released.
  Raw media may expire by policy, but metadata, hash, audit link, verifier,
  decision, and legal-hold state remain canonical. Human-PII media requires
  role-gated access, export audit, and redaction before broad sharing.
- policy owner, publish/rollback process, and effective-date behavior for each
  guardrail pack
- feed-impact rules for urgent shiftings after the feed-direction cutoff
- host-module readiness/order for feed bridge, sale/allocation blockers,
  procurement arrival, and vaccination confidence rechecks
- procurement arrival-gate evidence and discrepancy thresholds
- sale/allocation blocker policy for ICU, quarantine, kid stage, medication
  withdrawal, treatment status, and promise-risk

Missing business inputs must not lead to silent permissive behavior. Until
configured, critical actions should block, create a process exception, or require
explicit higher approval.
