# Procurement journey orchestration engine

Status: **Proposed** (2026-10-01). Not built. Decision owner: the maintainer (founder). Owner once accepted: `procurement_journey` (new
module), with procurement, animalpurchase, tasks, notification and platform for the parts listed
in [Touched modules](#touched-modules).

This ADR is the engine half of `docs/decisions/procurement-journey.md` and its build spec in
`docs/features/procurement-journey/` (README, journey-map, configuration, data-model, views,
engine-gaps, delivery-plan). It uses that spec's words: request, sourcing, journey (one vendor
load that was fixed), stage, SOP document, journey profile, `profile_snapshot`, riding AM,
transit manager, engine-hook step, funnel.

## Scope of the decision

Temporal with a saga is chosen **for the procurement journey only**. It is not a Goat OS-wide
workflow engine, and no generic Goat OS workflow engine is built first. The journey also does not
rely only on the existing Pub/Sub and kernel. Postgres stays the source of truth for every
business record; SOPs and configuration stay in Goat OS. There is **one Temporal workflow per
journey** (one fixed vendor load). It opens Goat OS tasks, waits for their completion signals,
waits on dispatch dates and deadlines (orchestration waits only; the kernel still owns every clock
a human sees), and handles truck and transit delay, partial delivery, seller terms and NDA gates,
load approval blocking later stages, mid-journey responsibility changes, and cancellation.
Cancellation opens reverse **human** undo tasks (recover advance, cancel truck, inform labour,
close vendor terms breach). Temporal remembers the order and the state; people do the real-world
undo.

## Why existing GoatOS flows never needed this

Every flow Goat OS runs today is short, single-owner, or anchored to a fixed clock. Event handlers
plus kernel sweepers are the right tool for them:

- **Vaccination obligations** are minted by event handlers, one goat at a time:
  `GoatCreatedHandler`, `GoatRecheckHandler`, `GoatReinstatedHandler`, `ProtocolPublishedHandler`
  (`backend/internal/vaccination/app/generation_handler.go:89,121,150,180`). Each run is
  idempotent and stateless between events.
- **Lateness and escalation** are a stateless sweep: the obligation sweeper runs on the 15-minute
  operational cadence (`backend/internal/kernelstages/obligation_sweeper.go:160`, `Run` at 205)
  with escalation levels at 0, 4 h, 24 h and 48 h (`:84-92`).
- **Cancel is a status update**, not an undo: `goat.exited` cancels the goat's open obligations
  in one repository call (`backend/internal/obligation/app/cancel.go:38-47`). Nothing outside
  the database must be reversed.
- **Feed** is a short chain inside one day on a fixed clock: direction, issue, packing and
  feeding freeze at the scheduled dispatch instant (07:00 normal, 14:00 experiment, on D-1)
  (`backend/internal/feeddirection/app/lifecycle_gate.go:12-40`).
- **Birth and death** open and cancel per-subject workflows from events
  (`backend/internal/tasks/app/event_handlers.go`).
- **Delivery reliability** comes from the outbox: retries, then permanent failure or dead letter
  with an error log (`backend/internal/outbox/app/service.go:218-232`).

| axis | vaccination | feed | procurement journey |
|---|---|---|---|
| Chain length and lifetime | One dose per obligation; days | About 4 steps within 1 day | Ten stages, 30+ days |
| Who owns the next step | Fixed by protocol and pen | Fixed by park role | Changes per stage; picked people per load |
| Outside commitments to unwind | None (status update) | None (frozen sheet) | Advance paid, truck booked, labour hired, vendor terms |
| Blocking approvals and moving dates | None; dates from anchors | Fixed daily clock | Load approval blocks later stages; dispatch date moves |
| Mid-flow reassignment | Rare; next sweep picks it up | None | Riding AM or transit manager can change mid-transit |
| Partial outcomes | Per goat; done or missed | Per pen; served or not | 70 expected, 50 delivered: reconcile, pro-rate, reopen or close short |

The procurement journey is the first flow where all six are true at once.

## Core rule in one paragraph

**The kernel owns every clock a human sees. Temporal owns the order of the journey.** A human
step is always a kernel `workflow_actions` task with a real owner and the canonical clock fields.
When a step should appear later ("two days before dispatch"), the journey opens it **now** with
`available_at` in the future; the kernel decides when it shows, when it is late, who is contacted
and who is escalated to. Temporal decides only what comes next: sequencing, waiting for signals,
branching, joins, child stages, saga compensation, forward recovery, exception subflows, and its
own engine-internal timeouts (for example "stop retrying an undo after 24 hours"). Temporal never
holds a timer that decides when a person sees work or when they are late.

## Context

The procurement journey runs from a CXO's request to animals in a pen and the last rupee paid:
ten stages, weeks long, many owners, two people picked per load (the **riding AM**, with the load
from loading to arrival, and the **transit manager**, at the park or office, the one contact for
the AM), money milestones, a series of transit checks, and real exceptions (truck breakdown,
deaths in transit, a stock check where too few animals meet our terms, a short delivery).

