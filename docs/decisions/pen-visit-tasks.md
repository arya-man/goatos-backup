# Pen visits: the day-after check on a treated pen

Maintainer decision, 2026-09-07 (chat session with the maintainer). Status: ACCEPTED,
landed. **SUPERSEDED IN PART on 2026-09-12** -- read the section immediately below first;
the rest of this document describes the 2026-09-07 shape and stays as the record of what
did not change (the trigger, the natural key, the kernel clock, the materializer, the push).

## 2026-09-12: the visit is the care work's LAST STEP, not a task of its own

Maintainer instruction (chat, 2026-09-12): "the care task should close when the park head
visits the shed on the next day ... first it will be fasting, removing feed and water, then
the deworming happens, and the park head will go. When all these videos are verified by the
verifier, then the task will be closed ... don't make that park visit a separate task
altogether." Asked three questions, the maintainer answered: fold the visit into **every PC
Care category AND vaccination**; the visit video **goes to the verifier** as its own item; who
visits stays **per-park HRMS config, changeable on /people, one or more people, "if anyone
does then enough"**.

Three things changed, each load-bearing:

1. **THE VISIT VIDEO IS VERIFIED.** `pen_visit_tasks` gained the PC Care gate: `status` open
   -> pending_verification -> completed | rework, `verified_by/at`, `rework_reason`
   (migration `000295`). Submit flips the gate and leaves the kernel clock alone; the verdict
   consumer (`penvisits/app.VerificationHandler`, category `pen_visit`, module `pen_visits`,
   ref_type `pen_visit_task`, listed under the Preventive Care verify tab) flips BOTH
   dimensions to completed on approve and to rework on reject. A rework keeps the old clip as
   history; the re-shoot carries a fresh row version and mints a fresh verifier item
   (`pen-visit-verification:<task>:<row_version>`). The roll-forward sweep no longer rolls a
   visit sitting with the verifier -- the kernel gate reads `submitted_at`, never review state,
   the rule the removal cards already follow.
2. **THE PARENT CLOSES ON THE VISIT.** `pen_visit_task_sources` links a visit to the PC Care
   task(s) and vaccination submission(s) whose submit raised it (the materializer reads the
   verification item's `source_ref_type/id`; a pen worked twice in one day is still ONE visit
   with two links). A PC Care pen task -- one of `domain.PenVisitCategories` with a shed --
   whose own videos are approved sets `status = completed` but keeps `work_state` open;
   `pen_visit.verified` (emitted in the approve transaction with the parents on its payload) is
   consumed by `penvisits/app.VerifiedHandler`, which calls the PC Care closer
   `pccare.Repository.PenVisitVerified` to flip `work_state = completed`. Either order
   converges: `ApplyVerifiedTask` closes the clock itself when it finds the visit already
   verified. The closer RE-READS the visit row rather than trusting the event, so an early or
   replayed event cannot close a task whose visit is not in fact verified (the red test that
   found it: `pen_visit_closure_integration_test.go`). `pc_care.task.completed` still fires at
   the task's own approval -- the field work IS verified; the clock is what waits. The PC Care
   roll-forward sweep skips a task whose own videos are verified (`status <> 'completed'`).
   Vaccination registers no closer: its shed reads derive the step from the visit row
   (`ForPens`), and the five-bucket drive progress contract is untouched.
3. **WHO VISITS IS PER-PARK HRMS CONFIG, ONE OR MORE.** `pen_visit_park_assignees` is keyed
   `(tenant, park, user)`; `assignee_user_id` is gone from the visit row; `CheckSubmit` is a
   membership test against the park's configured visitors, and `submitted_by` records who
   went. The /people access editor carries `pen_visit_park_ids` (label and blurb are backend
   copy), saved wholesale in the same transaction as the rest of a person's access, refused
   for a park outside the person's scope and for a person who has never signed in
   (`pen_visitor_has_no_login` -- a visit is owed to a user id). The morning push reaches
   every configured visitor and lands them on the PC Care module, not a list.

