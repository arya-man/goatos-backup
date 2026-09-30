# Kid stage shift tasks (maintainer decision 2026-09-30)

Status: implemented on `feat/kid-stage-shift-tasks` · Owner: tasks + counts + sop + admin-web + Android

## Decision

Every litter owes the **park head** three moves, raised by the system as a task **only when a move is actually owed**:

1. **K0 → K1, exactly 24 hours after the kids were born.**
2. **K1 → K2, exactly 7 days after the litter reached K1.**
3. **Its FEMALE kids → Non-Pregnant, 70 days (10 weeks) from the day of birth, whatever stage they
   are on** (maintainer instruction 2026-10-01; see "The Non-Pregnant step" below).

The rule is **authored data, not code**. The maintainer said *"once i set rule, that's all it should
work for all born animals"* and *"in future if i need anything more these things should be
configurable"*. The rule lives on the **Birth SOP** (`counts.birth`, edited on `/counts/sops`) as
the `birth_litter` follow-up track. Timing, owner and target stage are authored per step. A later
"K2 → K3 after N days" is a new step on the web, not a release.

The maintainer answered three questions on 2026-09-30:

| Question | Answer |
|---|---|
| How is the task finished? | **By the real shift.** The park head tells the health managers; they raise the growth shifting (the task's button opens Raise shifting with the kids selected), today's approvers approve it, and they complete the move. The task closes itself when the kids are on the stage. The K2 clock counts from that real move. |
| One task per kid or grouped? | **One per birth (litter).** |
| Who sees it? | **The park head only** (`owner: park_head` on both steps). Operators have no task access: the litter is never on an operator's own Work Board lens nor on the Birth list. Who approves the shifting is unchanged (today's counts approvers). |
| Kids moved before the deadline? | **No task at all.** A litter surfaces only once a step is past its deadline and the kids are still waiting. |
| Which kids? | **Also the animals already on the farm** (reversing a first "births from now on" answer the same day): every recorded litter with a live kid still on K0 or K1 gets its task through `cmd/backfill-kid-shift-tasks`. |

## How it works

- **One workflow per litter.**
  - Template key `birth_litter`, keyed on the birth event (`subject_ref_id = goat_births.birth_event_id`), with no single animal.
  - It opens beside the kid and mother tracks on the birth's `goat.created`. Twins land on the same workflow.
  - Its clock is the birth moment, not the moment the event was processed.
- **The steps.** Each uses task type `shift_kids_stage` (engine hook `shift_kids_stage`, migration `000462`), `owner: park_head` and a `target_stage`:
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

## The Non-Pregnant step (maintainer instruction 2026-10-01)

"After 10 weeks shift farm born female to non-pregnant, this is one more task to park head." The
maintainer answered: from the **day of birth**; **whatever her current stage**; owed **only if she
is before Non-Pregnant**.

- **The step.** `shift_to_non_pregnant`, `after_event` 100800 minutes (70 days), `owner: park_head`,
  `target_stage: Non-Pregnant`, and the new **`target_sex: female`** (`workflow_actions.target_sex`,
  migration `000463`). A step's `target_sex` makes it judge only that sex's kids: waiting kids,
  raise groups and completion all read the litter's female kids alone. Publish refuses
  `target_sex` on any step that is not a kid shift step, and any value but `female` / `male`. The
  web editor shows it as **Only for** (Every kid / Female kids / Male kids).
- **Nothing owed.** A litter with no live female kid SKIPS the step on its first judge (an all-male
  litter is never asked). A female already past Non-Pregnant (Pregnant, Mother) reads as reached:
  "reached" is now the target, anything past it, or any other rung on the ladder that is not before
  it. A kid off the ladder (ICU-Kid) is still neither waiting nor reached.
- **The raise is allowed.** The growth ladder had no K3 → Non-Pregnant edge, so the raise the task
  opens would have been refused. Growth gained an **age-entry rule** (shifting rulebook doc →
  "a female of age enters Non-Pregnant from any earlier rung"): a female whose age is at least
  Non-Pregnant's **From (days)** may enter Non-Pregnant from any earlier rung. Migration `000463`
  sets that From (days) to 70 where the farm has none, so the task and the raise agree. **They are
  two settings**: changing the step's 70 days on the Birth SOP does not change the stage's From
  (days) on Items & settings, and the raise follows the latter.
- **Animals already on the farm.** `backfill-kid-shift-tasks` now also opens the litter for any
  birth with a live FEMALE kid on a stage before Non-Pregnant (`-female-target-stage`, default
  `Non-Pregnant`). Opening an old litter completes K1/K2 at once (the kids are past them) and leaves
  the Non-Pregnant step owed from 70 days after birth.
