# Shifting applies after Park Head approval, then operator completion

**Status:** Accepted — maintainer decision, 2026-08-09 (APPROVE-FIRST).
**Supersedes:** the 2026-07-28 rule that made the two gates independent and order-free, which itself
superseded the 2026-07-26 rule that made verifier approval the relocation/count gate.

## Decision

A shifting has two ORDERED business gates and one evidence-review track:

```text
raise -> pending -> NOT in the operator's Actions work list
                    (read-only under the Pending tab: "Awaiting Park Head approval")

Park Head approval  -> authorized -> now in the work list, now executable (NOTHING MOVED YET)
operator completion -> APPLIED atomically (goat shed/stage + census move)

operator video -> generic Verification -> verified or evidence rework
                                      -> NEVER relocates or rolls back census
```

- The Park Head is the approval authority, and approval comes FIRST. Every movement, low and high
  priority alike.
- An unapproved movement is invisible to the operator's work list (`status=all` excludes
  `event_status='pending'`, and so does `status=rework`) and non-completable
  (`ErrShiftingNotAuthorized`, writing no proof, no completion stamp, and no verification item). The
  gate reads `authorization_state`, not `event_status`, because a legacy `pending_verification` row
  can still be unapproved.
- The raiser keeps visibility: the retained `pending` tab lists the movement read-only, with
  `primary_action_key='none'`.
- Why this replaced the order-free rule: an operator could burn the mandatory video — all THREE on a
  high-priority move — on a movement the park head then rejected, and a verifier could be handed
  evidence for a move nobody authorized.

### Actions lead time

An approved movement awaiting operator work enters Actions when it is DUE:

| Priority | Raised | Due |
|---|---|---|
| High | any time | immediately, to the second |
| Low | before 13:30 IST | next day, 00:00 IST |
| Low | at or after 13:30 IST | day after next, 00:00 IST |

High priority carries its own feed evidence, so nothing has to be prepared for it in advance. Low
priority is planned work: the destination has to be fed and the feed sheet has a packing day, so a
movement raised past the afternoon cutoff misses the next day's plan.

- A held movement keeps its RAISED business date and is absent from the queue until due; it does not
  move to a later date bucket. On its due day the operator pages back to the raise date, which is
  what the previous-dates strip is for.
- Anchored on RAISE time, so the due date a park head sees when approving is the one the operator
  gets — and a late approval needs no special case, since `now` is already past the due instant.
- Only `event_status='authorized'` rows are held. Applied movements (completed or in evidence
  rework) are history and are never held; `pending` rows are never held either.
- NOT an authority gate: completion is not blocked before the due date. The animals may genuinely
  have walked today, and refusing to record a movement that happened would make the herd register lie.
- **Actions queue only.** The feed-direction shifting projection keeps its 2026-07-27 rule — no lead
  time, no priority branch, counting `authorized` + `pending_verification` immediately. Do not
  collapse the two.

Canonical rule: `counts/domain.ShiftingActionsDueFrom`, mirrored in SQL by
`shiftingActionsVisibleSQL`, which the page query, the status counts, and the previous-dates strip
all share so a tab badge cannot advertise work the tab hides.
- Low-priority operator completion requires one live-camera shifting video in
  `shifting_events.proof_ref`; that existing flow is unchanged.
- High-priority shifting embeds feed packing and feeding inside Shifting. It shows the exact
  destination ration resolved from active Feed Config against the movement's EFFECTIVE management
  stage and the moved animals' breed groups. Three live-camera videos are mandatory: shifting,
  feed packing, and feed being given to the animal(s).
