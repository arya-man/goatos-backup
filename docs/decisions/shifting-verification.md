# Shifting applies after Park Head approval and operator completion

**Status:** Accepted — maintainer decision, 2026-07-28.
**Supersedes:** the 2026-07-26 rule that made verifier approval the relocation/count gate.

## Decision

A shifting has two independent business gates and one evidence-review track:

```text
raise -> pending -> visible in Android Actions immediately

Park Head approval ─┐
                    ├─ when BOTH exist -> APPLIED atomically
operator completion ┘                    (goat shed/stage + census move)

operator video -> generic Verification -> verified or evidence rework
                                      -> NEVER relocates or rolls back census
```

- The Park Head is the approval authority.
- Approval and completion may arrive in either order. The first fact is stored without relocating;
  the transaction recording the second fact applies the move.
- Low-priority operator completion requires one live-camera shifting video in
  `shifting_events.proof_ref`; that existing flow is unchanged.
- High-priority shifting embeds feed packing and feeding inside Shifting. It shows the exact
  destination ration resolved from active Feed Config against the snapshotted target management
  stage and the moved animals' breed groups. Three live-camera videos are mandatory: shifting,
  feed packing, and feed being given to the animal(s).
- Embedded packing evidence is shifting-scoped only. It never creates or completes a separate Feed
  Packing or Feed Distribution session.
- Missing high-priority feed config blocks the task. The app echoes a semantic config fingerprint;
  completion re-resolves it while holding the shifting row lock and rejects changed config with
  `409 feed_config_changed`. No feed type or quantity is guessed.
- The raiser does not choose a management stage (maintainer decision 2026-08-03, superseding the
  `keep_current` / `select_stage` / `destination_stage` chooser). A movement ADOPTS THE DESTINATION
  SHED's cohort, resolved server-side at raise time by
  `counts/domain.ResolveShiftingDestinationStage` and snapshotted on the shifting event, so the park
  head approves the same stage the completion applies. Clients send neither
  `management_stage_mode` nor `target_management_stage`; both are rejected as unknown fields.
  The animal KEEPS ITS CURRENT STAGE when the destination is a Flushing shed (flushing is a
  nutrition cohort owned by its own workflow, not a placement consequence), and — because an
  ambiguous destination has no truthful answer — when the shed holds more than one cohort, holds no
  live animals, or holds a cohort absent from active `animal_stage_lookup`. That last case is not
  hypothetical: real sheds carry `ICU-Kid`, `ICU-Non-Pregnant` and `Quarantine kids`, which the
  relocation cannot write, so adopting them would pass the raise and then fail at the second gate
  after the operator's video and the park head's approval. `shed_profiles` remains not
  movement-stage authority. A resolved `Mother` changes only `management_stage` and creates no
  pregnancy or lactation record.
- `goats.shed_id` and, when the raise resolved one, `management_stage` update in
  the same transaction as `shifting_events.event_status='applied'`. Herd Register and Counts read
  that canonical location, so their count changes at this exact second-gate transaction.
- Verification is evidence quality only. APPROVE sets `verification_state='verified'`. REWORK sets
  `verification_state='rejected'` and returns an evidence-rework card to Actions. Neither verdict
  writes goat location, changes `event_status='applied'`, or reverses a count.

## State transitions

```text
pending --operator complete--> pending + completed_at/by/proof (approval absent; no move)
pending --Park Head approve--> authorized            (completion absent; no move)

pending+completed --Park Head approve--> applied     (move/count now)
authorized --operator complete--> applied             (move/count now)

pending/applied --verifier approve--> same event_status + verified
pending/applied --verifier rework--> same event_status + rejected
rejected evidence --operator re-shoot--> same movement state + unverified/new item
```

`pending_verification` remains only as a rollout-compatible status for rows created under the
superseded rule; new completion-before-approval rows stay `pending` and use explicit completion stamps.

## Atomic writer and event spine

`applyAuthorizedCompletedShiftingInTx` is the only shifting relocation writer. Both
`CompleteShiftingEvent` (completion second) and `authorizeShiftingEventInTx` (approval second) call
it while holding the shifting row lock. It:

1. proves authorization and stored operator completion;
2. reads the exact animal set from the approved request payload;
3. revalidates source placement and the snapshotted destination shed profile;
4. calls the identity transaction seam to update shed/stage and write identity history;
5. emits per-animal `goat.location.changed` and, when applicable, `goat.stage_changed` through the
   transactional outbox; and
6. flips the shifting event to `applied` with the completing operator as actor.

Any failure rolls back the second gate and every relocation/event/count effect together. Exact
completion or approval replay cannot relocate twice.

`CompleteShiftingEvent` also enqueues one generic `shifting_move` verification item through
`internal/countsbridge`, keyed by shifting event + proof set. Low priority carries one video; high
priority carries all three videos together. The verdict consumer remains
`counts/app.ShiftingVerificationHandler`, but its counts-side effect is evidence state only. A
pre-000049 legacy row already holding approval + completion may be lazily applied by the approved
verdict handler once during rollout compatibility; new rows always apply at the second business gate.

## Actions read contract

`GET /app/counts/shifting-events/pending-execution` is the bounded Android Actions read at
`shifting_event` grain. It includes:

- `pending` with no proof — raised, awaiting approval and operator work;
- `authorized` with no proof — approved, awaiting operator work; and
- `verification_state='rejected'` — evidence rework, including an already-applied move.

High-priority rows also carry one backend-owned `feed_requirement` at shifting-event grain: `ready`
with fingerprint/stage/feed totals, or `blocked` with the exact reason. Quantity covers the full
approved animal set, never the bounded animal preview.

It excludes completed `pending` rows and ordinary `applied` rows while evidence review proceeds. The
query is keyset-paginated by `(raised_at, shifting_event_id)`, limited to 20, and backed
by the partial indexes in migration `000050_shifting_actions_index.sql`.

## Schema and rollout

- `000031_shifting_verification_gate.sql` remains historical: it introduced mandatory proof and
  `pending_verification`.
- `000049_shifting_approval_completion_gate.sql` adds nullable `completed_at`/`completed_by` without
  redefining a hot-table status constraint; application writes keep proof and completion stamps together.
- `000050_shifting_actions_index.sql` adds concurrent partial indexes for the Actions query.
- `000053_high_priority_shifting_feed_evidence.sql` adds nullable feed-proof, fingerprint, and
  requirement-snapshot columns while preserving low-priority and historical rows.

## Feed projection

Feed projected counts still begin at Park Head authorization. A normal new row is either
`authorized` (delta included) or `applied` (canonical goat location already includes the move, so
delta excluded). The SQL retains the legacy `(pending_verification + authorized)` shape only so an
in-flight pre-000049 row is not under-fed during rollout. Completion-before-approval does not affect
feed projection.

## Proof

- `shifting_approval_completion_integration_test.go`: raised Actions visibility; completion-first;
  approval-first; evidence rejection cannot roll back.
- `shifting_verification_integration_test.go`: low/high proof gates, exact feed resolution,
  stale-config rejection, evidence snapshot, and approve/rework idempotency.
- `countsbridge/shifting_verification_enqueue_test.go`: three high-priority videos stay together in
  one Shifting verification item.
- `approval_relocate_integration_test.go`: real identity transaction, location/stage events,
  rollback, stale placement, and raise-time management-stage selection checks.
- `feed_projected_counts_integration_test.go`: authorization, completion-before-approval, applied,
  rejected, and legacy rollout status matrix.
