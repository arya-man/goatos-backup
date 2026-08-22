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
- **THREE proofs are mandatory** (maintainer decision 2026-08-11; it was two until then). A
  completion missing any one is rejected (`422 proof_required`) before any state changes — there is
  nothing for a verifier to approve. In capture order:

  | # | Proof | Kind | Notes |
  |---|-------|------|-------|
  | 1 | `feed_weight_proof_ref` | **PHOTO** | The weighed feed, before it is given out. Live camera ASSERTED server-side. |
  | 2 | `distribution_proof_ref` | **VIDEO** | Unchanged. |
  | 3 | `water_proof_ref` | **VIDEO** | Was photo-OR-video until 2026-08-11. |

  **Why the weight photo.** The distribution video proves the feed reached the animals; it cannot
  prove HOW MUCH reached them, because a scale reading is not legible in a clip of feed being poured.
  The weight photo is the only capture that can be checked against the expected ration the verifier
  is already shown on the item.

  **Why it is FIRST.** It can only be taken while the feed is still on the scale. After distribution
  there is nothing left to weigh, so a later capture could only ever be staged.

  **Why water is now video-only.** A photo of a full trough proves a trough is full, not that this
  operator filled it today. The distribution video was always held to that standard.

- **The media KIND is enforced server-side, not trusted from the client.** The phone chooses which
  capture button it shows; a client built before this rule, or an outbox row queued under it, would
  happily send a water photo. `feeddirection/adapters/proof.Validator.ValidateFeedProofMedia` checks
  BOTH the declared `proof_type` and the stored `mime_type` — they are written by different steps of
  the upload (`/app/proofs/uploads` then `/app/proofs/{id}/complete`) and can genuinely disagree. The
  weight photo additionally requires `capture_source = in_app_camera`: it is the capture that carries
  a NUMBER, and a gallery still of a scale is a reading from some other day.
- **Capture is camera-only; slots are independent and parallel (SUPERSEDED: "sequential").** The
  original 2026-07-26 wording said each step unlocks the next (weight → feed → water). Superseded
  2026-08-15 by `docs/product/feed-proof-collaboration.md`: the three proof slots belong to one
  shared shed-session that multiple peer operators fill simultaneously from their own phones, in any
  split — no slot gates another. Camera-only stands: Feed Distribution exposes no gallery/import
  control for any proof. Automatic outbox upload remains unchanged. Vaccination is explicitly
  outside this rule and retains gallery upload.
- **One Accept per session covers all three proofs.** The three media travel on a single verification
  item; the verifier approves (or rejects) the set together.
- **Rejection bounces to `rework`.** The operator re-records and re-submits, which returns the row to
  `pending_verification` (row_version bumped) and enqueues a fresh verification item.

## Mechanics

Feed reuses the generic Verification module (the same machinery vaccination and shifting use):

- **Producer / enqueue** — `feeddirection` `CompleteDistribution` writes a NEW `feed_distribution_completions`
  row at `pending_verification`, stores the three proof ids in `feed_weight_proof_ref` +
  `distribution_proof_ref` + `water_proof_ref`, and (via the composition bridge
  `feeddirection/adapters/verificationbridge`)
  enqueues one verification item, category `feed_distribution`, with ALL THREE proofs as its media
  refs in capture order
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
- `distribution_proof_ref`, `water_proof_ref` (the original two proof ids), `verified_by`,
  `verified_at`, `rework_reason`, plus the standard `idempotency_key`/`row_version`/audit columns;
- a `CHECK` requiring a `pending_verification` or `completed` row to carry BOTH of those proof refs;
- the `validate_outbox_event_tenant()` trigger gains a `feed_distribution_completion` branch so the
  `feed.distribution.completed` producer's outbox INSERT validates against this table.

### Migration `000151_feed_distribution_weight_proof.sql` (2026-08-11)

Adds `feed_weight_proof_ref` (nullable text) and a SECOND, separate `CHECK`. Two properties of that
check are load-bearing and must survive any future edit:

- **`NOT VALID`.** Sessions already submitted, and items sitting in the verifier queue at deploy
  time, keep their two proofs and are verdicted normally — nobody re-shoots work already done. A
  `NOT VALID` check binds every INSERT/UPDATE from that point on while never scanning the rows
  already on disk. Do **not** later `VALIDATE` it: validating would fail on precisely the legacy rows
  the decision protects.
- **It covers `pending_verification` ONLY, not `completed`.** A `NOT VALID` check *is* enforced when
  an old row is UPDATED, and verifier approval is an UPDATE. A check that also covered `completed`
  would therefore find the NULL weight photo at the exact moment a verifier tried to approve a
  grandfathered item and refuse — making every in-flight queue item permanently un-approvable on
  deploy day, the opposite of grandfathering. The narrowing costs nothing because `completed` is
  reachable ONLY from `pending_verification` (`ApplyVerifiedDistribution` guards on it twice), which
  this check already enforces. If a future change ever writes `completed` directly, this check must
  be widened in the SAME change — with a plan for the legacy rows.

The re-submit path stays fully enforced, deliberately: a rejected row goes to `rework` (exempt, so
proofs can be cleared for the re-shoot) and comes back through `pending_verification`, where the
check fires. A re-shoot is new work, captured under the new rule.

Pinned by `TestGrandfatheredDistributionRowWithoutWeightPhotoStillVerifies` (which reproduces the
deploy ORDER — legacy row first, migration second — because `NOT VALID` exempts nothing that is
inserted afterwards) and `TestGrandfatheredRowReSubmitMustCarryWeightPhoto`.

**Known rollout cost, accepted:** a completion queued OFFLINE by a pre-2026-08-11 build carries no
weight photo and cannot be healed — the feed has been given out, so the photo no longer exists to
take. `SyncEngine.dispatchFeedDistributionComplete` fails such a row TERMINALLY with a farm-language
reason, returning the shed-session to the operator's list as work still needing action (the same
place a verifier bounce puts it) rather than retrying invisibly until someone notices the feeding
never registered.

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

## Feed packing is proved ONCE PER BAG — 2026-08-11

**Maintainer decision, 2026-08-11.** REVERTS the 2026-08-10 pen-day grain in full and restores the
shed-SESSION grain of the packing gate below. Everything about the gate itself is unchanged
throughout: one mandatory video per line, one verification item, completed only at verifier approval.

For one day (2026-08-10 → 2026-08-11) packing was shown as ONE card per pen per day with the morning
and evening shares as a nested breakdown, backed by ONE video. That was wrong, and the reason is one
sentence: **one clip cannot prove two bags.** The two shares are weighed out at different times, so a
single video shows at most one of them, and a verifier judging it against a day total cannot tell a
crew that packed the morning share twice from one that packed both correctly.

```
ONE card per operational location PER FEEDING SESSION
  Castro - 2                                   [Pending] [Normal]
    Morning
      Maize 12.4 · Soya 4.8 · Mineral mix 0.6
    Pack total                         17.8 kg
  -> ONE mandatory packing video for THIS bag

  Castro - 2                                   [Pending] [Normal]
    Evening
      Maize 12.4 · Soya 4.8 · Mineral mix 0.6
    Pack total                         17.8 kg
  -> ONE mandatory packing video for THIS bag
```

- **Completion grain is `(tenant, park, shed, partition, session_no, target_date, workflow)`** —
  exactly what `feed_packing_completions_natural_uq` has always indexed. Migration `000150` restores
  the grain; see below for what it can and cannot undo.
- **`session_no` is REQUIRED** on `POST /feed-direction/packing/complete`. A missing or `0` value is
  rejected (`ErrInvalidSession`). `0` is not "the whole day": it is a value no worklist line matches,
  so accepting it would record the operator's work against a row their bag never resolves to, and the
  bag would still show as owed. The database agrees — `CHECK (session_no >= 1)`.
- **The `session` filter is back** on `/feed-packing/worklist` (absent/`0` = every session), and
  `summary.line_count` counts pen×session lines. A packer works one bag at a time, so narrowing to
  the bag in front of them is real work rather than decoration. The web sheet at `/feed/packing`
  deliberately does not offer the control — it is a printed worklist read down in one pass, and
  hiding half the day's bags from it would understate what the crew must carry out.