`engine-gaps.md` lists nine gaps in the tasks/SOP engine (G1 chaining, G2 day and anchor
schedules, G3 step-bounded series, G4 owner lists and picked people, G5 completion guards, G6
lateness sweep, G7 per-step review, G8 Work Board source, G9 studio) and proposes chaining stages
by outbox events. That works for the happy path. It does not answer three questions the office
asks on day one:

1. **Where is this load stuck, and why?** With choreography the answer is spread across
   `workflow_actions`, hook receipts, outbox rows and Pub/Sub delivery.
2. **Who owns an exception?** A truck breakdown is not the next step of any SOP.
3. **How do we undo, or push forward?** A rejected load must release the truck, the people and
   the feed in reverse order, exactly once. A cancelled load already on the road cannot be undone
   at all; it must be brought somewhere safe.

Today (origin/main `ecc0ca1a28`):

- `internal/procurement` owns `procurement_loads` with a hard-coded Go state machine
  (`source_warmup` ... `accepted_intake`). No SOP authoring, no phone flow.
- `internal/animalpurchase` owns the phone inspection and intake SOP and stops at the decision
  (`animal-purchases.md:45`, rule 7: an accepted animal is not written to `goats`).
- `internal/sop` publishes versioned SOP documents; `internal/tasks` opens subject workflows into
  `workflow_actions` with engine-hook completion and the CEO floor.
- Outbox, outbox relay, Pub/Sub and `cmd/kernel-worker` (`kernelstages`) carry events and run
  cadence stages (`notification-delivery.md:29`).
