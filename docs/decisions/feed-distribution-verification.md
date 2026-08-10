# Feed distribution AND packing require a verifier-approved video before a session is completed

**Status:** Accepted — maintainer decision, 2026-07-26; sequential camera-source clarification
2026-07-28.
**Supersedes:** the "operator marks a shed-session fed (optional video), row completed at submit"
behaviour as the operator completion path for BOTH feed DIRECTION and feed PACKING. Direction was
gated first; packing was gated in a follow-up decision the same day (see
[Feed packing (also gated)](#feed-packing-also-gated--follow-up-2026-07-26) below), which retired the
earlier "feed packing is deliberately NOT gated" carve-out. The old `feed_direction_session_completions`
table + `POST /feed-direction/complete` are now INERT (no longer navigated to), not deleted.

## Context

Under the original contract, a feed-direction shed-session was **completed the instant the operator
submitted it**. Video was OPTIONAL, there was no independent check, and the serve path reported the
row completed immediately. In practice the farm runs feed distribution the same way it runs
vaccination and shifting: the operator records proof and an **independent verifier** approves it
before the work counts as done. The Slack "Feed Automation Updates" flow already worked this way
(operator uploads a consumption video and a water-distribution photo/video per session; a reviewer
marks it verified/rejected).

Feed direction is also **not a web surface** — it is an app-only operator + verifier workflow. The
`/feed/direction` left-bar leaf is removed from admin-web nav; `/feed/config` (ration authoring) and
the packing worklist remain web surfaces.

**Feed packing was initially carved out, then also gated (same day).** When distribution was gated,
packing was deliberately left instant so the shared `feed_direction_session_completions` record was not
dragged into verification. A follow-up maintainer decision the same day gated packing too, on the SAME
pattern — its own separate new table (`feed_packing_completions`), one mandatory packing video, one
verifier approve. See [Feed packing (also gated)](#feed-packing-also-gated--follow-up-2026-07-26). The
old `feed_direction_session_completions` path (table, `POST /feed-direction/complete`,
`feed.direction.completed`, and the mobile `FeedCompleteScreen`) is now INERT — nothing navigates to it —
but is retained, not deleted; retiring it is a separate cleanup.

## Decision

A feed-direction shed-session now passes through a verification gate, for **both** the `normal` and
`experiment` workflows:

```
generated session (the ration; operator sees only the two proof prompts, not the ration detail)
  -> operator records a MANDATORY feed-distribution VIDEO with the live in-app camera
  -> only then the water action enables; operator records a MANDATORY water proof
     with the live in-app camera (PHOTO OR VIDEO)
  -> PENDING VERIFICATION (status='pending_verification'; NOTHING is completed yet)     [operator, feed.write]
  -> verifier APPROVES -> COMPLETED (the session is done NOW)                           [verifier, verification.review]
  -> verifier REJECTS  -> REWORK (operator re-shoots + re-submits)                      [verifier, verification.review]
```

- **The session is "completed" at verifier approval, not at operator submit.** The serving overlay
  (`ListCompletedSessions`, `status = 'completed'`) shows a session as done only after approval. The
  submit→approval lag is accepted deliberately in exchange for verified distribution.
- **Both proofs are mandatory.** A completion missing the feed-distribution video OR the water proof
  is rejected (`422 proof_required`) before any state changes — there is nothing for a verifier to
  approve. The feed-distribution proof must be a video; the water proof may be a photo or a video.
- **Capture is sequential and camera-only.** Water capture cannot start until the feed video is
  recorded. Feed Distribution exposes no gallery/import control for either proof. Automatic outbox
  upload remains unchanged. Vaccination is explicitly outside this rule and retains gallery upload.
- **One Accept per session covers both proofs.** The two media travel on a single verification item;
  the verifier approves (or rejects) the pair together.
- **Rejection bounces to `rework`.** The operator re-records and re-submits, which returns the row to
  `pending_verification` (row_version bumped) and enqueues a fresh verification item.

## Mechanics

Feed reuses the generic Verification module (the same machinery vaccination and shifting use):

- **Producer / enqueue** — `feeddirection` `CompleteDistribution` writes a NEW `feed_distribution_completions`
  row at `pending_verification`, stores the two proof ids in `distribution_proof_ref` +
  `water_proof_ref`, and (via the composition bridge `feeddirection/adapters/verificationbridge`)
  enqueues one verification item, category `feed_distribution`, with BOTH proofs as its media refs
  and a `SourceRef{module: feed, ref_type: feed_distribution_completion, ref_id: <completion_id>}`.
- **Verifier verdict** — the verifier approves/rejects the item in the same app queue as vaccination
  and shifting proofs (the queue is category-driven, so `feed_distribution` appears automatically).
  The verification module emits `verification.verdict.approved` / `verification.verdict.rework`.
- **Consumer / apply** — `feeddirection/app.FeedDistributionVerificationHandler` subscribes to both
  verdict events, filters to `module=feed, ref_type=feed_distribution_completion`, and:
  - approved → `DistributionCompletionStore.ApplyVerifiedDistribution` — flips `pending_verification →
    completed`, stamps `verified_by`/`verified_at`, and emits `feed.distribution.completed`.
    Idempotent; a re-delivered verdict completes nobody twice.
  - rejected → `DistributionCompletionStore.BounceDistributionForRework` — flips to `rework`, stores
    the reason, no completion.

The direction preview overlay (`overlayDirectionCompleted`) reads the NEW
`feed_distribution_completions` table (verified distributions); the packing overlay
(`overlayPackingCompleted`) reads the NEW `feed_packing_completions` table (see the packing section
below) — no overlay reads the inert `feed_direction_session_completions` table anymore.

## Schema

Migration `000032_feed_distribution_verification_gate.sql` adds a NEW table
`feed_distribution_completions` (the `000030` `feed_direction_session_completions` table is NOT
touched — it remains packing's completion record):

- grain `(tenant_id, park_id, shed_id, session_no, target_date, workflow)`, one gated distribution
  completion per shed-session;
- `status IN ('pending_verification','completed','rework')`;
- `distribution_proof_ref`, `water_proof_ref` (the two proof ids), `verified_by`, `verified_at`,
  `rework_reason`, plus the standard `idempotency_key`/`row_version`/audit columns;
- a `CHECK` requiring a `pending_verification` or `completed` row to carry BOTH proof refs;
- the `validate_outbox_event_tenant()` trigger gains a `feed_distribution_completion` branch so the
  `feed.distribution.completed` producer's outbox INSERT validates against this table.

## Consequences

- Feed session completion is now a two-phase, verifier-gated fact with one source of truth
  (`feed_direction_session_completions.status`), reflected on every surface that reads it. `completed`
  moves at one moment: verifier approval.
- A session can sit in `pending_verification` indefinitely if no verifier acts; it is visible in the
  verifier queue and never auto-completes. (A future SLA/escalation on stale pending sessions is out
  of scope here.)
- `feed.distribution.completed` is emitted on verifier approval. Any consumer of that event sees a
  verified completion, never a self-attested one. (`feed.direction.completed` is unchanged and still
  belongs to the packing/`feed_direction_session_completions` path.)
- Idempotency is preserved end to end: completion replay re-enqueues the same item; verdict
  re-delivery completes nobody twice; a rework re-submit bumps `row_version` and enqueues a new item.

## Proof

- `backend/internal/feeddirection/adapters/postgres/feed_distribution_verification_integration_test.go`
  — the production-path proof: missing-proof completion rejected; completion →
  `pending_verification` with nothing completed; verifier approval → `completed` + `feed.distribution.completed`
  emitted + idempotent replay; rejection → `rework` + re-submit.
- Registered in `context/architecture/domain-event-registry.json`: a new `feed.distribution.completed`
  producer, and a feeddirection consumer under `verification.verdict.approved` /
  `verification.verdict.rework`. The existing `feed.direction.completed` producer (old instant path) is
  unchanged but is now inert.

## Feed packing is proved ONCE PER PEN PER DAY — 2026-08-10

**Maintainer decision, 2026-08-10.** SUPERSEDES the shed-SESSION grain of the packing gate below,
for PACKING ONLY. Everything about the gate itself is unchanged: one mandatory video, one
verification item, completed only at verifier approval.

A packer packs a pen's whole day in one go. The worklist showed the pen **twice** — Morning and
Evening — and asked the crew to film the same work twice. Now:

```
ONE card per operational location per feed day
  Castro - 2                                   [Pending] [Normal]
    Morning                            17.8 kg
      Maize 12.4 · Soya 4.8 · Mineral mix 0.6
    Evening                            17.8 kg
      Maize 12.4 · Soya 4.8 · Mineral mix 0.6
    Pack total                         35.6 kg
  -> ONE mandatory packing video for the whole card
```

- **The sessions are a BREAKDOWN, not work items.** They keep their authored split and their own
  rounding, so the two figures on the card are exactly what the direction sheet prints. They carry
  no completion, proof or verification state, and none may be added — a per-session state would
  rebuild the two-card model one field at a time.
- **Completion grain is `(tenant, park, shed, partition, target_date, workflow)`**
  (migration `000148`). `session_no` is retained as a nullable-in-practice sentinel `0` so pre-merge
  rows stay readable as history; every new row carries `0`.
- **The PEN did NOT merge and must not.** Castro 1 and Castro 2 hold different animals on different
  rations. Migration `000137` exists because one Castro - 1 clip was closing out all three pens;
  collapsing the session is not licence to collapse the partition. Pinned by
  `TestPenDayMergeStillKeepsPartitionsApart` and `FeedPackingSessionBreakdownTest`.
- **Feed DISTRIBUTION is untouched** and is still gated per shed-SESSION. This is the first place the
  two flows diverge, deliberately. `completedKey` (session-bearing) stays for distribution;
  `packingCompletedKey` is the pen-day twin. They are separate functions rather than one with a `0`
  argument precisely so the next author cannot reuse the wrong one — doing so would mark a
  distribution session fed because its sibling was.
- **The verifier sees the whole day.** The item's subject is the pen (`Castro - 2`), with no
  `Session 1 ·` prefix, and its expected-ration context reads
  `Morning: Maize 12.5 kg · Soya 4 kg | Evening: Maize 12.5 kg · Soya 4 kg`. The per-session
  breakdown is REQUIRED there, not decoration: handed only a day total, a verifier could not
  distinguish a crew that packed the morning share twice from one that packed both correctly.
- **`session_no` is REJECTED, not ignored,** on `POST /feed-direction/packing/complete`
  (`additionalProperties: false` + `DisallowUnknownFields`). Accepted-and-ignored, a stale client's
  morning and evening submissions would both key the same pen-day row and the second would return
  the first's result as an already-pending no-op — the operator would see his evening video accepted
  while nothing recorded it.
- **There is no `session` filter** on `/feed-packing/worklist`. Narrowing a pen-day to one session
  could only mean "show the pen but hide half its bags".
- **`summary.line_count` halves; `total_kg_by_feed_item` does NOT.** A line is a pen-day, but the
  crew still carries out both bags. `SummarizePacking` folds over row × session for exactly this
  reason, and `TestPackingWorklistServesOneLinePerPenDayCarryingEverySession` pins the pair.

Migration `000148` withdraws a superseded row's PENDING verification item **before** deleting the
row, so nothing is left pointing at a completion that no longer exists — an orphaned pending item is
one a verifier could approve every day with nothing ever happening. It also strips the `Session N · `
prefix from in-flight item labels. It is deliberately NOT reversible.

**The key swap is an EXPAND/CONTRACT rollout, and the contract half is NOT in this release.**
`000148` ADDS the pen-day unique index and deliberately KEEPS the session-bearing one. The deployed
binary inserts with `ON CONFLICT (…, session_no, …)`, and Postgres requires a unique index matching
that exact column list — so dropping it before every instance runs the new binary makes them fail
EVERY packing submission with `42P10`.

**Splitting the SQL into two files does not fix that on its own, and it is worth being precise about
why.** `backend/cmd/migrate` applies *every* pending migration sequentially in one run; there is no
per-release gate and no staged-apply flag. A contract migration sitting next to `000148` in the same
release would therefore run back-to-back with it, still before the new revision serves, and recreate
the identical window. Only shipping the two halves in two **releases** fixes it, and a header comment
saying "apply this later" cannot enforce a release boundary.

So the drop is simply absent from this release. It is authored in a LATER one, once every API
instance runs the pen-day binary, and is machine-blocked until then by
`make feed-packing-rollout-guard`, which fails on any post-`000148` migration that drops
`feed_packing_completions_natural_uq`. Landing the contract step means retiring that guard in the
same change — a visible, reviewable act rather than a silent one. Until then the two indexes coexist
safely: the pen-day one is strictly stricter, and step 3 has already collapsed the rows that would
violate it.

**A pen-day accepts exactly ONE video, and a second DIFFERENT one is a 409, never a success.**
`ErrPackingAlreadyRecorded` → `409 packing_already_recorded`. The upgrade case forced this: the
phone can hold TWO legacy queued packing rows for one pen (Morning and Evening, each with its own
video and its own idempotency key), both draining to the same pen-day row. The second used to take
the "already awaiting verification" branch and return **200 with its video discarded** — the
accepted-and-ignored failure the strict `session_no` rejection exists to prevent, reappearing one
layer above the API. A genuine retry is unaffected: an identical request replays on its idempotency
key, and the same `packing_proof_ref` re-sent under a new key still matches the stored proof and
stays a quiet no-op. Only a *different* video conflicts. The client terminalizes the 409 rather than
retrying it.

**admin-web is intentionally unchanged.** `/feed/packing` flattens `sessions[]` straight back into
per-session table rows: the web packing sheet is a printed worklist a packer reads down, not the
phone's capture card, and the session is still the line they physically fill a bag for.

## The afternoon correction reopens an already-packed pen — 2026-08-10

A low-priority movement raised at 09:00 is due **tomorrow**. Tomorrow's normal sheet was issued at
**07:00 that same morning** and is already being packed. Ten animals arriving in a pen that was
packed for one had **no feed at all** — the projection was waiting for a park head to approve a
movement the operator had already been told to plan around.

Two changes fix it, at 14:00, the `correction_time` both workflows already run on. **No new clock is
introduced and no lock is lifted:** the transport lock is 15:30, *after* the correction, so
`AmendDirection` was never blocked by it. `ErrAmendAfterLock` stands — past the transport cutoff the
feed has physically left the store and a correction cannot reach the shed.

### 1. A raised movement counts before it is approved

`pending_event` gained a second branch: `authorization_state='pending' AND event_status='pending'`.
The branches are **disjoint on `authorization_state`**, so a movement contributes exactly once as it
travels from raised to approved rather than being double-counted at the handover. `rejected`,
`canceled` and `applied` never appear, so a movement the park head turns down stops feeding a shed
immediately — approval no longer starts the feed clock, only rejection stops it.

Its effective date is the **ACTIONS lead time** (`ShiftingActionsDueFrom`), not the raise day. An
unapproved movement has no authorization instant to anchor on, and the honest answer to "when do
these animals eat here" is the day they are expected to walk: low priority raised before 13:30 IST →
tomorrow, at or after → the day after. Anchoring on the raise day instead would feed a destination a
full day before a 13:45 raise's animals actually move — the same over-feeding bug, one day earlier.

An **approved** movement keeps the 2026-07-27 rule untouched (effective = the authorization business
date, no lead, no priority branch). `TestRaisedAndAuthorizedRulesStayDistinct` exists because the two
look alike and answer different questions.

### 2. A pen already packed against the old count is sent back

`AmendDirection` → `reopenPackingForCorrection` → `ReopenPackingForFeedChange` moves the pen's
completion to `rework`, stores an operator-facing sentence, `withdrawn`s its still-pending
verification item, and clears `verified_by`/`verified_at`.

- **An already-APPROVED video is reopened too.** It proves the packer packed the OLD quantity, which
  is now the wrong quantity; an approved clip is no more usable than an unapproved one. A row already
  in `rework` is left alone — touching it would overwrite a verifier's real rejection reason.
- **Experiment is EXEMPT.** Its rations are authored as absolute kg per pen, so a head-count change
  moves no quantity there. Reopening one would discard a good video for a sheet that did not change.
- **Head count only, per PEN.** `AffectedShedIDs` also fires for a relabelled ration group and is
  shed-wide, so driving the reopen from it would make the packers of Castro - 1 and Castro - 3 refilm
  because Castro - 2 gained animals. `CellDiff.HeadCountChangedPens` is the strictly narrower signal:
  a grain's head count moved, a grain appeared (animals arrived), or a grain vanished (animals left).

### There is no new state, so the reason is not optional

A reopened pen uses the existing `rework`, which `NormalizeSessionStatus` folds into the client bucket
`pending` — "needs my action again". **The chip therefore cannot distinguish a reopened pen from one
nobody has packed.** `FeedPackingRow.rework_reason` is the only thing that can, which is why it is a
backend-composed sentence rendered verbatim rather than client-side copy derived from the status. The
`feed_packing_worklist` Paparazzi golden puts the reopened pen first, above the fold, precisely so the
image would change if that line were dropped.

## Feed packing (also gated) — follow-up, 2026-07-26

> **Grain superseded 2026-08-10** — see the section above. Everything below describes the gate
> correctly; only "shed-session" should now read "pen-day" for packing.

The same day distribution was gated, the maintainer extended the gate to feed **PACKING**, retiring the
"feed packing is deliberately NOT gated" carve-out above. Packing is gated on the identical pattern,
with one difference: packing needs **ONE mandatory video** (no water proof), so it is a strictly
simpler single-proof version of the distribution flow.

Packing exposes only live in-app camera recording; it has no gallery/import option. Proof upload
after capture remains automatic. This does not change Vaccination's gallery picker.

```
packing session -> operator records ONE MANDATORY packing VIDEO with the live in-app camera
  -> PENDING VERIFICATION (feed_packing_completions.status='pending_verification'; NOTHING completed)
  -> verifier APPROVES -> COMPLETED (feed.packing.completed emitted)
  -> verifier REJECTS  -> REWORK (operator re-shoots + re-submits)
```

- **Separate new table + record.** Migration `000033_feed_packing_verification_gate.sql` adds
  `feed_packing_completions` (same grain `(tenant, park, shed, session_no, target_date, workflow)`,
  same `status` set, one `packing_proof_ref` instead of two proofs, same idempotency/row_version/audit
  columns, same `CHECK` requiring the video on `pending_verification`/`completed`). It is NOT an ALTER
  of the old `feed_direction_session_completions` table. The `validate_outbox_event_tenant()` trigger
  gains a `feed_packing_completion` branch.
- **Producer / enqueue** — `feeddirection` `CompletePacking` writes the pending row and (via
  `feeddirection/adapters/verificationbridge.NewPacking`) enqueues one verification item, category
  `feed_packing`, `SourceRef{module: feed, ref_type: feed_packing_completion}`, with the video as its
  single media ref. Both feed gates share `module=feed`; the DISTINCT `ref_type` is what keeps the two
  handlers from cross-firing.
- **Consumer / apply** — `feeddirection/app.FeedPackingVerificationHandler` filters to
  `module=feed, ref_type=feed_packing_completion` and calls `ApplyVerifiedPacking` (approve →
  `completed` + `feed.packing.completed`, idempotent) or `BouncePackingForRework` (reject → `rework`).
- **Overlay** — `overlayPackingCompleted` now reads `ListVerifiedPacking` (verified rows only).
- **Route** — `POST /feed-direction/packing/complete` (422 `proof_required` when the video is blank),
  registered in `permissions/routes.go` on `FeedDirectionComplete`. Registering it also surfaced that
  the distribution route had never been added to `routes.go` — an unregistered route 403s
  (`route_not_registered`), so both are now registered.
- **Mobile** — a new `FeedPackingCompleteScreen`/`FeedPackingCompleteViewModel` (one mandatory video,
  camera-only), outbox op `FEED_PACKING_COMPLETE`, `enqueueFeedPackingComplete` /
  `dispatchFeedPackingComplete`. The packing row tap now opens this screen; the old
  `FeedCompleteScreen`/`feedCompleteRoute` is left inert.

**Proof:** `backend/internal/feeddirection/adapters/postgres/feed_packing_verification_integration_test.go`
— missing-video rejected; completion → `pending_verification`; verifier approval → `completed` +
`feed.packing.completed` + idempotent replay; rejection → `rework` + re-submit. Registered in the
domain-event registry as a new `feed.packing.completed` producer plus feeddirection consumers under the
verdict events.