- **The verifier sees ONE bag.** The item's subject is `Session 1 · Castro - 2`; without the prefix a
  verifier holding a pen's two cards cannot tell which clip proves which bag. Its expected-ration
  context names THAT SESSION's quantities (`Maize 12.5 kg · Soya 4 kg`), never the day's — handed the
  day total she would be checking the clip against twice what it should contain.
- **The PEN is part of the key, for a separate reason, and survived the merge.** Castro 1 and
  Castro 2 hold different animals on different rations. Migration `000137` exists because one
  Castro - 1 clip was closing out all three pens. Pinned together with the session by
  `TestPackingLinesKeepPartitionsAndSessionsApart` and `FeedPackingSessionRowTest`.
- **Feed DISTRIBUTION was never merged** and needed no repair. Both flows are gated per shed-session
  and share the session-bearing `completedKey` again; `packingCompletedKey` is deleted.
- **A line accepts exactly ONE video, and a second DIFFERENT one is a 409, never a success.**
  `ErrPackingAlreadyRecorded` → `409 packing_already_recorded`. This was introduced for the pen-day
  upgrade and is KEPT, because it is not specific to that grain: the morning and evening submissions
  no longer collide, but a re-send after a rework the server never recorded, or a duplicated queue
  drain, still reaches this branch. Returning 200 there told an operator their recording was accepted
  while nothing stored it and no verifier ever saw it. A genuine retry is unaffected — an identical
  request replays on its idempotency key, and the same `packing_proof_ref` re-sent under a new key
  still matches the stored proof and stays a quiet no-op. Only a *different* video conflicts, and the
  client terminalizes the 409 rather than retrying it.

### What migration `000150` can and cannot undo

`000149` did three destructive things. `000150` reverses the reversible one and is explicit about the
rest:

| `000149` did | `000150` does |
|---|---|
| ADDED `feed_packing_completions_pen_day_uq` | DROPS it |
| Set every surviving row's `session_no` to `0` | PROMOTES it to `1` |
| DELETED the losing row of each collapsed pen-day | **Cannot restore it** |

A promoted row's video and verdict stand as the **morning** packing; the pen's evening carries no
completion row and reappears on the worklist as work still owed. Nothing an operator filmed and
nothing a verifier approved is discarded. The rows `000149` deleted are gone, and those pens' second
bags simply reappear unpacked — the honest state, since no video for that session exists any more. If
a `session_no = 0` row cannot be promoted (a colliding session-1 row, which should be impossible),
the migration RAISES rather than choosing silently between destroying a video and inventing a session
number.

`000149` is **not amended**. It is already applied on STG, and STG records migration checksums, so
editing an applied file makes every later migration fail before it runs. Forward-only, always.

**No rollout window this time, and it is worth saying why** — `000149` needed a whole expand/contract
dance for exactly this. The index the reverted binary's `ON CONFLICT` resolves against
(`feed_packing_completions_natural_uq`) was deliberately KEPT by `000149`, and its contract half was
never shipped. So the key this release needs is already in place before `000150` runs. Backend and app
ship in one deploy, so there is no window in which an old instance submits against a key that is gone.
`make feed-packing-rollout-guard` existed only to protect that unfinished rollout and is **retired**
in this change — script, manifest entry, Make target and CI wiring together.

The Android outbox needs the same promotion the database gets: a packing row queued by the pen-day
build carries no session, decodes as `0`, and `SyncEngine` maps it to session 1 — matching `000150`
server-side, so phone and database agree on what an unlabelled pen-day video proves. The Room packing
cache namespace is bumped to `session-v3`; a stale `sessions`-shaped cached row would otherwise
deserialize WITHOUT ERROR into a card with **no feed lines at all**, because kotlinx-serialization
fills the missing field with its default empty list.

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