- Undo commands that exist: `tasks.CancelSaleWorkflow` (`backend/internal/tasks/app/service.go:755`,
  the pattern for cancelling a subject's open workflow) and `obligation.CancelOpenForGoat`
  (`backend/internal/obligation/adapters/postgres/repository.go:6808`). No vehicle, payment
  ledger, feed issue line or person-pick record exists for a journey yet.

## Why Temporal, given the kernel keeps all human time

Once the kernel owns every visible clock, what is left for an engine is still substantial:

| need | why kernel sweepers are the wrong place |
|---|---|
| Durable state for a multi-week journey | One load lives 30+ days across ten stages. The sweepers are stateless scans; they would need a hand-built per-journey state machine, cursor and history. |
| Signal-driven branching | "Distress on a check -> call; breakdown -> replacement truck; under half the stock meets terms -> cancel" are reactions to events, not cadence scans. |
| Ordered compensation that is guaranteed to finish | Undo must run in reverse, exactly once, with retries and a human fallback. Sweepers have no ordered list and no completion proof. |
| Forward recovery after the point of no return | After departure, cancel means "divert, return or sell", a child flow that must finish in arrival or a recorded loss. |
| Parent and child stages | Warm-up and transport prep run in parallel and join; a replacement-truck flow nests inside transit. |
| Versioned, replay-tested code | Running loads keep their process; interpreter changes are proved safe by replaying recorded histories in CI. |

Building these in `kernelstages` means rebuilding Temporal, worse, inside the kernel every module
depends on. The kernel stays generic and small; the engine is scoped to one module.

## Alternative: extend the existing kernel (G1/G2/G3/G6 as consumers and stages)

This is viable, and cheaper in runtime terms: no new service, no new SDK, the same outbox, Pub/Sub
and `kernelstages` every module already uses. G1 chaining becomes outbox consumers, G2 anchors
and G3 series become task fields, G6 becomes a lateness stage.

It is rejected because the per-load coordinator state would be spread across consumers, hook
receipts and outbox rows, and these become bespoke code per case:

- re-planning every open step when the dispatch date moves;
- partial-delivery branching (reconcile, pro-rate, reopen sourcing or close short);
- ordered human undo before departure and forward recovery after it, each guaranteed to finish;
- mid-journey reassignment of picked people, including during transit;
- answering "where is this load stuck, and why" from one place.

Temporal gives one durable coordinator per load, with its history, signals and replay tests.

The choice does **not** depend on external API calls. The journey talks only to Goat OS modules
through ports; it would still choose Temporal if no vendor, transporter or payment API were ever
integrated. The tipping conditions are the six axes above, not integration count.

## Decision

### 1. The platform stays as it is

One modular monolith. Kernel worker, outbox, relay and Pub/Sub are unchanged. **Pub/Sub
broadcasts facts; it is not the journey brain.** Other modules keep learning about the journey
from outbox events.

### 2. A new hexagonal module, `procurement_journey`, with Temporal as its engine

`backend/internal/procurement_journey/` follows the golden rule (`internal/sales` is the
reference):

| layer | holds |
|---|---|
| `domain/` | The pure **journey spec** (stages, node types, rule model, invariants) and `Advance(spec, state, event) -> (state, []Command)`. Standard library only; deterministic, no clock, no I/O. |
| `app/` | Use cases: publish and validate a template version, start a journey, record an answer or exception, change dispatch date, reassign a pick, amend terms, migrate (audited), read projected status. |
| `ports/` | `JourneyEngine` (start, signal, cancel, debug query), `RuleEvaluator`, projection and template repositories, and **module command ports** (open/cancel stage tasks, re-anchor tasks, book/release vehicle, issue/return feed, assign/unassign person, record departure, cancel dispatch, notify). Errors live here. |
| `adapters/temporal/` | Workflows, activities and the signaler. Activities call ports only. |
| `adapters/cel/` | `RuleEvaluator` on `cel-go` (default; see [open questions](#open-questions)). |
| `adapters/postgres/` | Template versions, projection, timeline, signal inbox, outbox writes. |
| `adapters/http/` | Thin handlers: board, detail, timeline, approve/reject, exception report, admin actions. |

**Temporal is scoped to this module.** One parent workflow per journey; each stage is a child
workflow keyed on its SOP document (`procurement.source_warmup`, `procurement.transit`, ...).

**Boundary guards** (added to the existing set, run by `make check`):

- `go.temporal.io/...` imported only under `procurement_journey/adapters/temporal/` and the `cmd/`
  wiring.
- `adapters/temporal` never imports another module's adapters; activities call ports.
- No HTTP handler calls Temporal synchronously (the journey's API paths stay inside the 500 ms
  budget).
- The determinism guard in [Determinism](#determinism-rules-and-guard).

### 3. Node types

The spec is data, versioned, validated at publish, interpreted by `Advance`.

| node | meaning | journey example |
|---|---|---|
| `task` | A human step: a kernel `workflow_actions` row opened through the tasks port with its clock fields. The workflow waits for its completed signal. | `reach_vendor`, `transit_check_{n}` |
| `approval` | A task answered only by named designations. | `load_approval` (Procurement Director with CXO) |
| `wait_until` | Opens the node's tasks **immediately** with `available_at = anchor - offset`. The kernel owns the time; the workflow holds no timer for it. | transport prep at dispatch - `{profile.transport_prep_days}` |
| `parallel` / `join` | Open branches together; continue when the named set is done. | warm-up and transport prep -> loading |
| `choice` | Branch on a rule over answers, profile and journey fields. | `transit_check = distress` -> `distress_call` |
| `subflow` | Run another SOP document as a child workflow. | each stage; replacement-truck SOP |
| `guard` | A rule that must hold before a node completes. | documents present before departure |
| `escalate_after` | Opens **no** task. Declares the follow-up step the kernel's escalation ladder opens through G4 `on_late`; the workflow only records the fact. | `check_missed_{n}` for the transit manager |
| `on_exception` | Handler for a named exception signal. | `truck_breakdown` -> replacement-truck SOP |
| `compensate` | The undo command for a node, with `valid_until`. | `arrange_truck` -> release vehicle |

Engine-hook steps from `configuration.md` (`vendor_fixed`, `load_approval`,
`journey_tag_animals`, `departure_recorded`, `arrival_recorded`, `arrival_reconcile`,
`place_in_pen`, `pc_handoff`, `payment_milestone`, `animal_purchase_decision`) stay facts the
system records; publish refuses a version that drops one (`engine_step_removed`).

### 4. Versioning

- A **journey template version** is an immutable list of SOP document versions (one per stage)
  plus the spec that wires them. Publish validates references, rules, `{profile.*}` keys, required
  picks, no cycles, and that every `compensate` names a registered command.
- A journey **pins** everything at fix vendor: the template version and the `profile_snapshot`.
  The Procurement Director may adjust profile numbers for this load within the farm range; the
  server enforces min/max.
- A running journey never changes version except through `MigrateTemplate` (see
  [Mid-journey changes](#mid-journey-changes)).
- Interpreter changes (Go workflow code) use `workflow.GetVersion` per change ID plus worker
  deployment versioning. An old branch is removed only after replay tests prove no open history
  uses it. A `cel-go` upgrade counts as an interpreter change.

### 5. Clock fields per step

Each journey step stores the canonical clock fields of
`task-timing-alerting-violations-and-appeals.md` (lines 9-16 five separate facts; lines 51-67
field table), never one collapsed status:

| field | journey use |
|---|---|
| `available_at` | `wait_until` anchor - offset; transit check `n` = departure + n x interval |
| `planned_at` | the check or prep time shown on the phone |
| `flexible_until` | grace for prep steps (from profile) |
| `hard_deadline_at` | only where ratified: feed/water stop interval, documents before departure |
| `contact_next_at` | transit manager's call timer; independent of the check's clock |
| `business_timezone` | `Asia/Kolkata` |
| `clock_policy_version` | the pinned template version |

Rules:

- A missed transit check is a **breach fact**, never a violation candidate against the riding AM
  (no network on a highway is not a fair opportunity). Lateness never becomes an HR finding.
- The transit manager acknowledging the call stops further contacts; it does not change the
  check's own clock.
- Anchors are converted to UTC instants in `app` at pin time (`Asia/Kolkata`); the workflow only
  sees instants.
- Offline phone captures carry `captured_at` (device time and GPS) separate from `synced_at`.
  Lateness is judged on `captured_at` once synced; "captured, uploading" shows as pending, not
  missed.

## Signal ingress via outbox, and Temporal-down behaviour

No request handler ever calls Temporal. Every fact enters the workflow through the outbox.

| step | what happens |
|---|---|
| 1 | The owning module commits the fact and an outbox row in one transaction (task completed or overdue, departure, arrival, exception, admin action). |
| 2 | A `procurement_journey` consumer (`adapters/temporal/signaler`) reads it and calls `SignalWorkflow` with the event ID as idempotency key; retries with backoff. |
| 3 | Start: the fix-vendor transaction writes `procurement.journey.start_requested`; the signaler calls `SignalWithStartWorkflow`. A duplicate start returns success. |
| 4 | The signaler rejects a signal whose tenant does not match the workflow's tenant. |

**When Temporal is down:** phones and screens keep working because every write is a normal module
transaction. The outbox backs up and drains in order when Temporal returns. Tasks already open keep
their kernel clocks, contacts and escalations. The projection shows `engine_lag` on a journey when
its oldest undelivered signal is older than 5 minutes. Timers that fire after an outage open the
late work once; they never replay N missed checks.

## Idempotency and races

| case | rule |
|---|---|
| Same signal twice | Workflow state keeps the set of handled `(action_id, kind)`; `Advance` on a repeat is a no-op. |
| Overdue arrives after completion | `task_overdue` for an already completed action is dropped. |
| Completion arrives after the kernel opened a follow-up | `task_completed` cancels the open `check_missed_{n}` through the tasks port. |
| Escalation activity races a completion | The activity re-reads the action in its own transaction and does nothing if it is done. |
| Commands to other modules | Every command port takes key `journey_id/node_id/iteration`; the owning module stores it under a unique constraint. |
| Projection writes | `eventSeq` is a counter in workflow state; `RecordJourneyEvent` is keyed on `workflowID + eventSeq`. |
| Duplicate journeys | Fix vendor is idempotent on `(request, quote)` plus a client key; a request refuses new journeys once agreed counts reach the asked count. |
| Queued offline capture | A capture that syncs late cancels any pending escalation for that check. |

## Determinism rules and guard

- Inputs are passed in, never fetched inside workflow code: pinned spec, profile snapshot and
  absolute anchor instants, or one `LoadPinnedSpec` activity at start.
- Use `workflow.Now`, `workflow.NewTimer`, `workflow.Go` only. Map iteration is sorted. `Advance`
  returns commands in a stable order.
- Rule evaluation (`RuleEvaluator`) has vetted functions only, no `now()`, and cost limits checked
  at publish.
- **Guard:** fails `make check` on `time.Now`, `time.Sleep`, `math/rand`, the `go` keyword, `os`
  or `net` under `adapters/temporal/workflows/`, and on unsorted `range` over a map in `domain/`.

## Continue-as-new, IDs and parent close

| item | rule |
|---|---|
| Parent ID | `journey/<tenant>/<journey_id>`, reuse policy RejectDuplicate. |
| Child ID | `journey/<tenant>/<journey_id>/stage/<stage>/<attempt>`, `ParentClosePolicy = RequestCancel`. |
| Recovery subflows | Numbered attempts (`.../replacement_truck/<n>`). |
| Continue-as-new | At every stage boundary and whenever `GetContinueAsNewSuggested` is true. Carries cursor, picks, compensation list, `eventSeq`, dedup set. Children do the same. |
| Compensation context | Runs under `workflow.NewDisconnectedContext` so a cancel of the parent does not cancel the undo. |
| Tenancy | One namespace per environment; tenant in the workflow ID and a `TenantID` search attribute; activities set tenant context. |

## Compensation vs forward recovery

**What "saga" means here.** A saga is the forward journey plus a planned recovery path for every important step. One procurement deal cannot sit in one database transaction, so each step completes on its own, and each important step declares what to do if the journey cannot continue normally. Recovery is not always a perfect rollback; it can branch. Example: the seller promised 70 animals and only 50 are acceptable. The Procurement Director decides between (A) accept 50 and reopen sourcing for 20, (B) renegotiate price, or (C) reject the load and recover the advance; escalation is how that decision reaches them, not a separate outcome. Recovery paths cover cancellation, partial delivery, vendor breach, truck delay, health rejection, transit issues, payment adjustment, reopening sourcing and human escalation. Temporal is the engine that remembers and runs or waits through the saga; the saga is the business pattern that says what to do.

**Point of no return: `departure_recorded`.** Before it, a failure or cancel undoes. After it,
nothing is undone; the load is brought somewhere safe.

| phase | on cancel or failure | owner |
|---|---|---|
| Before departure | Run the compensation list in reverse. Each entry has `valid_until`; an expired entry becomes a human task. | workflow, then owning modules |
| After departure | Compensation disabled. A **forward-recovery subflow**: divert to nearest park, return to vendor, or sell off. Ends only in arrival or a recorded loss. Cancel dispatch is refused once `departed_at` is set. | Procurement Director |

Undo commands (owning modules must build the missing ones before the stage that needs them
ships; publish refuses an unregistered command):

| undo command | owner | today |
|---|---|---|
| cancel a stage's open actions | tasks | pattern exists (`CancelSaleWorkflow`); stage variant **missing** |
| cancel open vaccination for a goat | obligation | **exists** (`CancelOpenForGoat`) |
| close a tagged goat (journey cancel or transit death) | identity / health | exit path exists; reasons **missing** |
| release vehicle | procurement (`procurement_journey_vehicles`) | **missing** |
| return journey feed to stock | feed / inventory | **missing** |
| unassign riding AM / transit manager and revoke grant | tasks / permissions | **missing** (G4) |
| cancel dispatch (before `departed_at`) | procurement | **missing** |
| mark unpaid milestone void; paid money -> refund-due task with owner and due date | procurement payments ledger | **missing**; paid money is never auto-reversed |
| recover advance (human task) | procurement payments ledger | **missing** |
| cancel truck booking (human task) | procurement | **missing** |
| inform labour (human task) | tasks / notification | **missing** |
| close vendor terms breach (human task) | procurement | **missing** |
| release candidates back to the request | animalpurchase / procurement | **missing** |

Undo commands succeed when the target is already undone or missing.

**`compensation_stuck`:** an undo retries for a bounded time (24 hours, engine-internal timer).
Then the workflow opens a `compensation_failed` task for the Procurement Director carrying the
command, error and key; the projection shows `compensation_stuck`; the workflow waits for
`CompensationResolved{node, retried | done_by_hand}`. An entry is never skipped silently.

## Mid-journey changes

All are signals from audited `app` use cases, validated by `Advance`.

| signal | who | effect |
|---|---|---|
| `DispatchDateChanged{new_date, by, reason}` | Procurement Director | Re-anchors `available_at`/due of open prep tasks via the tasks port; re-checks dispatch >= approval + warm-up days; if truck or labour is booked, opens "re-confirm for new date". Earlier than warm-up end is refused unless the Health Director signs off. |
| `ReassignPick{role, new_user}` | Procurement Director | Moves open tasks and the per-journey grant; timeline row. During transit it opens a handover step. |
| `AmendTerms{rate, counts, split}` | Procurement Director; CXO above threshold | Recomputes unpaid milestones only. Paid milestones never change. |
| `MigrateTemplate{target_version}` | admin, audited | Validated by `Advance`; refused if the journey is past a node the target lacks; adopted at the next continue-as-new. |

## Field exceptions

| case | node / signal | recovery type |
|---|---|---|
| Truck breaks down | `ReportException{truck_breakdown}` -> replacement-truck subflow (4 h step, kernel escalation). Vehicle rows: `seq`, `status active / broken_down / released`, from/to times. Transfer step scans every tag, records count and losses; transit check series paused during transfer; second truck is a landed-cost line; old vehicle charge kept as cost. | forward recovery |
| Riding AM has no network | Checks captured offline with `captured_at`; `check_missed_{n}` answer "AM reached by phone, no network" ends escalation. | none (no false alarm) |
| Distress on a check | `choice` -> `distress_call` for transit manager: continue, rest, divert to a vet. | branch |
| Death in transit | Any check can record a loss directly; goat closed via existing death workflow; `tell_health` opens a health case. | record and continue |
| Under 50% of the stock meets our terms (stock check) | At stock verification, if fewer than half of the animals the vendor brought meet our terms, the deal is cancelled. Compensation opens ordered undo tasks: (1) settle the 10% advance (Procurement Director), (2) close the vendor terms with the reason (Procurement Director), (3) reopen sourcing for the request (Procurement Manager). | compensation |
| Underweight before dispatch | `pre_dispatch_weigh` with a guard on the weight band; below band -> `mark_removed` underweight or renegotiate via `AmendTerms`. | branch |
| Partial delivery (e.g. 70 expected, 50 delivered) | `arrival_reconcile` records expected vs delivered; a `choice` branches on the shortfall: pro-rate unpaid milestones on accepted animals, then the Procurement Director requests the shortfall (reopens sourcing on the same request) or closes short. | branch |
| Seller terms or NDA not signed or breached | `guard` on `vendor_terms_signed` before load approval and before any advance; a breach signal holds the journey and opens `close_vendor_terms_breach` for the Procurement Director. | gate, then undo task |
| Overloaded truck | Guard: loaded <= vehicle capacity; more animals need another vehicle row. | guard |
| Pen full at arrival | Pen capacity guard before departure; re-pick pen step at arrival. | guard, branch |
| Approver unavailable | Delegate list per approval; kernel ladder escalates up to the CEO floor. | kernel |
| Cancel after departure | Forward-recovery subflow (divert / return / sell). | forward recovery |
| Unhandled exception kind | Projection `blocked`; transit manager and Procurement Director pushed. | fail safe |

## Tagged goats in procurement

Tagging at the vendor (day 1 of warm-up) creates `goats` rows with lifecycle `in_procurement` and
location `vendor:<journey>` until `place_in_pen`. Feed direction, counts, sales allocation, herd
signals and `ceo_ai` herd views exclude them, each pinned by a test. Passport and herd signals can
see them as in procurement. This amends rule 7 of `animal-purchases.md` (line 45).

## Projection: Temporal is the truth, Postgres is the read model

- Temporal history decides **which stage the journey is in**. Each human step's state stays in
  `workflow_actions`.
- One activity, `RecordJourneyEvent`, writes in **one transaction**: projection
  (`procurement_journeys` status and funnel columns), a `procurement_journey_events` timeline row,
  and the outbox row.
- Board, detail, Work Board source (G8), search, `ceo_ai.*` views and Load wise read the projection
  only. A live Temporal query is allowed only on a single-load support view.
- Projection lag is normally under a second; the UI shows `updated_at`.
- A reconciliation stage compares projection `eventSeq` with workflow state for open loads.
- `procurement_journeys.status` is written only by `RecordJourneyEvent`; `procurement_loads`
  stays read-only history (Decision 9 of `procurement-journey.md`).

**Cutover:** by fix date, no backfill. Any open source-entry load finishes on the old machine; every
journey fixed after the switch runs on Temporal; no load on both. Guard: a journey row needs a
`start_requested` event.

## AI is suggestion-only

AI runs as activities that **suggest** (vendor from the quote sheet, route, exception handling,
why a load is blocked, conflicts on template publish). Each suggestion is on the timeline with
model and inputs. It never satisfies an `approval` or `task`.

## Worked example: J-27

Request #12: 75 male sheep, 15-17 kg, fattening. Vendor fixed at ₹400/kg, 75 + 8 buffer, dispatch
18/10. Twelve-hour trip, checks every 3 hours, payments 10/20/60/10.

| when | what happens | mechanism |
|---|---|---|
| fix vendor | Journey J-27 opens; template and profile pinned; 10% milestone task opens. | `start_requested` -> `SignalWithStartWorkflow`; `RecordJourneyEvent` |
| 03/10-04/10 | Stock weighing, inspection, Health Director decisions; load approval (Procurement Director with CXO): 72 accepted, 72 x 16.2 kg x ₹400 = ₹4,66,560. 20% due. | `task` / `approval`; compensation entries start |
| 04/10-17/10 | Warm-up 14 days, day 1 tagging (goats `in_procurement`) and vaccine on video. Transport prep tasks open on 04/10 with `available_at` 16/10 (dispatch - 2 d): picks, truck, 3 days' feed issued. | `parallel`/`join`; `wait_until` opens tasks now, kernel shows them later |
| 18/10 05:40 | Loading: 71 scanned, injection on video, papers checked, depart. 60% due. Compensation disabled. | guard; `departure_recorded`; point of no return |
| 08:40 | Check 1 open at `available_at` 08:40, done on time. | kernel task |
| 11:40 | Check 2 not done by its deadline. The **kernel ladder** marks the breach fact and opens `check_missed_2` for the transit manager ("call the riding AM"), setting `contact_next_at`. The workflow receives `task_overdue` and only records it. At 11:52 the AM's capture syncs (`captured_at` 11:41, no network): the late capture cancels the pending escalation; no violation candidate. If the transit manager had not answered, the ladder would push the Procurement Director. | kernel ladder; dedup and race rules |
| 08:50, 14:50 | Feed and water stops. | kernel tasks |
| 17:10 | Arrival; open checks cancelled; count reconciled; placed in pen; goats leave `in_procurement`; landed-cost lines written. Park warm-up 14 days, PC handoff, final 10%. | `arrival_recorded`; `subflow` |

**Breakdown variant.** At 11:30 the truck breaks down. The riding AM reports
`truck_breakdown`. The workflow pauses the check series and starts
`replacement_truck/1`: book a second truck (4-hour step; lateness is the kernel's), transfer with
every tag scanned (71 in, 0 lost), record the new vehicle and driver. Vehicle 1 becomes
`broken_down`, vehicle 2 `active`. Checks resume from the new departure. Both truck charges become
Transport lines in landed cost. Nothing is compensated; this is forward recovery.

**Partial delivery variant.** The load approval fixed 70 animals; 50 arrive. `arrival_reconcile`
records 70 expected and 50 delivered with the reason per missing tag. The workflow branches:
milestones not yet paid are pro-rated on the 50 accepted animals (paid milestones never change;
any overpayment becomes a refund-due task). The Procurement Director then answers one task:
**request the shortfall**, which reopens sourcing on request #12 for 20 animals, or **close
short**, which ends the journey at 50. Either answer is a timeline row.

## How each engine gap is closed

| gap | how |
|---|---|
| G1 chaining | `parallel`/`join`/`subflow` in the journey workflow. `stage_completed` still emitted as a broadcast fact. |
| G2 anchors, `{profile.*}` | `wait_until` writes `available_at` from pinned anchors; a missing anchor fails `Advance` (`anchor_missing`); keys checked at publish (`unknown_profile_key`). |
| G3 series | Transit child workflow opens `transit_check_{n}` (each with its own `available_at`) from departure until arrival, then cancels the rest. `max_count <= 100`. |
| G4 owner lists, picks | Still in tasks. The workflow holds the picks and passes `assignee_user_id`; G4 `on_late` is what the kernel ladder uses for `escalate_after`. |
| G5 guards | `guard` nodes over projected ledger counts; tasks' `409 <guard>_pending` uses the same predicate. |
| G6 lateness | Kernel `workflow-lateness` stage, as `engine-gaps.md` says. The workflow reacts only. |
| G7 per-step review | Unchanged; not in v1. |
| G8 Work Board | Reads the projection. |
| G9 studio | SOP studio authors steps inside stages; stage graph is read-only in v1. |

## Touched modules

| module | change |
|---|---|
| procurement | Vehicles, payments ledger, cancel dispatch; `procurement_loads` read-only history; Load wise unions journeys with historic loads. |
| animalpurchase | Inspection feeds journey; rule 7 amended (tagging at vendor). |
| tasks / sop / sopbridge | Cancel-stage and re-anchor commands; G4, G5; studio shows stage graph read-only. |
| kernelstages | G6 lateness stage and escalation ladder. |
| outbox / domainconsumer / eventwiring | About 11 new events in the registry, replay/DLQ coverage, subscriber-count tests. |
| identity / tagging / RFID | `in_procurement` lifecycle; exit reasons for journey cancel and transit death. |
| movement / locations | `place_in_pen` through the canonical partition composer; placement undo. |
| health | Transit deaths through the existing death workflow; `tell_health` opens a health case. |
| vaccination / obligation / vaccinationexecution | Journey vaccine and medicine steps record via vaccinationexecution with source `procurement_journey`; PC handoff reads them before scheduling. |
| pccare | Handoff consumer; loading injection capture videos. |
| feed / inventory | Journey feed is an inventory issue line owned by feed (feed, tubs, tarps, medicine); undo is a return-to-stock line. Never edits frozen dispatch or packing rows (`feed-dispatch-gate.md:8-23`). |
| weighing | Stock weighing drives money (blind verifier is a later improvement). |
| counts | Arrival reconcile hook. |
| penroutines | No double feed tasks during park warm-up. |
| sales / loadwise / landed cost / growth | Journey writes landed-cost lines (Purchase, Transport, Labour, Transit Feed) at `place_in_pen` per `load-landed-cost-and-growth.md:26-33`; parity test on J-27. |
| notification | Catalog rows `procurement.transit_check_missed`, `.prep_late`, `.milestone_overdue`, `.journey_blocked` with clear copy; journey-assignee audience; delivery through the kernel-worker dispatcher. |
| permissions / per-person page access / proof routes | Proof routes accept `procurement.journey.execute` or a `JourneyAssignee` grant; grant expires on arrival or cancel and is revoked by compensation; journey pages are catalog rows checked against nav. |
| workforce / HRMS | Lateness never becomes an HR finding. |
| calendar | Journey steps project as tasks with their `available_at`. |
| adminui Work Board | Step rows stay in the generic lane; the G8 journey row is a header, never counted as work. |
| ceoai / ask-mesha | `ceo_ai.procurement_journeys`, `ceo_ai.procurement_journey_events`, `ceo_ai.procurement_payments` in the data map and coverage matrix. Temporal history is never an AI or report source; core code never reads `ceo_ai.*` (`ceo-ai-reporting-boundary.md:20-22`). |
| verification | G7 later; follows `proof-business-ack-contract.md`. |

Untouched: shiftingsop, toxin, breeding, genetics, market.

**Amends:** `operational-task-kernel-non-deviation.md` (K1), `animal-purchases.md` (rule 7),
`schema-and-system-design.html:77` ("No Temporal/Redpanda/n8n": add "except Temporal, scoped
to `procurement_journey`"),
`feed-dispatch-gate.md` (journey feed as issue lines), `work-board.md`,
`load-landed-cost-and-growth.md`, `per-person-page-access.md`.
**Cites:** `task-timing-alerting-violations-and-appeals.md`, `notification-delivery.md`,
`ceo-ai-reporting-boundary.md`, `proof-capture-authorization.md`, `pc-care-module.md`,
`go-backend-stack.md` (this is the later ADR that extends the stack with `go.temporal.io/sdk`).

## Testing, observability, retention

**Testing**

- `Advance` table tests in pure Go.
- Temporal test suite for races, signals and continue-as-new.
- **Replay tests in `make check`:** `WorkflowReplayer` over golden histories in
  `adapters/temporal/testdata/histories/` (J-27 happy path, rejection with compensation,
  breakdown, migrate). Any workflow change must add a history. Stg exports one closed history
  weekly.
- Whole-journey E2E `TestKernelStory_ProcurementJourney` against Postgres and a Temporal dev
  server (`temporal server start-dev` locally and in CI).

**Observability**

- Search attributes: `Stage`, `Status`, `TenantID`, `TemplateVersion`.
- SDK metrics to the mesha-ops Grafana; one trace ID from outbox to signal to activity.
- Alerts: schedule-to-start latency, workflow task failures (non-determinism), signaler backlog,
  `compensation_stuck`, `engine_lag`.

**Retention:** the projection timeline is the audit record. The namespace keeps histories 30 days
after close.

## Alternatives rejected

| option | why not |
|---|---|
| Pub/Sub choreography only (G1 design) | No "where is it stuck", no exception owner, no ordered proven undo. Kept for broadcasting. |
| Grow `tasks` and kernel sweepers | Rebuilds durable state, signals, compensation and replay by hand inside the shared kernel. |
| n8n | Licence limits, no compensation model, outside the Go binary, guards and CI. |
| Camunda / BPMN | JVM runtime and a second authoring surface. |
| DBOS, Restate, Inngest | Viable; one engine is better than several. **DBOS is the fallback** if Temporal hosting fails, as it runs on Postgres in-process; the `JourneyEngine` port keeps the swap in one adapter. |
| AI agent framework owning state | Non-deterministic, not replayable, cannot approve. |

## Consequences

- One place answers "where is this load and why".
- Exceptions, undo and forward recovery are authored spec, tested in Go and by replay.
- `go.temporal.io/sdk` becomes a dependency; a Temporal service runs in every environment that
  runs journeys; a journey worker is wired in `cmd/`.
- Owning modules build the missing undo commands before the stage that needs them.
- Tasks scope shrinks: G1 and G3 move out; G2 becomes `available_at` from anchors; G4, G5, G6 stay.

## Risks

| risk | mitigation |
|---|---|
| Operating a new runtime | Single small deployment, Grafana via mesha-ops, hosting decision below. |
| Non-determinism | Pure `Advance`, determinism guard, replay tests. |
| Projection drift | Retries, reconciliation stage, visible `updated_at`. |
| Two task authorities | Core rule: kernel owns all human time; K1 amendment and non-deviation guard. |
| Undo commands land late | Publish refuses unregistered commands. |
| Cost on the free OCI box | Hosting question. |

## Delivery phases (aligned to `delivery-plan.md`)

| phase | slice | contents |
|---|---|---|
| A | 0 | This ADR and K1 amendment accepted; C1-C9 answered; hosting chosen. |
| B | replaces 1 | Module skeleton; spec, nodes, `Advance` tests; publish/validate; guards; Temporal adapter, signaler, `RecordJourneyEvent`; tasks port open/cancel/re-anchor; G4, G5 in tasks. |
| C | 2 | Requests and sourcing as SOP workflows; fix vendor starts the journey. |
| D | 3 | Stock verification, selection, load approval; rejection runs compensation. |
| E | 4 | G6 kernel lateness and ladder; transit series; Work Board source; exceptions. |
| F | 5-6 | Warm-up, prep, loading, transit; undo commands; replacement-truck and forward-recovery subflows; `in_procurement` goats. |
| G | 7 | Arrival, payments ledger, landed cost, PC handoff; whole-journey E2E. |
| H | 8 | Retire source-entry; cutover; AI suggestions. |
| I | 9 | G7 per-step review. |

## Conflicts with existing decisions (flagged, not overridden)

| # | existing doc says | this ADR says | proposed resolution |
|---|---|---|---|
| K1 | `operational-task-kernel-non-deviation.md`: no module keeps its own scheduler, clock rule or escalation ladder. | Temporal sequences stages; the kernel keeps all human time. | Add to that ADR's Decision: *"A module may run a module-scoped durable orchestrator (first: `procurement_journey` on Temporal) for sequencing, signal waits, branching, compensation, forward recovery and exception subflows. It opens every human step as a kernel task with a real owner and canonical clock fields, including future `available_at`; it holds no timer that decides when a person sees work or is late. Lateness, contact and escalation stay the kernel's."* Update its companion sources and the non-deviation guard. |
| K2 | `procurement-journey.md` Decision 1, G1: stages chain via `stage_completed` and `triggers.on_complete`. | Stages chain in the workflow; the event opens nothing. | Replace G1 for the journey; keep the event as a fact. |
| K3 | `configuration.md`: an SOP edit reaches the next stage opened. | Whole journey pinned at open. | Pin per journey; change the row to "journeys opened after the publish". Running journeys change only via `MigrateTemplate`. |
| K4 | `configuration.md`: one profile per purpose. | No version picker; per-load adjustment within the farm range. | Confirm min/max per profile number. |
| K5 | G6 and slice 4: generic lateness stage. | Same. | No conflict. |
| K6 | `data-model.md`: status written by hooks. | Written by `RecordJourneyEvent`. | Accept the workflow as the one writer. |
| K7 | Decision 9: `procurement_loads` stays history. | Same. | No rewrite of `procurement_loads`. |
| K8 | `delivery-plan.md` puts G1-G6 in `tasks`. | G1, G3 move; G2 becomes `available_at`. | Replan slices 1 and 4 as above. |
| K9 | Owner list in `procurement-journey.md`. | `procurement_journey` owns `procurement_journeys`, `procurement_journey_events`, template versions; procurement keeps vehicles, ledger, loads. | Open question 5. |

## Open questions

1. **Hosting:** self-hosted Temporal (where, on which Postgres, within OCI free tier and GCP
   budget) or Temporal Cloud.
2. **`cel-go` placement:** default is evaluation behind `ports.RuleEvaluator` in
   `adapters/cel/`, with `domain/` keeping only the parsed rule model. Maintainers may instead
   allow `cel-go` in `domain/`.
3. **Who decides each animal:** Health Director alone, or Health Director with the CXO as today.
4. **Warm-up lengths:** confirm about 14 days for fattening and about 42 for breeding, plus park
   warm-up days.
5. **K9 table ownership:** confirm the split above.
