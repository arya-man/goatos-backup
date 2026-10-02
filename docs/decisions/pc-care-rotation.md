# PC Care rotation: "rotate through the pens" on the SOP card (maintainer instruction 2026-10-02)

Status: accepted · Owner: pccare + kernelstages + notificationbridge + admin-web

## What was asked

"It should go as a round robin: one shed after one shed, and if all are completed then repeat
again." Asked about fumigation, two days after the per-pen interval repeat
(`docs/decisions/pc-care-repeat.md`) shipped.

Choices the maintainer made the same day:

1. A pen counts as done when the **operator SUBMITS** it (waiting for the verifier counts).
2. Only **pens with animals in them** are in the rotation.
3. The **park's normal pen order**.
4. **One pen per day.**
5. **Keep both**: the interval repeat stays; rotation is a second choice on the card.

## Decision

| Rule | Answer |
| --- | --- |
| Where it is set | `form_dsl.pc_care.categories.<work>.repeat_mode = "rotation"`, with an optional `rotation_gap_days` (0..365, the wait after the last pen before the first pen of the next round). On `/pc-care/sops` the card's **Repeat** picker reads *No repeat / Every N days, same pens / Rotate through the pens*. The seed rotates nothing. |
| Exclusive | A card either repeats every N days or rotates, never both (refused at save). A rotation is refused on work the feed & water removal applies to: the next pen is planned for the next day and its removal evening would already be gone. |
| Which version decides | The PUBLISHED card when the next pen is planned (a planning act), exactly like the interval repeat. |
| Starting | A planner plans the FIRST pen by hand (any pen, any operators). From then on the kernel carries it. |
| Next pen | When EVERY pen of the park's latest task of that work is submitted (`submitted_at` set -- a verifier rework later does not pull it back), the next pen with a live animal in it after the last pen, in the park's natural pen order (`Part 2` before `Part 10`). After the last pen it wraps to the first. The pen set and order are the Care Coverage board's own (`penCoverageScopedPensSQL`), so the two screens agree. A pen that empties drops out; a pen that fills up joins at its place. |
| Date | One pen per day: the day after the later of the last submit and the last planned date, plus the gap on a wrap; never before today. Made up to `RepeatLeadDays` (2) ahead. |
| Operators | The last pen's operators who are still an ACTIVE member with an ACTIVE grant for the park. None left -> the rotation STOPS (`pc_care_repeat_skips`, `no_operator_available`) and the planner of the last pen is pushed ONCE ("Fumigation rotation stopped · <pen>"). Nobody is substituted. Planning that pen by hand carries the rotation on from there. |
| Stopping | Close the open pen. A closed pen is never submitted, so nothing follows it, and it is never treated as "the latest" pen: the pen before it already has the closed pen as its follow-up, so nothing more is planned from it either. |
| Restarting | Plan any pen by hand (with operators who can still do it). That pen becomes the latest -- even when it is dated EARLIER than the closed one -- and the rotation carries on from it. |
| A pen planned by hand mid-rotation | The latest pen by planned date leads. A pen hand-planned for a LATER date than the rotation's open pen becomes the latest, so the rotation continues from it once it is submitted; one planned for an earlier date leaves the rotation where it is. |
| Verifier rework | Does not pull a pen back: "done" is the operator's submit. A rejected pen is re-shot by the operator; the next pen already planned stays, and no second one is made. |
| Owner | The next pen's `created_by` is the last pen's planner. |
| Once only | The new pen task carries `repeat_of_task_id` = the last pen's task; the existing partial unique index (`000460`) makes "planned once" a database fact. A pen planned by hand for the same day wins. |
| Everything else | A rotated pen is an ordinary `pc_care_tasks` row: pinned to the published SOP version, two videos per pen, verifier-reviewed, owes the next-day pen visit. |

## Storage

No migration. Rotation reuses `repeat_of_task_id`, its unique index and `pc_care_repeat_skips`
from `000459`-`000461`. It runs in the existing `pc-care-repeat` kernel stage, straight after the
interval pass.

## Deploy-day behaviour

Nothing changes on deploy. There is no migration, the seeded and the live published `pc_care.tasks`
documents carry no `repeat_mode`, and the stage plans nothing for a card that does not rotate.
Checked 2026-10-02 against the published STG document (v1, read-only): it parses, validates and
every category reads "no interval, no rotation".

## Pinned by

`TestRotationIsAuthoredPerCardAndExclusiveWithTheInterval` (domain),
`TestFumigationRotatesOnePenADayThroughOccupiedPens` and
`TestRotationStopsAndAlertsOnceWhenNoOperatorIsLeft`, `TestRotationRestartsFromAHandPlannedPenAfterAClose` (real Postgres: waits for the submit, natural
order, empty pen skipped, a late submit moves the next pen, once only, wrap waits the gap, a closed
pen stops it, nobody left = ONE alert). Mutation-tested: dropping the "every pen submitted"
condition turns the first red. Admin-web `pc-care-model.test.mjs` covers the editor round trip.

End to end: `tests/e2e/story_pc_care_rotation_test.go` (`TestKernelStory_PCCareRotationRoundRobin`)
drives the real SOP publish (with the API's PC Care contract), planner, operator proof uploads and
submit, verifier verdict, identity herd move and the REAL `pc-care-repeat` kernel stage across
fifteen steps -- from "the shipped SOP plans nothing" through every edge above. Mutation-tested:
switching the rotation pass off turns it red.
