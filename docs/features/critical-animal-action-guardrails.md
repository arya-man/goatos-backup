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

## Source Evidence Reviewed

Sanitized committed findings:

- `context/source-findings/drive-docs-findings.md`
- `context/product/goat-os-feature-phases.md`
- `context/architecture/operational-kernel.md`

Maintainer-local wiki/legacy sources reviewed:

- `wiki/graphify-out/converted/Goats and Parks_0e7e3494.md`
- `wiki/graphify-out/converted/Goat Passport Q&A_26b0ac60.md`
- `wiki/graphify-out/converted/Shifting Reports_27be55d1.md`
- `wiki/graphify-out/converted/Health Reports_886d914e.md`
- `wiki/graphify-out/converted/Birth Reports_10089bdd.md`
- `wiki/graphify-out/converted/Death Reports_77cc2f42.md`
- `wiki/graphify-out/converted/Feed, Shiftings and Count_27cef16d.md`
- `wiki/graphify-out/converted/Mesha-dept-directors_3e7ab556.md`
- `wiki/graphify-out/converted/Procurement DB [Goats]_dda03a25.md`
- `slack-automation-scripts/shifting_death_automation.js`
- legacy dashboard repos for read-only shifting/quarantine display behavior

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
- PHC reporting tracks incoming animals in quarantine and days remaining by
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
| Allowed reasons | Versioned reason codes and which evidence makes each reason valid. |
| Decision rules | Deterministic rules that return allow, block, require approval, require exception, defer, or create process exception. |
| Approval plan | Who can approve routine action, who can approve exception, expiry, and whether approval is required before or after execution. |
| Obligation plan | Tasks/checks/follow-ups created automatically after the decision. |
| Proof policy | Media/form/signature requirements, proof subject, verifier role, rejection/rework behavior, human-PII classification, access/export/redaction rules, and retention. |
| SLA waterfall | Reminder, missed-SLA, escalation, acknowledgement, and resolution rules. |
| Projections | What Action Center, Control Tower, Calendar, Protocol Adherence, Workflow, and Goat Passport must show. |
| Tests | Allow, block, approval, exception, replay, idempotency, missing evidence, stale state, and scale cases. |

Policy packs must be open for extension and closed for kernel modification. New
critical workflows should add a pack and adapters; they should not edit a
central switch statement or bypass the evaluation record.

## Quarantine Entry Guardrail

When destination location is quarantine, Goat OS must evaluate the entry before
the move can complete.

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
  reason, it must use `approved_exception`.
- `approved_exception` must require a stronger approval level than routine
  shifting and must record why no normal destination was suitable.

## Quarantine Location Fitness

Quarantine is not a label on a shed alone. Goat OS must know whether the specific
destination is fit for the specific animal/action.

Location fitness should include:

- active/inactive lifecycle state
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

## Quarantine Obligations

Entering quarantine must create work automatically. It must not rely on someone
tracking a separate manual follow-up outside the workflow.

At minimum, create:

- quarantine episode record
- health/weight check obligations
- due dates and owner assignments
- proof policy for each check
- reminders and escalation deadlines
- release review obligation

The check frequency should come from configurable protocol/SOP rules. If the
rule is missing, the move should create a process exception instead of silently
doing nothing.

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

## Approval And Authority

Do not create a separate approval maze for every action. Use the operational
kernel's assignment/approval model, but make the reason and guardrail evaluation
part of the same workflow.

Rules:

- Routine shifting approval is not enough when destination is quarantine/ICU.
- The same shifting request can carry quarantine-specific validation fields.
- Shifting Direction by park head/central team must still pass guardrail checks.
  Authority can approve; it cannot bypass evidence silently.
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
| Quarantine move missing reason/evidence | requester blocked instantly | Park Head / Health owner within 10 minutes | configured PHC or Health Director role within 30 minutes | leadership before 4 hours |
| Quarantine check overdue | assigned operator immediately | Park Head within 30 minutes | configured PHC or Health Director role within 2 hours | leadership before 4 hours if critical |
| Death in quarantine/ICU/high-risk context | Park Head + Health owner immediately | configured PHC or Health Director role within 10 minutes | COO/CEO within 30 minutes | never later than 4 hours |
| Biosecurity breach / contagious cluster | Park Head + PHC owner immediately | configured PHC or Health Director role within 10 minutes | COO/CEO within 30 minutes | never later than 4 hours |
| Missing proof for critical action | assignee immediately | verifier/Park Head within 30 minutes | configured PHC or Health Director role within 2 hours | leadership if still open |

Role names in this table are human-readable labels. Policy packs must resolve
them to configured role or capability codes for the active deployment; do not
persist labels such as "Director" as authorization truth.

Alerts must be durable rows/events, not only Slack messages. Notification
adapters can send Slack/FCM/email, but Postgres state is the truth.

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
- missed health treatment or ICU follow-up count
- birth/abortion blocked-sequence count
- procurement arrival gate blocked count
- feed-impact exceptions after movement
- sale/allocation blocked-by-health/quarantine count
- unresolved preventive actions