- **Effective stage = the snapshotted target stage, or, when that is blank, each ANIMAL's own current
  stage (maintainer decision 2026-08-12).** The target is blank whenever
  `ResolveShiftingDestinationStage` declines to adopt a destination cohort -- an EMPTY pen, a pen
  holding more than one cohort, a Flushing pen, or a cohort the relocation cannot write. That is a
  normal outcome meaning "keep each animal's current stage", not a missing input, and since the
  2026-08-03 decision the raiser is never asked for a stage at all. Pricing the ration off the blank
  target therefore hard-blocked EVERY high-priority movement into an empty pen with
  `selected destination management stage is missing` -- naming a choice the phone does not offer.
  The fallback prices exactly the cohort each animal keeps, and it is per ANIMAL: a movement
  carrying two cohorts prices each on its own grid and sums them.
  - Still blocked, because there is genuinely no cohort to price: an animal with no stage on either
    side. Its message names the herd-data gap rather than blaming the raiser.
  - The ration RATE and the shed TAG must key off the same effective stage; keying one off the
    target and the other off the animal would price one cohort against another's grid.
  - The config fingerprint's `string_agg` orders by breed, effective stage and pen. Breed alone
    stopped being unique once one breed can appear under two stages, and an unstable order would
    report `feed_config_changed` for a config nobody touched.
  - Pinned by `TestHighPriorityShiftingPricesRationPerAnimalStageWhenTargetStageIsBlank`
    (mutation-tested: restoring either pricing key to `target_stage` turns it red with the old
    blocked reason) and `TestHighPriorityShiftingStillBlocksWhenAnimalHasNoStageAtAll`.
- Embedded packing evidence is shifting-scoped only. It never creates or completes a separate Feed
  Packing or Feed Distribution session.
- Missing high-priority feed config blocks the task. The app echoes a semantic config fingerprint;
  completion re-resolves it while holding the shifting row lock and rejects changed config with
  `409 feed_config_changed`. No feed type or quantity is guessed.
- **THE RAISER PICKS BETWEEN TWO BACKEND-OWNED ANSWERS** (maintainer decision 2026-08-15,
  superseding the 2026-08-03 no-chooser rule on WHO decides). The raise form shows a two-position
  toggle — `keep_current` (the animals keep the tag they carry) or `destination_stage` (they adopt
  the destination pen's tag, the DEFAULT) — sent as `stage_mode`. Absent means `destination_stage`,
  so a client predating the toggle is unchanged; a present-but-invalid value is rejected with
  `invalid_stage_mode` rather than rewritten to the default.

  The safety property of the superseded rule is INTACT: `target_management_stage` is still rejected
  as an unknown field. The client sends a MODE and the server still resolves which tag that means,
  from the same catalog the form renders — so a phone cannot invent a cohort, cannot name one the
  relocation would refuse at the second gate, and cannot disagree with what the park head approved.
  The resolution still happens at raise time and is snapshotted on the shifting event, so the park
  head approves the same stage the completion applies. Do not widen the toggle back into a stage
  picker; that is the retired 2026-07-29 chooser.

  An unavailable option is GREYED OUT WITH A REASON. `GET /app/counts/shifting/destinations` carries
  `destination_stage` and `destination_stage_reason` per pen (exactly one non-empty), both
  backend-owned farm copy rendered verbatim. The catalog and the raise share one resolver, so the
  tag the toggle advertises is the tag the raise stamps.

  **FLUSHING IS ADOPTED; A CLINICAL STATE IS NOT.** The flushing carve-out is retired — the
  maintainer accepted that a move into a flushing pen puts the animal on flushing ration and re-keys
  her vaccination schedule (migration `000169` lists Flushing as writable). Bare `ICU` / `Quarantine`
  / `sick` / `under_treatment` / `recovering` remain refused, and are now refused at RAISE time via
  `protocol/domain.IsClinicalManagementStage` rather than only at the second gate — a tenant can
  legitimately list `ICU` in `animal_stage_lookup`, and resolving it at raise would kill the movement
  after the operator's video and the park head's approval. The clinical PEN names `ICU-Kid` /
  `Quarantine kids` are not states and stay writable (migration 000167).

  **THE DESTINATION IS A PEN, AND THE PEN'S OWN TAG IS THE COHORT** (maintainer decision
  2026-08-14, superseding the resident-derived rule for every movement). Animals never move into a
  bare shed; they move into one of its pens (`Godel 1 - Part 2`), so the cohort adopted is that
  PEN'S configured tag — `shed_partitions.animal_stage_id`, migration 000161, the same value the
  Counts → Breakdown Stage cell shows and edits. `counts/domain.ResolveShiftingDestinationPenStage` owns
  the rule.

  What this replaced, and why: the previous rule derived the cohort from the destination's RESIDENT
  animals, aggregated across the WHOLE SHED. Two failures followed. A shed whose eight pens
  legitimately hold different cohorts read as "mixed" and kept each animal's current stage, even
  when the pen actually chosen is unambiguously one cohort. And the raise matched the catalog on
  shed id alone, so the pen the operator picked never reached the resolver at all. The authored tag
  is also the better answer on its own terms: it is what somebody decided the pen is FOR, and it
  does not drift as animals move in and out.

  Strictly additive. A pen nobody has tagged yet still falls back to the resident-derived rule with
  every fallback it already had, so no movement that used to adopt a stage stops adopting one. A
  shed with no pens keeps using `shed_profiles`, because for such a shed the shed IS the
  operational location.

  The animal KEEPS ITS CURRENT STAGE when the destination's tag is Flushing (a nutrition cohort
  owned by its own workflow, not a placement consequence), when the tag is absent from active
  `animal_stage_lookup`, and — for an unconfigured pen falling back to residents — when the pen
  holds more than one cohort or holds no live animals. The unwritable case is not hypothetical:
  real sheds carry `ICU-Kid`, `ICU-Non-Pregnant` and `Quarantine kids`, which the relocation cannot
  write, so adopting one would pass the raise and then fail at the second gate after the operator's
  video and the park head's approval. A resolved `Mother` changes only `management_stage` and
  creates no pregnancy or lactation record.