**EVERY SESSION of a reopened pen comes back** (2026-08-11, once packing returned to the shed-SESSION
grain). Head count scales the morning and the evening ration alike, so both of that pen's videos now
prove the wrong quantity; reopening one would leave the other bag packed for a head count the farm no
longer has. `ReopenPackingForFeedChange` therefore names pens WITHOUT a session and applies no session
predicate — the golden shows Castro - 2 twice, Morning and Evening, both carrying the sentence.

A verdict already CAST is kept as history rather than rewritten: only a still-`pending` item is
`withdrawn`, because that is the one sitting in a verifier's queue pointing at a stale clip. An
approved item is closed and in nobody's queue, and rewriting it would erase the fact that a verifier
genuinely watched and passed that video — what protects the operator is the completion row returning to
`rework` with `verified_by`/`verified_at` cleared. Both halves are pinned by
`TestReopenPackingWithdrawsPendingItemsAndKeepsCastVerdicts`, mutation-tested two ways (a session
predicate on the reopen; withdrawing a cast verdict).

## Feed packing (also gated) — follow-up, 2026-07-26

> **Grain note** — briefly superseded on 2026-08-10 by a pen-DAY grain and RESTORED on 2026-08-11;
> see the section above. Everything below describes the gate correctly, and "shed-session" reads
> correctly again.

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

## Feed WASTAGE — experiment pens, and the VERIFIER records the number — 2026-08-18

Feed Wastage is the fourth feed gate (maintainer decision 2026-08-18): every feed day, each pen on
a hand-authored feed EXPERIMENT owes ONE wastage video — the operator films the leftover feed and
submits, nothing more. The verifier watches the clip; when she can read the leftover weight in it
she RECORDS that weight in kg and approves, and when she cannot she rejects for a re-shoot.

What makes wastage different from its three siblings, and why each difference exists:

- **PEN-DAY grain, no session.** Packing and distribution are per-bag work (a pen's morning and
  evening are two bags, two videos), but wastage is what is LEFT OVER after the day's feeding,
  measured once. Table `feed_wastage_completions` (migration `000176`), natural key
  `(tenant, park, shed, partition_key, target_date, workflow)`.
- **EXPERIMENT ONLY, by derivation, not by flag.** The worklist (`GET /feed-wastage/worklist`) is
  derived from the day's FROZEN experiment sheet — the same rows packing reads — so "which pens
  are on the experiment today" has exactly one source of truth. The write refuses a pen the sheet
  does not cover (`422 not_experiment_pen`), and the table's CHECK pins `workflow = 'experiment'`.
- **THE VERIFIER OWNS THE NUMBER.** The operator submits no number at all; the measured leftover
  is born on the verifier's screen. It rides the measurement-correction mechanism the weighing
  weight correction introduced (registry `MeasurementCorrectionSpec` on category `feed_wastage`;
  producer route `POST /feed-direction/wastage/{completion_id}/measurement`, gated on
  `verification.verdict`), REPLACES on re-entry, relabels the queue item with the value, and never
  changes the completion's status — the verdict owns the lifecycle. ZERO IS A VALID MEASUREMENT
  (an empty trough), so the range check is `>= 0`, deliberately unlike weighing's `> 0`.
