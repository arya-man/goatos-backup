# Goat OS Forms And SOP Engine

Status: authoritative.

Forms are not surveys. A Goat OS form is a versioned operational contract that can create goat events, proof records, verification work, task transitions, analytics events, and audit records.

## Final Decision

```text
Visual creator:
  Goat OS-owned builder/editor over the Goat OS DSL.

Canonical contract:
  Goat OS Form DSL.

Rules engine:
  Goat OS evaluator, deterministic and offline-capable.

Runner:
  Native React Native Android renderer.

Backend:
  Go revalidates submissions against pinned form_version.

Storage:
  Postgres append-only form versions and submissions.

Media:
  GCS signed upload, proof artifact, hash, lifecycle policy.
```

Generic form products can be studied as UI references, but they are not the logic engine, runtime, or source of truth.

## Program Goal

The final SOP intake path is:

```text
Android operator runner
  -> Goat OS app API
  -> backend validation, idempotency, proof, audit
  -> module-owned canonical command/event
  -> Postgres projections
  -> operational product dashboards and notifications
```

Governed/leadership analytics KPIs and AI read the Cube metric layer, not raw
Postgres. Operational product dashboards read Postgres projections only when the
KPI is dual-served and covered by the parity gate in
`docs/decisions/high-scale-dashboard-projections.md`, per
`context/analytics/final-analytics-infra.md`.

Legacy Slack/App Script/Sheets/BigQuery can be temporary migration inputs and
parity oracles, but they are not the final operating path. Counts, Locations,
Mortality, and later dashboards may remove BQ/Sheets only when their feature
coverage registry, cross-source dedup, and shadow parity gates are complete for
the relevant section/grain.

The first Shifting SOP proves the reusable loop. Full SOP closure requires
migrating the Slack/App Script operating families onto the same builder, Android
runner, backend validator, proof/rework model, and module-owned domain commands.
Phase 2 platform acceptance, legacy SOP execution retirement, and dashboard
BQ/Sheets source retirement are separate gates and must not be reported as one
cutover switch.

## Why Own The Builder

The builder must understand Goat OS semantics directly:

```text
goat_scan
rfid_scan
vaccine_batch_picker
medicine_picker
shed_picker
cohort_picker
session_picker
photo_proof
video_proof
deferred_reason
supervisor_approval
verification_policy
offline_cache
repeat_for_each_goat
block_submission_if
```

A generic creator cannot reliably preview RFID, camera, dynamic stock options, offline cache state, proof policy, or per-goat batch semantics. The builder must edit the same DSL the Android runner and backend validate.

## Builder Sequencing

The engine is complete from the start. The builder UI can be sequenced without changing the contract:

```text
first creator:
  structured DSL editor
  typed field/rule forms
  validation
  exact mobile preview
  no unsupported rule can be authored

expanded creator:
  drag/drop layout
  visual conditional-rule editor
  richer canvas
  same DSL output
```

Do not block the first real SOP on a polished drag/drop canvas. The walking skeleton is DSL -> builder/editor -> native runner -> submit -> event -> verification.

## DSL Requirements

Rules are declarative only. No arbitrary JavaScript, network calls inside rules, random functions, or hidden time-dependent logic.

Required constructs:

```text
fields:
  text
  number
  date_time
  boolean
  select
  multiselect
  goat_scan
  rfid_scan
  goat_lookup
  shed_picker
  cohort_picker
  vaccine_batch_picker
  medicine_picker
  session_picker
  photo_proof
  video_proof

rules:
  visible_if
  required_if
  enabled_if
  proof_required_if
  branch_to
  repeat_for_each_goat
  block_submission_if
  requires_supervisor_if
  validation_rule
  calculated_value

option sources:
  backend-owned named sources
  scoped by park/team/operator/task
  offline cache policy
  refreshed on sync
```

## Repeat For Each Goat

This is a first-class semantic, not a normal field.

```text
one task/form:
  many goats

one submission batch:
  one parent form_submission
  N goat_submission_items
  N typed goat events when applicable
  N proof links when proof is per-goat
  N verification records when verification is per-goat

must support:
  partial completion
  offline resume
  per-goat retry
  per-goat idempotency
  batch-level proof when configured
  per-goat proof when configured
```

## Server Authority

Offline/client rules improve UX but never replace backend enforcement.

```text
block_submission_if:
  client evaluates from cached facts as a warning.
  server rechecks live state at submit.

requires_supervisor_if:
  creates an approval gate.
  touches permissions, task state, and verification.
  cannot be treated as only a form visibility rule.
```

## Submit Transaction

