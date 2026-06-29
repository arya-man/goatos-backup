# CEO Guide - Goat OS Vaccination Dev Walkthrough

Status: reviewer walkthrough for the vaccination slice. This guide is for the
website experience after login. Internal engineering follow-ups are tracked
separately in `context/execution/vaccination-workflow-followups.md`.

Audience: CEO, COO, and internal Mesha reviewers.

Purpose: explain what to open, what to look at, what to test, and what is not
finished yet.

## Login

Open:

```text
https://dev.dashboard.mesha.sg/
```

Use the approved Mesha Google account. Access requires both Google sign-in and
a Goat OS role grant, so a normal Google login is not enough by itself.

After login, use the top bar to review the current park/date scope. If a screen
looks empty, first check whether the top bar is scoped to a park or date that
has no sample vaccination work.

## What This Slice Is For

The vaccination slice answers four business questions:

1. Which goats or sheds need vaccination work now?
2. Who owns the next action?
3. Is proof uploaded and verified?
4. Did the work follow the published vaccination rule and SOP?

The old dashboard mostly showed counts and a shed matrix. Goat OS keeps the
matrix idea, but adds ownership, proof, verification, missed work, blockers, and
the reason something is not moving.

## Fast Walkthrough

Use this path for a first review.

| Step | Open | What to check |
| --- | --- | --- |
| 1 | Control Tower | Overall vaccination health, top broken items, owner, next action. |
| 2 | PHC -> Vaccination | Cohort matrix, per-cohort detail, supplier warmup, and shed execution rows. |
| 3 | Action Center | Due, overdue, proof-pending, verification-pending, rejected, blocked, and owner-missing work. |
| 4 | Calendar | The date view of vaccination due work, reminders, snoozes, and escalations. |
| 5 | Protocol Adherence | Expected vs actual work, adherence percent, proof count, and gaps by rule/shed. |
| 6 | Workflows | The step-by-step chain for one vaccination item: config, work created, drive, SOP, proof, verify, close. |
| 7 | Config and SOP Library | The published vaccination rule and linked SOP that generate the work. |

## Control Tower

Use Control Tower as the CEO summary.

Expected review:

- If the process is healthy, the first card says the process is intact.
- If something is broken or at risk, the alert band shows the issue, the owner,
  and the next action.
- Open an alert to see the detail drawer.
- Use the drawer links to open the same item in Action Center, Workflows,
  Protocol Adherence, or Vaccination.

What this proves:

- Broken work is not hidden in a table.
- The dashboard shows who should act next.
- CEO review starts from exceptions, not raw goat counts.

## PHC -> Vaccination

Use PHC -> Vaccination as the operating floor.

Expected review:

- The top band shows how a vaccination drive runs.
- Supplier warmup shows purchased or holding-farm vaccination evidence when it
  exists, so accepted goats are not double-dosed on arrival.
- The status matrix shows cohorts against vaccination rules.
- Per-cohort detail shows animals, age band, last dose, next due, and status.
- Drive - shed events show park, shed, owner, stock, proof, verification, and
  next action.
- Click a shed event to open the shed execution detail.

If Record / verify fields are disabled on a matrix or rollup drawer, that is
expected. The matrix is a summary, not a single task. To act on one real item,
open the Action Center work row or the shed execution detail.

If "Import sheet" on the vaccination page is reference-only, that is expected.
Vaccination drives are not hand-created from that drawer. Drives are generated
from published rules and eligible goats. Goat and shed entry lives in the herd
registration/import path.

## Action Center

Use Action Center when something needs action.

Expected review:

- Status Board groups work by state: scheduled, due, overdue, proof pending,
  verification pending, rejected, blocked, owner missing, missed, deferred, and
  completed.
- Click a card to open the work drawer.
- The drawer shows owner, due date, SOP progress, proof state, verification
  state, next action, and links to the full workflow.
- The SOP Queues view shows proof waiting for verification.
- Verify, reject, or request rework only when the row is a real review item.

If a verify/reject button is disabled, the row is missing the review handle for
that action. Open the workflow detail or the related shed execution row to see
the actual work context.

## Calendar

Use Calendar to see vaccination work by date.

Expected review:

- Week and month views show vaccination due work.
- Owner lanes separate PHC, stock, and admin/data work when the data supports
  those lanes.
- Open an event to see detail and actions such as nudge, snooze, escalation, or
  open workflow.

The "New event" button is disabled by design. Vaccination calendar items come
from published rules and real due work; reviewers should not create free-form
calendar events that bypass the vaccination process.

## Protocol Adherence

Use Protocol Adherence to check whether the rule is being followed.

Expected review:

- Overall adherence percent shows how much expected work is on track.
- Open process gaps show what is late, blocked, deferred, rejected, or missing
  proof.
- Deferred rows are shown as explained work, not hidden skips.
- Click a row to see expected vs actual, owner, next action, and proof count.
- Use the row links to open Action Center or the full Workflow.

What this proves:

- Goat OS can show why the process failed, not only that a dose was not done.
- A sick, quarantined, ICU, missing-date, or stock-blocked case stays visible
  with a reason.

## Workflows

Use Workflows to follow one vaccination item from start to finish.

Expected review:

- The chain shows: Config -> work created -> drive opened -> SOP -> proof ->
  verify -> close.
- The selected workflow shows current state, severity, SOP state, proof state,
  verification state, and completion progress.
