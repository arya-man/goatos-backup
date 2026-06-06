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

