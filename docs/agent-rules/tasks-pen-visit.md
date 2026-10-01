# Tasks, Pen Visit and Task-Kernel Rules

> Moved verbatim from `AGENTS.md` (split 2026-09-24 to keep session start small).
> These rules are as binding as `AGENTS.md` itself. Only file location changed.

## Pen Visit Tasks: the day-after check (maintainer decision 2026-09-07, REAFFIRMED 2026-09-14)

The day after ANY vaccination shed proof or PC Care task (deworming, anti protozoan, ticks
removal, hoof trimming, hair trimming, fumigation -- the last added 2026-09-30) is SUBMITTED in a pen, one of the park's configured
visitors owes that pen a visit and ONE live in-app-camera video. It is the phone Tasks module's
**For me** tab beside a director's **Raised by me**; a CXO's Tasks module is unchanged (one
list, no bar). Read `docs/decisions/pen-visit-tasks.md` before touching
`backend/internal/penvisits`, the `pen-visit-kernel` stage, the Tasks module's nav, or the
phone's `/pen-visits` screens. **The 2026-09-12 fold that made the visit a step on the parent
card instead was a mistake (maintainer, 2026-09-14) and is retired -- see the section below.**

Rules that must survive any edit: the task is SYSTEM-RAISED by the kernel from
`verification_items` (one source for both triggers; `created_at` is the submit instant), never
typed; one pen + one day = ONE task (natural key; a second submit only widens `reasons`); WHO
visits is `pen_visit_park_assignees` per park (CBE -> Dinakar, CPT -> Chandrakant, seeded by
on /people as `pen_visit_park_ids`, one or more people, any of whom may go) and a park with no
row gets NO task plus a loud log, never a fallback person; the permission `pen_visits.execute`
is the director roles / the Tasks module's Do tick and NEVER `ceo_internal`; the video goes to
the VERIFIER (kept from 2026-09-12) and the care task that raised the visit closes only when
that clip is approved; unvisited pens roll FORWARD as delayed and keep their planned date; the
morning push is ONE digest per park per date (stage-queued, business-date key), not one per
pen, and lands on `/pen-visits`; and every word on the card is backend copy.
Hemant/procurement is deliberately not configured against any park.

## The Pen Visit Is A Task Of Its Own On The Tasks Module (maintainer decision 2026-09-14, RETIRING the 2026-09-12 fold)

On 2026-09-12 the visit was folded INTO the care work as its "last step": a "Pen visit" box on
the deworming task, a "Visit pen today" chip on the PC Care card and round card, a strip on
the vaccination shed card, the Tasks "For me" tab removed, and board rows under Preventive
Care / Vaccination titled as the work continuing. On 2026-09-14 the maintainer called that
day a mistake: **"keep it in Tasks module only; the common board also -- it should come like
this only, under Tasks only."** The visit is a task of its own again:

1. **The phone lists it on the Tasks module's "For me" tab** (`/pen-visits`, badge = visits
   still to record), beside "Raised by me"; the tab is the generic "work the system owes this
   person" list -- pen visits are its first card type, and a future module adds a CARD TYPE
   here, never a third tab. The park head opens the card, records one video, it submits
   itself, and the clip goes to the verifier.
2. **The parent surfaces carry NOTHING about the visit.** `PCCareTask` has no `pen_visit`,
   `pen_visit_owed`, `pen_visit_chip`; the round card has no visit chip; `ShedCardSummary` and
   the shed drilldown have no `penVisit`; the PC Care worklist admits no visitor scope. A
   deworming card whose own clips are verified reads "Done"; its kernel clock still waits on
   the visit (`pen_visit.verified` -> `pccare.Repository.PenVisitVerified`), invisibly.
   The tab, the routes and the visitor config share ONE permission: `/app/pen-visits*` ride
   `pen_visits.execute` alone (no `pc_care.execute` OR -- a route without a screen strands a
   visit behind a push), and `/people` refuses a pen-visit park for a person whose saved access
   lacks it (`pen_visitor_cannot_reach_tasks`). A care operator who should visit gets Tasks/Do
   ticked first.