```text
app-api receives submission
  -> validate auth and permissions
  -> load pinned form_version
  -> validate structure and rules
  -> recheck server-authoritative gates
  -> one Postgres transaction:
       form_submission
       goat_submission_items
       typed goat event(s)
       proof records
       verification record(s)
       outbox event(s)
       audit record
  -> return stable submission/event IDs
```

Every submission has an idempotency key. Retries must not duplicate goat events, proof records, or verification work.

## Migration Tools

```text
Google Forms:
  migration bridge only.

Typeform:
  public lead/customer survey only.

Slack forms:
  legacy input during cutover only.
  Android/app-api owns operational submission.
```

## Closure Inventory

Known SOP families that need first-class migration planning:

```text
shifting / movement
count verification
weight capture
status / stage transition
death report
birth / abortion
health diagnosis and follow-up
not eating
vaccination
feed report
video / proof verification
procurement / arrival
sale / exit / inactive
```

Use `docs/phases/phase-02-sop-task-engine/SOP-CLOSEOUT.md` as the live Phase 2
cross-check for what is already captured and what remains missing before legacy
SOP execution and BQ/Sheets dashboard intake can be retired. The closeout file
does not mean Phase 2 implements every SOP family; it prevents Shifting from
being mistaken for full SOP replacement.

## Legacy Slack Form Inputs To Preserve

The General/Slack source docs show the forms that Goat OS must replace with
versioned DSL forms and native Android runners. These schemas are inputs to the
future form DSL; they are not raw copied Slack forms.

```text
Death report:
  fields: farm, goat_id, gender, breed, shed, reason
  proof: deceased goat video showing goat_id, full body, reproductive tract,
    and shed
  optional proof: post-mortem video when central requests it
  correction: void/reversal/audit, never hard delete

Shifting report:
  fields: farm, type, category, priority, goat_ids, breed, source_shed,
    destination_shed, comments
  type: Shifting Request | Shifting Direction
  category: Health | Growth | Breeding | Delivery
  priority: Low | High
  proof: video showing each shifted goat ID in the destination shed
  request flow: request needs authorization; direction is already authorized
  timing: high priority due same day; low priority due depends on 13:30 cutoff

Birth / abortion report:
  fields: farm, type, mother_id, source_shed, destination_shed, breed,
    time_of_delivery, number_of_kids, kid_gender_breakdown, comments
  proof/actions: mother checks, kid cleaning, iodine dipping, teeth check,
    suck reflex, colostrum, kid weight, next-morning K1/mother shifts
  correction: rectified video and corrected answer must preserve audit trail

Health diagnosis / follow-up:
  fields: type, goat_id, health symptom fields, video proof
  type: Diagnosis | Follow Up
  derived flow: disease selection, one problem per disease, treatment sessions,
    daily proof videos, close/extend decisions
  ICU rule: all ICU goats need daily follow-up symptom reports

Not Eating:
  separate health-related form whose output appears in the treatment flow.
```

Health symptom field groups from the legacy source:

```text
goat_status: single select normal | pregnant | mother
rectal_temperature_f: number, one decimal; normal range 101.5-103.5 F
eyes: multi select normal | red | swollen | cloudy
famacha: single select red | pink | pale | jaundice
nasal_discharge: boolean
orf_scabs: boolean
frothy_mouth: boolean
eartag: multi select normal | wound | flystrike
skin_coat: multi select normal | ticks | hair_loss_neck | hair_loss_body | hair_loss_legs
wounds: multi select no | horn | neck | body | legs
rashes: multi select no | neck | body | legs
lumps: multi select no | neck | body
left_stomach: single select normal | bloating | acidosis
diarrhea: boolean
flystrike: boolean
udder: multi select normal | swollen_hard | rashes | wound | lumps
lactation: multi select no_output | milk | colostrum | water | pus | bloody_discharge
mastitis_test: boolean
head_position: single select up | down
activity: multi select normal | not_able_to_stand | limping | back_leg_drag | front_leg_on_knees | weak
leg_injury: multi select normal | arthritis | fracture | foot_rot
miscellaneous: multi select none | competition | panting | red_urine | body_edema | stomach_inside
eating: multi select normal | not_eating | concentrate | green_feed | dry_feed
```

Phase 3 should convert these into a versioned health form DSL, preserve raw
legacy labels as aliases, and keep abnormal symptom -> disease mapping as
configuration reviewed by health/central users.

Implementation rules:

```text
repeat_for_each_goat:
  used for shifting and batch health/proof flows instead of comma-separated goat IDs.

proof policy:
  each form version declares required proof type, proof count, proof subject,
  verifier role, and rework/rectification behavior.

corrections:
  legacy delete-row behavior becomes void/reversal/correction events with audit.

dynamic pickers:
  farm/shed/breed/goat/assignee options come from backend reference data and
  offline caches, not hardcoded Slack dropdowns.
```
