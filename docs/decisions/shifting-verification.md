# Shifting requires verifier-approved video before the count moves

**Status:** Accepted — maintainer decision, 2026-07-26; camera-source clarification 2026-07-28.
**Supersedes (for shifting only):** the 2026-07-19 "operator completion applies the move" rule.

## Context

Under the 2026-07-19 rule, a shed move (shifting) applied — relocated the animals in `goats` and
moved the head count — the instant the operator confirmed completion. Completion was self-attested:
no video, no independent check. Approval was already separated out as authorization-only ("a
permission slip is not evidence"), and the relocation lived on the operator's completion.

The maintainer decided a shed move must be proved the same way a vaccination is: the operator records
a **mandatory video**, and an **independent verifier** approves it before the move becomes real.

## Decision

A shifting movement now passes through a verification gate:

```
raise -> pending -> APPROVED (authorized; NOTHING moves)                     [manager, counts.approve_shifting]
      -> operator walks the animals + records a MANDATORY video
      -> PENDING VERIFICATION (event_status='pending_verification'; STILL nothing moves)  [operator, counts.write]
      -> verifier APPROVES the video -> APPLIED (animals relocate, count moves NOW)       [verifier, verification.review]
      -> verifier REJECTS the video  -> back to AUTHORIZED (bounce; operator re-shoots)    [verifier, verification.review]
```

- **The count moves at verifier approval, not at operator completion.** Between completion and
  approval the animals are physically in the destination shed while the census still reads the source
  shed. This lag is accepted deliberately in exchange for verified movement.
- **The video is mandatory.** A completion with no `proof_ref` is rejected (`422 proof_required`)
  before any state changes — there is nothing for a verifier to approve.
- **The operator records it with the live in-app camera.** Shifting exposes no gallery/import
  control. The captured file still uploads automatically through the proof outbox. This source rule
  does not alter Vaccination, whose existing gallery picker remains allowed.
- **Rejection bounces to `authorized`.** The operator re-records and re-submits; nothing relocated.
- **Birth and death are unchanged.** Those approvals still apply immediately, because for them the
  approval *is* the record of the fact.

The 2026-07-19 rule still stands as the *reason* approval never relocates; only the final leg
(operator-completion-applies) is superseded, replaced by verifier-approval-applies.

## Mechanics

Shifting reuses the generic Verification module (the same machinery vaccination uses):

- **Producer / enqueue** — `CompleteShiftingEvent` flips `authorized -> pending_verification`, stores
  the video in `shifting_events.proof_ref`, and (via the composition bridge
  `internal/countsbridge`) enqueues one verification item, category `shifting_move`, with the video as
  its media ref and a `SourceRef{module: counts, ref_type: shifting_event, ref_id: <shifting_event_id>}`.
- **Verifier verdict** — the verifier approves/rejects the item in the same queue as vaccination
  proofs. The verification module emits `verification.verdict.approved` / `verification.verdict.rework`.
- **Consumer / apply** — `counts/app.ShiftingVerificationHandler` subscribes to both verdict events,
  filters to `module=counts, ref_type=shifting_event`, and:
  - approved → `Repository.ApplyVerifiedShiftingEvent` — relocates the animals (the only place a
    shifting writes canonical location), emits `goat.location.changed` + `goat.stage_changed`, flips
    to `applied`. Idempotent; a re-delivered verdict relocates nobody.
  - rejected → `Repository.BounceShiftingEventForRework` — flips back to `authorized`, no relocation.

## Schema

Migration `000031_shifting_verification_gate.sql`:

- adds `pending_verification` to the `shifting_events` `event_status` domain;
- `shifting_events_pending_verification_proof_check` — a `pending_verification` row must carry a
  non-blank `proof_ref` (the mandatory video);
- `shifting_events_pending_verification_requires_authorization_check` — a video can only be submitted
  for a move a manager already authorized.

## Consequences

- The head count (herd register / counts breakdown, read live from `goats.shed_id`) reflects a move
  only after verifier approval. Any surface reading that count is automatically correct — there is
  one source of truth and it moves at one moment.
- A move can sit in `pending_verification` indefinitely if no verifier acts; it is visible in the
  verifier queue and never auto-applies. (A future SLA/escalation on stale `pending_verification`
  movements is out of scope here.)
- Idempotency is preserved end to end: completion replay re-enqueues the same item; verdict
  re-delivery relocates nobody twice.

## Proof

- `backend/internal/counts/adapters/postgres/shifting_verification_integration_test.go` — the
  production-path proof: no-video rejected; completion → `pending_verification` with nothing moved;
  verifier approval → relocation + count move + idempotent replay; rejection → bounce to `authorized`
  + re-submit.
- Registered in `context/architecture/domain-event-registry.json` under
  `verification.verdict.approved` / `verification.verdict.rework` (counts consumer).