Calendar:

- quarantine checks
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

## Data Model Implications

Future PRD/TRD work should decide exact table names, but the shared model must
support these generic records:

- critical action request with action type, subject refs, transition, scope,
  actor, source, idempotency key, and requested reason
- policy pack and immutable policy version used for the decision
- guardrail evaluation result with decision, disabled reasons, evidence summary,
  risk/severity, and replay metadata
- linked evidence refs to source records, proof media, forms, health problems,
  procurement loads, location profiles, stock records, bookings, or approvals
- approval, rejection, override, and exception records with authority, reason,
  expiry, and audit
- obligation/task plan created by the decision
- proof and verification state, including reject/rework/accept paths
- proof privacy state, including human-PII classification, access class,
  redaction requirement/status, export permission, retention policy, delete/hold
  status, and audit for access/export/redaction decisions
- SLA escalation state with reminder, missed, acknowledgement, resolution, and
  delivery attempts
- process exception and preventive action records
- full business audit/history and outbox/domain events

Animal policy packs then add or reference domain records:

- movement request/direction with critical destination policy evaluation
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
2. Every critical action request records policy version, evidence refs,
   decision, actor, scope, audit, and outbox.
3. Missing evidence returns a deterministic block/disabled reason instead of a
   permissive fallback.
4. Retry/replay of the same action (same idempotency key, same payload) returns
   the original result and does not duplicate obligations, proof tasks,
   notifications, approvals, or audit rows. A same-key, different-payload replay
   is rejected or returns the original result with no new side effects.
5. Batch preflight evaluates bounded subjects and exposes partial failures.
6. A policy version change does not rewrite old evaluation history.
7. Action Center, Control Tower, Calendar, Protocol Adherence, Workflow, and
   entity detail surfaces all read projection state created by the kernel.
8. Load tests prove the widest allowed evaluation/list/worker paths do not scan
   the full herd.
9. AI/automation can attach risk signals and missing-evidence warnings to a
   critical action, but cannot approve, override, or complete it. Only an
   authorized human decision closes the guardrail.
10. Proof media that may include staff faces, voices, names, or other human PII
    records privacy classification and enforces configured access, export,
    redaction, and retention rules.

Animal policy cases:

1. Incoming purchased goat can enter quarantine with linked intake evidence and
   automatically receives quarantine checks.
2. Goat with active ORF/contagious-risk health problem can enter quarantine with
   linked health evidence.
3. Healthy existing goat cannot enter quarantine without approved exception.
4. Quarantine move with free-text-only reason is blocked.
5. Quarantine destination over capacity or inactive is blocked.
6. Shifting Direction by authorized user still fails if guardrail evidence is
   missing.
7. Quarantine check overdue creates visible Action Center and Control Tower
   exception plus escalation.
8. Goat cannot exit quarantine while required checks or blocking health problems
   remain open.
9. Health diagnosis cannot complete before disease selection and required proof.
10. Missed treatment proof creates rework/escalation and does not silently close
    the treatment.
11. Birth/kid/mother sequence does not post or complete the next action until
    the prior proof gate is accepted.
12. Procurement arrival cannot accept goats into clean herd truth until intake,
    health, weight/count, media, source, and discrepancy checks are resolved.
13. Urgent movement after feed cutoff creates a feed-impact/bridge obligation.
14. Sale/allocation is blocked when goat is in ICU/quarantine, milk-drinking kid
    stage, dead, or under a configured withdrawal blocker.
15. Death in quarantine creates death event, investigation, proof verification,
    preventive action, and escalation.
16. Wrong death report is voided/reversed with audit, not deleted.
17. High-value/pregnant/breeding/vulnerable animal movement requires stricter
    approval and evidence.
18. All critical actions show a timeline on Goat Passport.

## Business Inputs Still Needed

These must be confirmed before implementation:

- criticality matrix by action type and animal state
- exact quarantine health/weight check frequency by animal class and reason
- exact role that can approve routine quarantine moves
- exact role that can approve exceptions
- high-value animal definition
- quarantine shed fitness dimensions and thresholds
- when a death is "unexpected", "cluster", or "high severity"
- default SLA minutes for each escalation level
- whether post-mortem is mandatory for every quarantine/ICU death or only by
  severity
- who verifies quarantine proof and who verifies death/investigation proof
- human-PII policy for proof media: classification, access, export, redaction,
  deletion/retention, and legal-hold behavior
- policy owner, publish/rollback process, and effective-date behavior for each
  guardrail pack
- feed-impact rules for urgent shiftings after the feed-direction cutoff
- procurement arrival-gate evidence and discrepancy thresholds
- sale/allocation blocker policy for ICU, quarantine, kid stage, medication
  withdrawal, treatment status, and promise-risk

Missing business inputs must not lead to silent permissive behavior. Until
configured, critical actions should block, create a process exception, or require
explicit higher approval.