**WHERE THE VISIT LIVES NOW.** The "For me" tab, its L0 route, list screen and ViewModel are
retired; the Tasks module is one list again for everyone. The visit is a STEP ON THE PARENT
CARD: `PCCareTask.pen_visit` (the shared `penvisits/domain.Step` shape, composed for the
caller) plus `pen_visit_chip`/`pen_visit_tone` so a card whose own clips are verified reads
"Pen visit tomorrow" / "Visit in review" / "Visit verified" rather than "Done";
`ShedCardSummary.penVisit` and the shed drilldown carry the same step for vaccination. The
phone's PC Care task screen shows a "Pen visit" step card and the vaccination shed card a
strip; both open the hosted visit drill (`/pen-visits/{id}`, the one route that survives),
where the drill renders the gate (in review, sent back with the verifier's words, verified).
A configured visitor also finds the parent task on their own PC Care worklist on the VISIT's
due day (`ListTasksQuery.VisitorUserID`), under its category tab, and the operator's carry
list no longer resurfaces a task whose own videos are verified as work owed. Badges follow
the parents: visits still to record count on Preventive Care (per category tab) or on
Vaccination.

**THE WORK BOARD (PR #243) FOLLOWS THE STEP.** On the task's own day the PC Care source maps a
verified task with an open clock from the VISIT: in progress (owed), overdue, in review,
rejected, completed; its subtitle says so and the issue view gains a final "Pen visit" unit
(visit -> verify). On the visit's own due day two sources in
`penvisits/adapters/boardsource` row the visit under Preventive Care (any care reason) or
Vaccination (vaccinated alone), titled as the work continuing ("Deworming · Castro 2", "Pen
visit · work done 10/09/2026"), owned by the park's configured visitors, with the vaccination
shed href. Canonical rows: `docs/decisions/work-board.md`.

**Routes** admit `pen_visits.execute` OR `pc_care.execute`; the permission opens the routes
and the per-park config decides WHO may record a given visit. `pen_visits.execute` stays on
the director roles through the Tasks module's Do tick (never `ceo_internal`).

Pinned by: `penvisits/domain` tests (gate chips, submit rule for any configured visitor,
in-review refusal), `penvisits/adapters/postgres.TestPenVisitLifecycle...` (two visitors,
parent links both ways, approve/replay/reject/re-shoot, the sweep leaving a reviewed visit
alone), `pccare/adapters/postgres.TestPenTaskClosesOnlyWhenBothTheWorkAndTheVisitAreVerified`
and `TestShedLessTaskClosesOnItsOwnApproval`, the two board-source round trips,
`workforce/adapters/postgres.TestSavePersonAccessWritesThePenVisitParks`,
`permissions.TestPenVisitsExecuteIsDirectorsNeverCEO`, the eventwiring drift guard (eleven
appliers plus the `pen_visit.verified` subscriber), and the Android
`PenVisitDetailViewModelTest` gate cases.

## What it is

The day after **any vaccination or preventive-care work is submitted in a pen** -- a
vaccination shed proof, or a PC Care task (deworming, anti protozoan, ticks removal, hoof
trimming, hair trimming) -- the park's head goes to that pen, looks at the animals, records
**one live in-app-camera video** and submits it. That is the whole task. There is no
per-animal scan, no roster, no head count and no verifier: submit is completion.

It lives on the phone's **Tasks** module as a second tab. A director's Tasks module now reads
**Raised by me** (the asks they raise for the CXO desk, unchanged) beside **For me** (the pen
visits the kernel owes them). A CXO's Tasks module is unchanged: one list, no bar.

The maintainer's words: "in For me they should get cards ... visit shed because of vaccination
or because of deworming ... they will record one video and submit. Simple task. Dinakar gets
all CBE cards, Chandrakant gets CPT."

## The decisions, each load-bearing

1. **The task is SYSTEM-RAISED, never typed.** The kernel stage `pen-visit-kernel` (shared
   operational lane, every 5 minutes) reads the pens whose work landed on a business date and
   writes one task per `(park, shed, pen, source date)`, due the next day. Nobody plans a visit;
   nobody can forget to.
2. **ONE source covers both triggers.** Every vaccination shed submit and every PC Care submit
   already writes a `verification_items` row carrying `park_id`, `shed_id`, `partition_label`
   and a category, so the materializer reads that one table for the day (`created_at` in the
   IST window -- the SUBMIT instant, never `captured_at`, which is when the animal was handled)
   and maps category to reason. `vaccination_proof` -> vaccination; `pc_deworming`,
   `pc_anti_protozoan`, `pc_ticks_removal`, `pc_hoof_trimming`, `pc_hair_trimming` -> their
   category. Inventory-vaccine and feed-and-water-removal items are not pen work and raise
   nothing. A new partial index `verification_items_created_pen_idx (tenant_id, created_at)
   WHERE shed_id IS NOT NULL` serves the read.
3. **One pen, one day, ONE task.** The natural key `(tenant, park, shed, partition_key, source
   date)` is unique. A pen vaccinated and dewormed on the same day is one card carrying both
   reasons; a late item for the same pen (an offline phone syncing after the tick) WIDENS the
   open task's reasons and never mints a second. This is what makes the tick idempotent: the
   first tick after 00:00 IST inserts, every later tick inserts nothing -- "once per day" from
   the natural key, not from a schedule, which the task-kernel lock requires. The stage looks
   back two days so a worker down at the boundary still catches up; a late-created task is
   due the LATER of source+1 and today, so nothing is born delayed.
4. **WHO visits is a per-park config row, never a fallback.** `pen_visit_park_assignees`
   names one person per park -- seeded by email: CBE -> Dinakar, CPT -> Chandrakant. A park
   whose pens had work but no row gets NO tasks and the stage logs
   `pen_visit_park_without_assignee` naming the park; it never picks a director, a park head
   position or the other park's person (the vaccination operator rule). Hemant is deliberately
   not configured ("we will discuss later").
5. **The permission is the module's Do tick.** `pen_visits.execute` is held by the six director
   roles (the raise side of Tasks) and rides `LevelDo` of the `leadership_tasks` module, so a
   person ticked Do on /people gets the tab with no code change. `ceo_internal` never holds it:
   the CXO desk answers asks, it does not walk pens. The permission opens the tab and the
   routes; WHICH visits a person sees is decided per row by `assignee_user_id`, so a director
   with no park configured against them (Hemant today) sees an empty tab with the backend's
   empty message.
6. **The bar is served only when there is something to switch to.** The 2026-09-05 "Tasks has
   no bottom bar" rule is kept for the CXO and widened into `barOnlyWhenSwitching`: a module
   flagged that way serves its bar only to a principal with two or more permitted destinations.
   A director gets the two-tab bar; a CXO still gets none. The nav item labels are backend
   copy in four locales (`nav.leadership_tasks` = "Raised by me", `nav.pen_visits` = "For me").
7. **The kernel shape is PC Care's, minus the verification gate.** `planned_business_date` is
   the immutable anchor; `due_business_date` rolls FORWARD ONLY as `delayed`
   (chunked `FOR UPDATE SKIP LOCKED`, the 000183 sweep), so an unvisited pen keeps showing with
   the date it was owed -- "Delayed since 7 Sep" -- never as fresh work. Submit flips
   `work_state` to `completed` in one transaction with the proof, audit and outbox, under the
   row lock and the row version the screen loaded with (`0` skips the fence for a client that
   does not carry one). A completed task always carries its proof (`CHECK`).
8. **The video is a PROOF and is validated like every other proof.** Same pipeline as PC Care:
   the phone records with the in-app camera, writes Room first, queues the upload and the
   submit on the outbox under one group key, and the server refuses anything that is not a
   finished, tenant-owned, in-app-camera video (`422 invalid_proof`: "record it again"). A
   gallery pick is not evidence of a visit made today.
9. **One push per park per morning, not one per pen.** The stage pushes a digest to the
   assignee for the tasks THAT pass created -- "Visit 3 pens at Coimbatore: Castro 2
   (vaccination, deworming), Godel 1 - Part 3 (hoof trimming), ..." -- keyed
   `pen_visit.due:<date>:<park>:<assignee>` so a replay tick pushes nothing. The morning push
   is deliberately NOT driven by the `pen_visit.created` event: ten pens is one round and one
   message.
10. **Every word on the card is backend copy.** Title ("Visit Castro 2 · Coimbatore"), reason
    line ("Vaccination, deworming yesterday"), state chip, tone token, instruction, done line,
    filter chips and empty messages are composed in `penvisits/domain` and rendered verbatim.
    The pen label is `oploc`'s display of `(shed name, partition_label)`, never a hand-rolled
    join.

## Data

- Migration `000277_pen_visit_tasks.sql`: `pen_visit_park_assignees` (PK tenant+park, seeded
  by email), `pen_visit_tasks` (natural key, kernel columns, completed-has-proof check), the
  two serving indexes, `pen_visit_due` in the notification-type check, and the
  `pen_visit_task` branch in `validate_outbox_event_tenant`.
- Migration `000278_verification_items_created_pen_idx.sql`: the partial index the
  materializer's day read uses, built CONCURRENTLY (no transaction) because
  `verification_items` is a populated hot table.
- Envelope enums: `pen_visit.created`, `pen_visit.submitted`; aggregate/subject
  `pen_visit_task`.

## Events

`pen_visit.created` (materializer transaction) and `pen_visit.submitted` (submit transaction),
aggregate `pen_visit_task`, topic `pen_visits.events`, deterministic event ids. Neither has a
consumer by decision (the `pc_care.task.completed` precedent): a visit has no verifier and no
downstream state; the push is a stage-queued digest, above. Registered in
`context/architecture/domain-event-registry.json`.

## Routes

| Route | Permission |
|---|---|
| `GET /app/pen-visits?filter=todo\|done` | `pen_visits.execute` |
| `GET /app/pen-visits/{task_id}` | `pen_visits.execute` |
| `POST /app/pen-visits/{task_id}/submit` (Idempotency-Key, `{proof_ref, row_version}`) | `pen_visits.execute` |

`pen_visits.execute` is also ORed into the `/app/proofs/*` upload routes, the `toxin.execute`
lever, so the visit video can finish uploading.

## Module badge, and each tab's own

The Tasks drawer badge is the SUM of both halves: the CXO's unseen asks plus the park head's
visits still owed (`penvisits/app.ModuleBadges` wraps the leadership tasks source). One person
carries one of the two today, so nothing double-counts.

**Each bar tab carries ITS OWN number** (2026-09-07, found on the first phone run): the module
badge alone landed on the first tab, so a park head with two pens owed saw a "2" on *Raised by
me*, the tab with nothing in it. `BootstrapNavigationItem.badge_count` now carries the per-tab
count -- unseen asks on `/leadership-tasks`, pens owed on `/pen-visits` -- through
`workforce/app.NavItemBadgeSource`, an OPTIONAL second half a badge source may implement. A
source that answers only the module half keeps the old behaviour (the module's number on its
landing tab), so no other module changed. The phone renders whichever the backend sent and
counts nothing itself. Pinned by `workforce/app.TestTasksBarItemsCarryTheirOwnBadges`.

## What the phone run found in the outbox engine (2026-09-07)

With the API stopped, a fresh recording sat retrying its upload while the detail read
**Submitted** and the card read **Done**. Root cause was GENERIC, not pen-visit specific:
`SyncEngine.drainOnce()` replays every recent SUCCEEDED outbox row's stored response into Room
on EVERY drain pass (the process-death repair), so the earlier submit's completed payload kept
overwriting the fresher detail. Two fixes, both pinned: the replay is now once per process
(`replayedTerminals`, a bounded id:status set -- a restart still repairs once), and
`persistServerDetail` is monotonic on the server's `row_version`, so a stale payload never
wins over a newer one. `SyncEngineTerminalReplayTest` holds the engine half.

Also proven on the Realme: recording with the server down shows "Sending…" on both the detail
and the card, killing the app mid-queue loses nothing, and the visit completes by itself within
seconds of the server returning -- register, upload, complete, submit, all under the visit's one
FIFO lane.

## The screens, as landed on the phone

List card: the pen label leads (the one line that names where to go), the backend's reason line
under it ("Vaccination yesterday"), the park name faint beneath, the state chip on the right;
a card whose video or submit is still on the wire wears a quiet "Sending" mark instead of the
chip. Detail: the pen label is the header with the park as its subtitle (the backend's full
title, "Visit Gandhi 3 · Coimbatore", is what the recorder chrome and the burned-in overlay
carry, where the whole sentence has room), then one card with the chip, the reason line and
the instruction, then the video card. Submit is implicit -- a finished recording is written
down and queued in one act, so nothing asks the park head to press anything twice.

## Pinned by

- `permissions.TestPenVisitsExecuteIsDirectorsNeverCEO`,
  `TestPenVisitRoutesAreGatedOnPenVisitsExecute`.
- `workforce/app.TestLeadershipTasksModuleIsOfferedToDirectorsAndCEO` (director: two tabs,
  CXO: none) and the amended no-bottom-bar tests.
- `penvisits/domain` tests (copy, submit rule, filters, reasons).
- `penvisits/adapters/postgres.TestPenVisitLifecycleOneToManyParkScopePaginationPostgresPaths` (materialize folds two items
  into one pen, names the park with no head, replay is a no-op, late item widens, list/counts,
  submit under the fence with audit + outbox, exact replay, roll-forward, keyset paging).
- `notificationbridge.TestPenVisitDueCopyNamesParkPensAndReasons`.

## Not in this decision

- No admin-web surface. Visits are phone-only, like the Tasks module they live in.
- No verifier and no CXO review of the videos. The video is stored as a proof and reachable
  through the audit/event spine; a review surface is a separate decision.
- No cancel path on the phone. `canceled` exists in the vocabulary for a future config change
  (a pen retired, a park re-assigned) and is never listed.
- Hemant / procurement is not configured against any park.
