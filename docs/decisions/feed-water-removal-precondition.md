# Feed & Water Removal Precondition (Weighing + Deworming)

Maintainer decision 2026-09-03.

## The rule

Animals must have feed and water removed the **evening before** they are
weighed, and the evening before a **tablet-form (in-feed) deworming** — or the
work is wrong (wrong weights; tablets not eaten). The app now owns that
precondition as a first-class task:

1. A weighing task / a feed-removal deworming for day **D** must be created
   **strictly before 20:00 IST on D−1**. At or after 20:00, D is no longer
   offered or accepted as a date (the earliest becomes D+1). For weighing this
   applies to **every** task — weighing can no longer be planned for "today".
   For deworming it applies **only when feed removal is required**; injection
   deworming is untouched.
2. At creation a **second operator** (same park) is assigned for the removal.
   The evening-shift person and the task's own operator are different people,
   which is why this is its own assignment.
3. The removal cards are **served from 20:00 IST on D−1** (server-side clock;
   no client derives the window). **ONE CARD PER SHED** (maintainer correction
   #2, same day, superseding the umbrella-card-with-per-shed-slots second cut,
   which had itself superseded the one-pair-per-card first cut): the operator's
   LIST holds a separate card for each shed of the round — "Remove feed &
   water · Castro 1" — never one card the sheds hide inside. Each card owes its
   own live-camera feed-removal video and water-removal video (one clip
   stretched over several sheds proves nothing) and is SUBMITTED ON ITS OWN.
   The round's deadline is unchanged — **00:00 IST of D** — and is satisfied
   only when EVERY shed's card is submitted: the LAST shed's submit stamps the
   round's `submitted_at` inside the same transaction, and that stamp is the
   only fact the midnight gate reads. A clip reused anywhere in the round —
   the card's other slot or a sibling shed's card — refuses the submit.
   Deworming's removal is a single pen, so it is one card by construction.
4. **Operator SUBMISSION is the gate.** Submission of every card before
   midnight lets day-D work run. The videos go to the **verifier post-hoc**,
   and review follows the EVIDENCE (ledger B-5): **one item per shed**,
   carrying that shed's two clips and NAMING the shed ("Remove feed & water ·
   Godel 1"), shed-routed for her filters. A rejection sends back THAT shed's
   card alone — it returns to rework with her reason verbatim, the sibling
   cards keep their state, and the resubmit demands fresh clips for that shed
   (either rejected clip, in either slot, is refused by name). A submitted or
   approved card refuses a second submit outright, so a duplicate review round
   cannot be minted. The round completes only when EVERY shed is approved.
   None of this **ever un-runs** work that already happened: the kernel gates
   read `submitted_at`, never review state.
5. Unsubmitted at midnight → the kernel rolls the **whole cycle forward one
   day**: the weighing work items / the deworming task move to D+1 (never
   "today"), and the removal card re-arms for the next evening's 20:00 window.
   This repeats daily until the removal is actually submitted.
6. Weighing and deworming removal cards are **separate cards, each in its own
   module** — even for the same shed on the same night (maintainer's words:
   "cards are separate, each in each category").

## Where it lives

**Weighing** (isolation preserved — new weighing-owned tables, no cross-module
reads): `weighing_fasting_tasks` (migration 000253, the ROUND: one park, one weigh
night, one removal operator, the dates and the midnight stamp) + the per-shed
evidence table `weighing_fasting_shed_proofs` (migration 000255, UNIQUE per
(task, bucket) so the verdict join is provably 1:1). The operator surface is
ONE CARD PER SHED over that pair. Product rule + the three clocks:
`backend/internal/weighing/domain/fasting.go`. Create/cutoff:
`weighingapp.Service.CreateCampaign` / `validateCreate` /
`UpdateCampaign` (a date cannot move once the removal was submitted —
`fasting_date_locked`). Card list + per-shed submit:
`GET /app/weighing/fasting` (per-shed cards),
`POST /app/weighing/fasting/{id}/sheds/{campaign_shed_id}/submit`
(both `weighing.execute`; submit further requires being the assigned
operator). Midnight gate: `sweepFastingGate` in
`weighing/adapters/postgres/kernel_fasting.go`, a pass of the existing
5-minute kernel sweep. Guard: `weighing_fasting_tasks` and the fasting write
functions are allowlisted BY NAME in `check-weighing-free-flow-guard.mjs` —
the table knows a campaign, a park, an operator and two proof refs, and must
never learn an animal.