- Open the detail page to jump to Goat Passport, shed execution, Action Center,
  and Protocol Adherence.

What this proves:

- The screen is not just a list. It explains where one item is stuck.

## Config And SOP Library

Use Config and SOP Library to confirm what creates vaccination work.

Expected review:

- Draft rules do not create work.
- Only a published rule backed by approved source records creates vaccination
  work.
- A published version is not edited in place.
- A change creates a new draft and then a new published version.
- Completed historical work stays tied to the version that created it.
- The vaccination SOP carries the proof requirements used during execution.

If Publish is disabled, check the reason shown in the modal. Common reasons are:
the user is not CEO/COO, the draft was changed after the last save, the selected
source is not approved, required source fields are missing, no SOP is selected,
or proof requirements are empty.

## Registering Goats And Sheds

If the herd registration/import screen is included in the dev review:

- Register goat creates the official Goat OS goat record.
- Import sheet previews rows first and only commits the accepted rows.
- Bad rows return row-level errors and can be exported.
- Register shed creates a vaccination-usable shed under a real park.
- A shed by itself does not create vaccination work. Eligible goats in that shed
  plus a published vaccination rule create work.

If location or animal-stage data is missing, goat/shed creation is blocked with
a visible message. That is intentional: Goat OS should not create vaccination
work for an invalid park, shed, or stage.

## What Happens When Events Occur

New goat:

- Goat OS records the goat.
- If the goat is eligible under the published rule, vaccination work appears.
- If the goat is sick, quarantined, ICU, too young, missing a required date, or
  otherwise deferred by the rule, the work stays visible as deferred with a
  reason.
- If the goat is terminally out of care, vaccination work is not created.

New shed or move to shed:

- A shed alone does not create work.
- Moving eligible goats into a vaccination-usable shed can create, move, reopen,
  or cancel open vaccination work based on the published rule.

Published rule or SOP change:

- Future work uses the new published version.
- Finished work is not rewritten.
- Open work needs an explicit outcome such as continue, cancel, supersede, or
  regenerate.

Proof submitted:

- Proof moves the item into verification.
- Accepted proof closes the work, records stock use, and schedules the next
  booster when the rule requires one.
- Rejected proof sends the item back for rework.

Missed date:

- The item becomes due, overdue, or missed.
- The owner and next action remain visible in Action Center, Calendar, Protocol
  Adherence, and Workflows.

Stock problem:

- The item is blocked with the stock reason.
- It should not disappear or be silently marked complete.

Supplier or holding-farm vaccination evidence:

- Accepted evidence is used to avoid double-dosing after intake.
- Goats rejected before truck or intake remain procurement history and do not
  become active PHC vaccination work.

## What To Test

For a short CEO review, test these:

1. Login works for an approved Mesha account.
2. Control Tower shows process health and opens an alert drawer.
3. PHC -> Vaccination shows the matrix and shed execution rows.
4. A disabled rollup Record / verify drawer explains why action must happen on
   the real work item.
5. Action Center filters work by status and opens a work drawer.
6. SOP Queues show verification rows when proof is pending.
7. Calendar opens an event and explains why free-form New event is disabled.
8. Protocol Adherence shows adherence percent and an expected-vs-actual row.
9. Workflows shows the chain and opens a workflow detail.
10. Config shows whether the vaccination rule is draft or published, and Publish
    gives a clear disabled reason when blocked.

## What Is Done In This Slice

- Vaccination rules can generate due work.
- Due, overdue, missed, deferred, blocked, proof-pending, verification-pending,
  rejected, and completed states are visible.
- Shed execution rows show owner, stock, proof, verification, and next action.
- Proof upload and SOP task submission exist on the real task path.
- Verification can accept, reject, or request rework when a real review row is
  available.
- Stock reservation and blocking are enforced by Goat OS.
- Booster scheduling works after accepted completion.
- Procurement holding-farm vaccination evidence can prevent double-dosing after
  intake.
- Critical direct ICU/quarantine/death shortcuts are blocked unless the approved
  guarded death path is used.

## What Is Not Finished Yet

These are the items to state plainly during review:

- The full ICU/quarantine critical-action approval workflow is not a live
  business screen yet. Unsafe direct shortcuts are blocked first; the full
  approval screen still needs its own policy and review flow.
- The stock issue owner workflow is not a finished inventory screen in this
  walkthrough. Vaccination shows the stock block and reason; the full inventory
  resolution flow belongs to a separate owner workflow.
- Missed-dose operating ownership still needs the final business rule for who
  receives the follow-up, how fast they must act, and what closes the exception.
- Final PHC production schedules and large source data must be loaded and
  approved before production use.
- Real notification channels may be dev-safe during review. Treat dev messages
  as proof of routing, not production delivery.
- Google dev deployment, old-dashboard archive, final seeded count, and the live
  Google walkthrough proof are separate rollout steps. Do not claim them from
  this guide until they have been run and recorded.

## Acceptance Note Template

Use this after the dev review:

```text
Reviewed URL:
Reviewer:
Date:
Seed/source summary:
Screens checked: Control Tower, PHC -> Vaccination, Action Center, Calendar,
Protocol Adherence, Workflows, Config, SOP Library.
Observed process states:
Disabled buttons explained correctly:
Items accepted:
Items not accepted:
Next owner:
```