- **THE APPROVE CARRIES THE NUMBER, and for wastage it MUST — 2026-08-20, SUPERSEDING the
  "recording is deliberately NOT a hard gate on approve" rule this bullet used to state.**

  That rule made recording and approving two separate acts on two routes. Two things went wrong
  with it, and both were reported from the field:

  1. **The save broke the approve.** Recording the measurement relabels the verification item, and
     the relabel is `row_version = row_version + 1`. The verdict UPDATE is version-fenced
     (`AND row_version = $6`), so the Approve she pressed straight afterwards carried the version
     her screen had loaded with, matched no row, and silently did nothing.
  2. **Approving without recording completed a pen-day with no wastage at all.** The producer's
     own `ErrWastageMeasurementRequired` fires in the verdict CONSUMER, after the verdict is
     already durable, so it stranded the item mid-apply instead of telling her to enter the number.

  The number now travels ON the verdict (`measurement` on
  `POST /verification/items/{item_id}/verdict`). The old objection — that coupling them would make
  verification read a producer's table — is answered by a seam, not by a read:
  `verificationapp.MeasurementApplier`, registered per category at composition time exactly like
  the enqueue/withdraw/relabel seams the producers already register. Verification still does not
  know what the number MEANS; it holds the copy and the decision, and the producing module owns the
  write. It calls the SAME `WastageMeasurementService` the standalone route calls, so the range
  check, idempotency, audit row and relabel are one implementation.

  Order inside one request: fence on the version she had on screen → apply the measurement through
  the seam → re-read the item's row_version (it moved through OUR relabel, not a competing
  verifier's) → record the verdict. Concurrency is still fenced, because the verdict UPDATE also
  requires the item to be `pending`, so a verdict that landed in between is still a 409. A producer
  that refuses the number stops the whole approve rather than leaving an approved item beside a
  value that never landed. The measurement's idempotency key is the verdict's suffixed
  `:measurement`, so a replayed approve re-applies the same reading.

  `MeasurementCorrectionSpec.RequiredForApprove` is TRUE for wastage and FALSE for weighing, and it
  is what holds Approve until a number exists — checked BEFORE the verdict, not after. An item
  measured earlier through the standalone route (an installed APK still showing its own save
  button) is still approvable: the applier is asked whether a value is already recorded. Both
  producer routes stay served for exactly that reason; no current client calls them.

  A REJECT never carries the number. Rejection sends the work back to be recorded again, so writing
  a value onto a record about to be redone would store a number nobody will use.

  **Proof:** `backend/internal/verification/app/verdict_measurement_test.go` — the save-then-approve
  409 reproduced as the defect being replaced; one-act approve applies value + verdict and targets
  the ITEM's own source; blank approve leaves the operator's weight alone; reject drops the value
  before the verdict is stored; wastage refuses a number-less approve and accepts a recorded ZERO;
  an already-measured pen still approves; a producer refusal stops the approve; a stale screen is
  refused before anything is written. Each was mutation-tested when written.

Lifecycle, idempotency, one-video-per-unit conflict (`409`), rework/re-submit, outbox event
(`feed.wastage.completed`, emitted ONLY at approval, carrying `wastage_kg` only when recorded),
verdict consumer (`FeedWastageVerificationHandler`, registered in `eventwiring` and both bus
builders), audit rows, and the serving index all mirror the packing gate exactly.

Permission: `feed_wastage.read` gates the worklist and the phone's fourth Feed tab
(`/feed/wastage`); the completion write reuses `feed_direction.complete`.

**Proof:** `backend/internal/feeddirection/adapters/postgres/feed_wastage_verification_integration_test.go`
— pending-verification write with stamped experiment workflow; approval → `completed` +
`feed.wastage.completed` + idempotent replay; rejection → `rework` + re-submit bumps row_version;
second differing video refused; measurement stores/replaces/replays, zero accepted, out-of-range
and unknown-completion refused; pens kept apart. Registered in the domain-event registry as the
`feed.wastage.completed` producer plus feeddirection consumers under the verdict events.

## Packing review is BLIND PER-ITEM ENTRY — 2026-08-21, SUPERSEDING the visible "Expected ration" context row

Maintainer decision 2026-08-21. Feed packing verification changes shape: the verifier no longer
judges the video against a printed expected ration. Her item now shows, below the video on BOTH
surfaces (admin-web `/verify` drawer and the Android Verify detail), **one numeric entry box per
feed item of that pen-session — names only**. She watches the clip, types the packed weight she can
see for each item, and presses **Accept once**; the approve carries every reading (the 2026-08-20
"THE APPROVE CARRIES THE NUMBER" rule, extended from one value to one value per field). A verifier
who cannot see a usable video **rejects**, which sends the bag to rework exactly as before.

The load-bearing choices:

- **BLIND ENTRY.** The planned quantities are deliberately absent from the verifier's item — the
  "Expected ration" / "Animals in this pen" context rows the item used to carry are gone, because a
  verifier who can see the sheet can copy it, and a copied number confirms nothing. The
  intended-vs-entered comparison is computed by the producing module and surfaces ONLY on the
  leadership Feed Analytics execution section (`/feed/analytics`), which the verifier lens can never
  open. Do not put the planned figures, or the variance, on any verifier surface.
- **The field list rides the ITEM.** New generic column `verification_items.measurement_fields`
  (migration `000181`): an ordered `[{key,label}]` the producer composes at enqueue from the FROZEN
  issued sheet — `key` is the normalized feed item key (`NormalizeConfigKey`, the same key the sheet
  rows carry), `label` the display caption. Composed at enqueue and stored, like `context_rows`, so
  re-authoring the config cannot change which boxes an already-submitted bag is judged with. Served
  to clients inside `measurement_correction.fields`; the verdict's measurement carries `entries`
  echoing each key. **Only items the sheet DIRECTS for the bag become boxes** (maintainer decision
  2026-08-22, superseding the initial every-item-blocked-included composition): the frozen grid
  mentions every feed item the pen's ration rows carry, zero-quantity cells included, and boxes for
  those forced the verifier to type 0 for items the shed is never fed. `packingEntryFields` keeps
  only resolved, positive-quantity items (sheet order preserved); an all-zero/blocked bag yields no
  fields and falls into the same judge-the-video exemption as an unreadable sheet.
- **Every box must be filled to Accept.** `MeasurementCorrectionSpec` on category `feed_packing`:
  `RequiredForApprove: true`, `PerItemFields: true`. Verification enforces completeness against the
  item's OWN fields before the verdict (422 `measurement_required` naming the missing box), refuses
  unknown keys, and — the one deliberate exemption — lets a FIELDS-LESS packing item approve as a
  plain judge-the-video item, because the enqueue composes fields fail-open (an unreadable sheet
  must never fail the operator's submit, and a required control with no boxes would strand the item
  unapprovable). ZERO IS A VALID ENTRY ("this item was not packed"); blank is not entered.
- **The producer owns the readings.** New table `feed_packing_verified_quantities` (migration
  `000182`), PK `(tenant_id, completion_id, feed_item_key)`, written only through
  `RecordPackingVerifiedQuantities` via the registered `PackingMeasurementApplier` — BEFORE the
  verdict, so a refusal (unknown completion, out-of-range weight; ceiling 10000 kg like wastage)
  stops the whole approve. REPLACE semantics per completion: a rework re-submit's fresh approve
  overwrites the previous reading set, and keys it no longer names are removed.
- **A REJECT never carries the readings** — same as the 2026-08-20 rule: rejection sends the bag
  back to be packed and filmed again.
- **Variance pops beyond a 0.2 kg tolerance** (maintainer decision 2026-08-21, second same-day
  decision SUPERSEDING the initial any-mismatch rule stated that morning): a reading taken off a
  video is honest to a couple hundred grams, so a difference of 0.2 kg or less is treated as the
  same number and stays off the execution view; strictly more than 0.2 kg pops. The threshold is
  `feeddirection/domain.PackingVarianceToleranceKg` — one constant, bound into the SQL as a
  parameter, never re-hardcoded. The execution analytics read joins the
  readings to the frozen sheet on the completion's own natural-key coordinates plus
  `feed_item_key`, pre-aggregating the ration-grain side (the same summation `BuildPackingRows`
  does for the packer's worklist) before comparing. Only `status='completed'` rows count — a
  pending or reworked completion's readings are not yet a finding. A planned quantity the sheet
  never resolved renders blank, never zero.

Pinned by `verdict_measurement_entries_test.go` (completeness, unknown key, fields-less exemption,
reject-drops-entries, entries-refused-on-single-value-items),
`TestMeasurementFieldsRoundTripThroughBothReadPaths_RealPostgres`,
`TestPackingVerifiedQuantitiesUpsertAndVariance`, and
`TestPackingVarianceOneToManyParkScopeStatusMatrixPageBoundary` (grain, scope, status and window
adversarial proofs), plus the packing enqueue tests asserting the item carries entry boxes and
NEVER the planned quantities.
