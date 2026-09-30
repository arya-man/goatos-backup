# PC Care repeat: "repeat every N days" on the SOP card (maintainer instruction 2026-09-30)

Status: accepted · Owner: pccare + pccaresop + kernelstages + notificationbridge + admin-web

## What was asked

"In web SOP make it configurable in terms of repetition: for fumigation, if I keep a repeat value
of 30 days, this task should be created automatically after 30 days, for the same operator who did
it last time."

Choices made with the maintainer the same day: count from the last task's **planned date**;
create the next one **even if the last one is not done**; the field is on **every** work card; if
the same operator can no longer do it, **skip that pen and alert the planner** -- nobody is
substituted.

## Decision

| Rule | Answer |
| --- | --- |
| Where it is set | `form_dsl.pc_care.categories.<work>.repeat_every_days` on `/pc-care/sops` ("Repeat every (days)"), 1..365, blank = no repeat. The seed repeats nothing, so deploying changes nothing until someone sets it. |
| Which version decides | The PUBLISHED card at the moment the next task is made -- repeating is a planning act. Changing 30 to 45, or clearing it, governs the next task. |
| What is created | For each pen, the LATEST live task of that work (any status but canceled): the next task is planned for `planned + N`, for the same pen and the same operators, as one round per source round. It pins the published SOP version like any planned task, is verifier-reviewed and (for the pen-visit categories) owes the next-day visit. |
| When | `pc-care-repeat` kernel stage, operational lane, `RepeatLeadDays` (2) ahead of the date so the operator sees it and a deworming removal crew still has its evening. A worker that was down past the date plans for today, never a past day. |
| Not done yet | Created anyway; the old task stays as overdue work. |
| Feed & water removal | Follows the card: `required` rides every repeat of a listed work; `optional` repeats what the last planner chose; the removal crew is the last removal crew. |
| Operators | Of the last task's operators, those still an ACTIVE workforce member with an ACTIVE grant for the park. None left -> the pen is skipped (`pc_care_repeat_skips`, reason `no_operator_available`) and the person who planned the last task is pushed ONCE, naming the work, pens, park and date. A removal whose evening has passed is skipped the same way (`removal_window_closed`). |
| Owner | The repeat's `created_by` is the last task's planner, so they can end it and are the one alerted. |
| Once only | `pc_care_tasks.repeat_of_task_id` with a partial unique index: a task is repeated at most once, racing ticks cannot both insert. A pen planned by hand for that day wins (the stage logs a conflict and does nothing). |

## Storage

Migration `000458_pc_care_repeat.sql`: `pc_care_tasks.repeat_of_task_id` + unique index, and the
`pc_care_repeat_skips` ledger. Both additive.

## Pinned by

`TestRepeatEveryDaysIsAuthoredPerCardAndBounded` (domain), `TestFumigationRepeatsForTheSamePenAndOperators`
and `TestNoRepeatWhenTheCardSaysNone` (real Postgres: too early / on time / idempotent / chain
continues from the repeat / an operator who left drops off / nobody left = skip + ONE alert /
cleared = nothing; the one-alert rule was mutation-tested), and admin-web `pc-care-model.test.mjs`.