- `goats.shed_id` and, when the raise resolved one, `management_stage` update in
  the same transaction as `shifting_events.event_status='applied'`. Herd Register and Counts read
  that canonical location, so their count changes at this exact second-gate transaction.
- Verification is evidence quality only. APPROVE sets `verification_state='verified'`. REWORK sets
  `verification_state='rejected'` and returns an evidence-rework card to Actions. Neither verdict
  writes goat location, changes `event_status='applied'`, or reverses a count.

## State transitions

```text
pending --operator complete--> REFUSED, nothing written (ErrShiftingNotAuthorized)
pending --Park Head approve--> authorized            (completion absent; no move)

authorized --operator complete--> applied             (move/count now)
authorized --operator cancel--> canceled              (no move)

applied --verifier approve--> applied + verified
applied --verifier rework--> applied + rejected       (no rollback)
rejected evidence --operator re-shoot--> applied + unverified/new item
```

`pending_verification`, and a `pending` row carrying completion stamps, remain only as
rollout-compatible states for rows created under the superseded order-free rule. The
approval-arrives-second apply branch in `authorizeShiftingEventInTx` exists solely to finish those
in-flight rows and MUST NOT be read as permission to complete before approval — the completion path
refuses that outright. New movements can no longer reach either state.

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
`shifting_event` grain. The five buckets stay disjoint and every non-canceled row lands in exactly
one, so nothing becomes unreachable:

| `status` | Contains | Actionable |
|---|---|---|
| `all` | the operator's WORK LIST — everything except `pending` and `canceled` | per row |
| `pending` | `event_status='pending'` — raised, awaiting Park Head approval | NO, read-only |
| `authorized` | approved, awaiting operator work | yes |
| `rework` | `verification_state='rejected'`, excluding `pending` and `canceled` | yes |
| `completed` | `applied` and not in evidence rework | no |

`primary_action_key` is backend-owned and is `none` for every `pending` row, so a client cannot make
an unapproved movement executable by rendering it differently. Each bucket's whole-filter count in
`status_counts` mirrors its page predicate exactly, so a tab's badge always equals what that tab
lists.

High-priority rows also carry one backend-owned `feed_requirement` at shifting-event grain: `ready`
with fingerprint/stage/feed totals, or `blocked` with the exact reason. Quantity covers the full
approved animal set, never the bounded animal preview.

The query is keyset-paginated by `(raised_at, shifting_event_id)`, limited to 20, and backed by the
partial indexes in migration `000050_shifting_actions_index.sql`.

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

- `shifting_approval_completion_integration_test.go`: raised Actions visibility; completion before
  approval is refused without writes; approval-first applies at completion; evidence rejection cannot
  roll back.
- `shifting_verification_integration_test.go`: low/high proof gates, exact feed resolution,
  stale-config rejection, evidence snapshot, and approve/rework idempotency.
- `countsbridge/shifting_verification_enqueue_test.go`: three high-priority videos stay together in
  one Shifting verification item.
- `approval_relocate_integration_test.go`: real identity transaction, location/stage events,
  rollback, stale placement, and raise-time management-stage selection checks.
- `feed_projected_counts_integration_test.go`: authorization, completion-before-approval, applied,
  rejected, and legacy rollout status matrix.