3. **The Work Board rows it under TASKS** (`workboard/domain.ModuleTasks`, appended last so the
   cursor order holds; visible on `pen_visits.execute` or `leadership_tasks.read`): ONE
   `penvisits/adapters/boardsource` source, "Pen visit · Castro 2", subtitled "Deworming · work
   done <date>", no href. The PC Care and vaccination rows read their own work's state only.

What the 2026-09-12 decision got RIGHT and is KEPT: the visit video is verified (category
`pen_visit`, Preventive Care verify tab, rework re-shoots); the care task closes only when
both its own clips and the visit are approved; who visits is per-park HRMS config on /people,
one or more people. Do not re-fold the visit into a parent card, and do not re-title the board
row as the parent work. Canonical prose: `docs/decisions/pen-visit-tasks.md` -> "2026-09-14".

## Simple Tasks Get A Phone Tab Defined On The Web (maintainer instruction 2026-10-01)

A SIMPLE task -- a pen, a schedule, one person, questions and photo/video captures, optional
verifier, and nothing the server must compute (fumigation-style) -- is added with NO code: it is
authored as a "Task with its own phone tab" SOP on ITS MODULE'S SOP page (`form_dsl.phone_task`:
tab icon and filters, pens, schedule, who per park, questions, captures, verifier). Publishing the
SOP writes, in the publish transaction, a phone tab in that module's bottom bar and one pen routine
per park (`sop_code`); those routines are never edited on `/routines`. The phone renders every such tab
with ONE parameterized root, `/pen-routines/tab/{tab_key}`. Complex modules (feed direction /
packing / transport / wastage, weighing, vaccination, PC Care's own categories) stay coded and must
not be moved onto it. This is the one place a bar item comes from data: the module and icon keys
are closed vocabularies in `penroutines/domain/tab.go`, mirrored by `MeshaIcons.forTabIcon`, and
who gets the tab is who owes the work (the task-list predicate), never a role template. A new
simple task is an SOP on its module's SOP page, never a new screen or a new registry entry. Canonical prose:
`docs/decisions/simple-task-phone-tabs.md`.

## Operational Task-Kernel Non-Deviation Lock (Mandatory)

Maintainer decision 2026-08-10: Goat OS is one event-driven, interlinked
task/ticketing waterfall. Every operational feature follows:

```text
business event -> canonical transaction + audit/outbox -> real owner + clock
-> bounded task hierarchy -> acknowledgement-gated contact waterfall -> proof
-> separate verification/sign-off task -> close/reopen rollup -> shared reads
```

Module state machines remain authoritative for domain facts, but no module may
create, retain as canonical, or exempt a private app-visible task authority,
scheduler, owner fallback, overdue calculation, reminder/escalation ladder,
verification queue, or screen-only follow-up pipeline. A feature whose shared adapter is not ready
stays shadowed or blocked; it does not bypass the kernel. Every activated,
app-visible task has a real owner and pinned clock. Failed owner resolution
creates a separate durable exception owned by a real configuration/operations
resolver; the exception is not a substitute owner and the task stays hidden.
Operator and verifier/sign-off work are separate sibling leaves.
Acknowledgement stops contacts, not the work clock, and authorized descendant
reopen propagates upward.

Task clocks and accountability follow
`docs/decisions/task-timing-alerting-violations-and-appeals.md`. Planned,
available, flexible, hard-deadline, clinical-safe, contact, and appeal clocks
must never be collapsed. Vaccination drives and Weighing allow the accepted
two-day carry-forward described there. A breach is not a personal violation:
the kernel must complete attribution, notice, appeal, and independent decision
before a final finding, and it never calculates or changes salary/payroll.

Read and obey
`context/execution/operational-task-kernel-remediation-plan.md` and
`context/execution/defect-prevention-execution-contract.md` for any trigger,
task, Today/My Tasks, owner/duty, clock, reminder, escalation, proof,
verification, hierarchy, or shared operational-read change. Changing this
architecture requires an explicit maintainer decision plus same-change updates
to the kernel architecture, plan, skills, guardrails, and adversarial tests.
Ambiguity is not approval.

The persistent multi-session checkpoint is
`context/execution/operational-kernel-program-state.md`. Coordinators update it
on the sole integration branch after each accepted batch; worker and review
agents never edit it.