**Deworming (PC Care)**: a new category `feed_water_removal` (capture mode
`task_proof`, backend-owned slots `feed_video` + `water_video`), created in the
**same transaction** as the deworming when `feed_removal_required` is true,
linked via `pc_care_tasks.gates_task_id` (migration 000254; partial unique
index makes the gate join provably 1:1). It rides the existing PC Care
machinery end to end: assignees, worklist (served **inside the Deworming tab**
— the four-tab bar lock stands), task-level proofs, submit, verification,
roll-forward. Midnight gate: `sweepDewormingRemovalGate` in
`pccare/adapters/postgres/kernel.go`. Canceling a deworming cancels its
unsubmitted removal (a submitted one is history).

## Deliberate narrowings

- **Two videos means two clips.** The same proof ref for both slots is refused
  (`fasting_proof_invalid`) — one clip cannot prove two removals. Both proofs
  must be completed, in-app-camera videos.
- **A rolled weigh/deworm date does NOT re-demand a fast when the removal WAS
  submitted.** If the removal was done but the weighing itself slips a day
  (operator didn't weigh), the next day's weighing runs on the previous
  evening's fast — the same trade the kernel's existing carry-forward takes.
  Re-arming the fast on every slip was considered and deferred; changing this
  is a maintainer decision.
- **No new outbox event types.** The fasting writes record audit rows and
  idempotency snapshots; verifier routing rides the verification module's own
  events. `weighing.fasting_submitted` / `weighing.fasting_verdict_applied`
  are audit actions and idempotency scopes, deliberately not bus events —
  nothing consumes a fasting bus event today, and a producer with no consumer
  is a silent drop.
- **Rollout-safe:** campaigns and dewormings created before this feature have
  no removal row and behave exactly as before; the gates key on the row's
  existence.

## Pinned by

Weighing: `TestEarliestPlannableWeighDateFlipsAtEightPMIST`,
`TestCreateCampaignEnforcesTheFastingEveningCutoff`,
`TestCreateCampaignRequiresTheFastingOperator`,
`TestUpdateCampaignFastingDateRules`,
`TestSubmitFastingShedRequiresBothVideosAndEnqueuesThatShed`,
`TestFastingListVisibilityOpensAtEightPMIST` (pg, one card per shed),
`TestSubmitFastingShedWritesOnceAndStampsTheRoundOnTheLastShed` (pg),
`TestReworkResubmitRefusesTheRejectedClipAndAcceptsFreshOnes` (pg),
`TestMidnightGateRollsUnlessEveryShedWasSubmitted` (pg, including the
half-submitted round still rolling),
`TestApplyFastingVerdictNeverUnsubmits` (pg).
PC Care: `TestFeedWaterRemovalCategoryContract`,
`TestEarliestFeedRemovalDewormingDateCrossesTheEveningCutoff`,
`TestCreateTaskFeedRemovalRequiresRemovalOperators`,
`TestCreateTaskFeedRemovalOnNonDewormingIsRejected`,
`TestDewormingRemovalGateSweepShape`, and the pg integration suite in
`pccare/adapters/postgres/fasting_precondition_integration_test.go`.
Key mutations were run red→green when written (cutoff branch deleted, gate
invocation removed, `submitted_at IS NULL` dropped, visibility predicate
deleted, D−1 changed to D).
