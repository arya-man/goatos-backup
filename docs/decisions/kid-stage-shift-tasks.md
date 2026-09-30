# Kid stage shift tasks (maintainer decision 2026-09-30)

Status: implemented on `feat/kid-stage-shift-tasks` · Owner: tasks + counts + sop + admin-web + Android

## Decision

Every litter owes two moves, raised by the system on its own as a task:

1. **K0 → K1, exactly 24 hours after the kids were born.**
2. **K1 → K2, exactly 7 days after the litter reached K1.**

The rule is **authored data, not code**. The maintainer said *"once i set rule, that's all it should
work for all born animals"* and *"in future if i need anything more these things should be
configurable"*. The rule lives on the **Birth SOP** (`counts.birth`, edited on `/counts/sops`) as
the `birth_litter` follow-up track. Timing, owner and target stage are authored per step. A later
"K2 → K3 after N days" is a new step on the web, not a release.

The maintainer answered three questions on 2026-09-30:

| Question | Answer |
|---|---|
| How is the task finished? | **By the real shift.** Tapping the task only navigates to Raise shifting (growth, kids selected). Whoever raises fills in the rest, the approver approves, the operator completes, and the task closes itself when the kids are on the stage. The K2 clock counts from that real move. |
| One task per kid or grouped? | **One per birth (litter).** |
| Who sees it? | **Anyone in herd operations** (no owner on the seeded steps; a farm may still author one per step). The park head / approver only approves the shifting. |
| Which kids? | **Also the animals already on the farm** (reversing a first "births from now on" answer the same day): every recorded litter with a live kid still on K0 or K1 gets its task through `cmd/backfill-kid-shift-tasks`. |

## How it works

- **One workflow per litter.**
  - Template key `birth_litter`, keyed on the birth event (`subject_ref_id = goat_births.birth_event_id`), with no single animal.
  - It opens beside the kid and mother tracks on the birth's `goat.created`. Twins land on the same workflow.
  - Its clock is the birth moment, not the moment the event was processed.
- **The steps.** Each uses task type `shift_kids_stage` (engine hook `shift_kids_stage`, migration `000462`), no owner, and a `target_stage`:
  - `shift_to_k1` is due 1440 minutes after the event and targets `K1`.
  - `shift_to_k2` is due 10080 minutes after `shift_to_k1` completes, requires it, and targets `K2`.
- **Engine-completed, never a tap.**
  - `tasks/app.LitterShiftWorkflowHandler` consumes `goat.stage_changed` and `goat.exited`. It re-reads the litter from the herd register.
  - `domain.ApplyLitterShift` completes a pending step once its prerequisites are complete and **no live kid is left on a stage before the target**. At least one live kid must have reached the target.
  - The step completes at the moment the LAST kid entered the stage, read from the herd register's own history (`goat_identity_events`, the latest `goat.stage_changed` naming the kid's current stage). The next step's due time is stamped from that instant, so a litter judged late (a delayed event, or the backfill) still gets its K2 task seven days after it really reached K1. A kid with no recorded entry (seeded on its stage) falls back to the moment of judging.
  - A by-hand completion is refused with **409 `kid_shift_pending`**.
- **One ladder.** "Before K1" / "past K1" come from `counts/domain.GrowthStagesBefore`, which walks the same `growthForwardEdges` the growth raise obeys. The task and the raise cannot disagree. Publish refuses a `target_stage` no growth shifting reaches (`K0`, `ICU`). The editor offers exactly `counts/domain.GrowthTargetStages()`.
- **Kids who leave or fall sick.**
  - A dead or sold kid no longer holds the move open.
  - A litter with no live kid is canceled.
  - A rejected birth cancels the litter workflow with its kid and mother tracks.
  - A kid in an off-ladder pen tag (`ICU-Kid`) counts as neither waiting nor reached. A litter whose only live kid is in ICU stays owed.

## Where it is seen

- **Phone → Work Board.** A Counts-lane workflow row's **Open** goes to its step screen, resolved from the row's own key (`counts|workflow|<id>`). The board `href` is shared with the web console, which has no step screen, so engine rows carry none.
- **The step** lists the kids still waiting ("A1024 · K0 · Castro 1") and a **Raise shifting for these kids** button. The button opens Raise shifting with Growth chosen and those kids selected. They are resolved through the same tag lookup and eligibility checks a scanned tag uses.
- **Shifting itself is unchanged.** Park Head approval comes first, then the completion video and the atomic apply. The step completes when the apply moves the kids' stage.

## Authoring on the web

On `/counts/sops` → Birth → **Litter** track, a `shift_kids_stage` step shows a **Move the kids to** select. Its choices come from `sop_shift_target_stages`. Which task types carry a hook comes from `sop_task_type_hooks`. Both are compiled by the backend.

Publish refuses:

- a shift step on any track other than `birth_litter`;
- a shift step with a missing or unreachable target stage;
- a target stage on any other step.

## Deploy-day behaviour

Migration `000462`:

- adds `birth_litter` to the template-key check;
- adds the nullable column `workflow_actions.target_stage`;
- inserts the task type for every tenant;
- appends the litter track **in place** to each tenant's published `counts.birth` version that lacks it. This follows the `000443` shape.

A tenant on the seeded document gets the same track from `sopseed.FollowUpTrackAddenda`. The base `counts_birth.json` stays byte-identical to `000308`. Workflows already open are untouched. A Birth SOP published *without* the litter track opens no litter workflow.

## Not done (recorded)

- **No push when a step falls due.** Nothing in the engine pushes on a workflow step's due time; colostrum rounds do not either. A due-time push is a new notification pipeline: an audience catalog row, specificity copy and FCM routing. That needs its own maintainer decision.
- **The backfill is a one-shot command**, not a sweep: after deploy, run `backfill-kid-shift-tasks -tenant-id <id>` (dry-run) and then with `-apply`. It opens ONLY the litter workflow (never the old kid/mother tracks), anchored on the kid's recorded birth, and is idempotent. Kids with no `goat_births` litter row (bought animals, animals imported without a recorded birth) have no litter to key on and get no task.
- **Future rule, recorded:** "farm-born animals → breeding tag after 10 weeks". Breeding is a shift TYPE that keeps the tag, not a growth stage, so that step needs `target_stage` extended to name a shift type; the timing and the per-litter workflow already fit.
