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
| Stopping | Close the open pen. A closed pen is never submitted, so nothing follows it. |
| Owner | The next pen's `created_by` is the last pen's planner. |
| Once only | The new pen task carries `repeat_of_task_id` = the last pen's task; the existing partial unique index (`000460`) makes "planned once" a database fact. A pen planned by hand for the same day wins. |
| Everything else | A rotated pen is an ordinary `pc_care_tasks` row: pinned to the published SOP version, two videos per pen, verifier-reviewed, owes the next-day pen visit. |

## Storage

No migration. Rotation reuses `repeat_of_task_id`, its unique index and `pc_care_repeat_skips`
from `000459`-`000461`. It runs in the existing `pc-care-repeat` kernel stage, straight after the
interval pass.

## Pinned by

`TestRotationIsAuthoredPerCardAndExclusiveWithTheInterval` (domain),
`TestFumigationRotatesOnePenADayThroughOccupiedPens` and
`TestRotationStopsAndAlertsOnceWhenNoOperatorIsLeft` (real Postgres: waits for the submit, natural
order, empty pen skipped, a late submit moves the next pen, once only, wrap waits the gap, a closed
pen stops it, nobody left = ONE alert). Mutation-tested: dropping the "every pen submitted"
condition turns the first red. Admin-web `pc-care-model.test.mjs` covers the editor round trip.