- **Proven on the phone (2026-10-01)** against the throwaway DB: a 72-day K3 female and her K3
  brother were backfilled; the park head's board read "Shift the female kids to Non-Pregnant · 2 of 3
  steps done · Overdue"; the step listed only the female with "Raise shifting · 1 female kid"; the
  raise into a Non-Pregnant pen was accepted, approved and completed; she became Non-Pregnant, her
  brother stayed K3, and the step and litter closed on their own (3 / 3 done). A 2-day-old female
  raised into the same pen was refused `growth_not_next_stage`.

## Where it is seen

- **Only when owed.** The board shows a litter's row only while a shift step is past its due instant and still pending, or on the day such an owed step got done (`boardsource.litterSurfacedSQL`, applied to the list AND the count). A litter whose kids were shifted before the deadline, or one between its K1 move and its K2 deadline, is not on the board. The operator lens (`OwnerUserID` set) never includes it, and the Birth list (`ListWorkflows`: chips, overdue dates, page) excludes `birth_litter` entirely.
- **Phone → Work Board.** A Counts-lane workflow row's **Open** goes to its step screen, resolved from the row's own key (`counts|workflow|<id>`). The board `href` is shared with the web console, which has no step screen, so engine rows carry none.
- **The step** lists the kids still waiting ("A1024 · K0 · Castro 1") and a **Raise shifting for these kids** button. The button opens Raise shifting with Growth chosen and those kids selected. They are resolved through the same tag lookup and eligibility checks a scanned tag uses.
- **Shifting itself is unchanged.** Park Head approval comes first, then the completion video and the atomic apply. The step completes when the apply moves the kids' stage.

## One raise per group the server accepts (maintainer decision 2026-09-30, after the phone E2E)

A shifting's count impact is keyed on (destination pen, breed), and the animals on one key must
agree on stage, kid/adult band and sex -- counts refuses a mixed set with `missing_impacts` rather
than invent one stage or sex for it. Mixed-sex twins could therefore never be raised in ONE
shifting (true on main before this feature, and from any screen). The maintainer chose to fix it in
the task, not in counts: the step serves `shift_groups` -- its waiting kids split by breed, sex,
stage and band (`domain.ShiftGroups`) -- and the phone shows one **"Raise shifting · 1 female kid"**
button per group. Mixed-sex twins are two taps; nothing about how counts works changed.

Waiting kids and groups are served only on a step whose earlier steps are done: a K2 step behind an
unfinished K1 step lists no kids and reads "Finish the earlier steps first." Once K1 is done the K2
step does offer its buttons before its deadline -- moving early is allowed and simply completes it.

**The raise result shows on the task.** The raise form queues the write and closes; the litter
screen follows that outbox item and shows "Shifting sent for approval…" or the server's refusal
verbatim, in red. Before this, a refused raise was silent to the park head, who has no Shifting list.

**Untagged newborns can be found.** `/goats/search` sends `"animal_identifier_1": null` for a kid
still on its birth tag, and the phone's `GoatSearchItemDto` declared it non-null, so the WHOLE page
failed to decode and the kid could not be selected at all (a bug on main, from any screen). The field
now decodes null as blank (`NullAsEmptyStringSerializer`), pinned by `GoatSearchDtoDecodeTest`
(mutation-tested). The litter read also shows a kid's birth tag (CBE-38085) before its internal id.

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

- **Duplicate raises are possible.** The server accepts a second growth shifting for kids that
  already have one pending (a shifting does not check other open shiftings of the same animals).
  Pre-existing shifting behaviour; the task cannot see a shifting's animals without reading counts'
  approval payloads, so it is left to a shifting-side rule.
- **The board row reads "Open to the team"** for a park-head task: a board row's owner can only be a
  person, so naming a designation needs a board contract change.

- **No push when a step falls due.** Nothing in the engine pushes on a workflow step's due time; colostrum rounds do not either. A due-time push is a new notification pipeline: an audience catalog row, specificity copy and FCM routing. That needs its own maintainer decision.
- **The backfill is a one-shot command**, not a sweep: after deploy, run `backfill-kid-shift-tasks -tenant-id <id>` (dry-run) and then with `-apply`. It opens ONLY the litter workflow (never the old kid/mother tracks), anchored on the kid's recorded birth, and is idempotent. Kids with no `goat_births` litter row (bought animals, animals imported without a recorded birth) have no litter to key on and get no task.
- **"Farm-born" is read as "born on the farm with a recorded litter".** The litter is keyed on
  `goat_births`, so a bought female never gets the Non-Pregnant task.
