# The procurement journey: from a CXO's ask to animals in a pen (proposal 2026-10-01)

Status: **PROPOSED, awaiting maintainer confirmation** on the conflicts in the last section.
Nothing here is built. Owner once accepted: tasks + sop + animalpurchase + procurement +
notificationbridge + adminui + admin-web + Android.

Companion documents (the build spec) live in `docs/features/procurement-journey/`:

| document | what it holds |
|---|---|
| `README.md` | index and reading order |
| `journey-map.md` | every stage, every step, who does it, what proof, what gates the next stage |
| `configuration.md` | everything the farm can change without a deploy, and where |
| `data-model.md` | tables, events, engine hooks, routes, permissions |
| `views.md` | every web and phone screen, state by state |
| `engine-gaps.md` | what the tasks/SOP engine cannot do today and must learn |
| `delivery-plan.md` | the slices, in order, with tests, guards and proof per slice |

## What was asked (maintainer, 2026-10-01, whiteboard + voice brief)

"Procurement is a very hectic, long, tedious set of interlinked processes and tasks assigned to
multiple people, with multiple configurations." The journey, as described:

1. A **CXO raises a request**: X animals, in a price range, at a live weight, by a deadline.
2. It reaches the **Procurement Director and Procurement Manager**. The manager calls vendors
   and notes what each one says on one page, then **fixes one vendor**. NDA and terms are part
   of fixing a vendor. The Procurement Director has the final call (whiteboard: "₹400/kg live;
   75 male sheep; 15-17 kg; in a week's time → Procurement Director final call").
3. The vendor brings **75 + X** animals, because some will be rejected. The Procurement
   Director or Manager goes to the vendor's farm and **weighs every animal in the evening**.
   Some are removed on sight and never enter the app; some are removed after weighing.
4. **Next day: selection.** The inspection questions the app already has are answered, with
   photos, only for animals that were not straight rejects. The **Health Director** decides,
   per animal, take or leave.
5. A **load summary** goes to the CXO / Procurement Director: asked for, brought, removed on
   sight, removed at weighing, rejected by health, accepted, and what it will cost. They
   **accept or reject the load** (whiteboard: "Load approval ⇒ Procurement Director with COO").
6. On acceptance the **warm-up SOP starts at the vendor**: for example an ET+TT injection and
   a defined feed. Vaccine, medicine and feed are configurable. How many days of warm-up is
   decided up front, per kind of purchase (fattening about two weeks; breeding six weeks).
7. When warm-up completes, **transport**: before the intended dispatch date, arrange the truck
   and complete its SOP, arrange labour, send a responsible Assistant Manager from the park who
   rides with the animals. **"Every transit has a transit manager."** Route planning is the
   Procurement Director's.
8. **Loading day**: tag the animals, give the loading injection, load the animals, load feed
   for the journey (days configured), tubs and tarps.
9. **In transit**: a fixed set of things to do, for example a video every three hours; a
   transport SOP for where and how to stop for feed and water.
10. **After arrival** a further configured number of warm-up days at the park.
11. **Payments** at each step, configured when the vendor is fixed (for example 10% now, 20%
    after selection), with "paid or not" visible in the app.

"Everything should be always configurable."

## What exists today, and why the journey cannot simply be bolted on

The repo holds **two purchase-load systems that do not know each other**:

- **`procurement_loads` / `procurement_load_goats`** (the original source-entry slice, admin-web
  `/procurement/source-entry`): a state machine that already names `source_warmup`,
  `pre_dispatch_pending`, `dispatch_ready`, `in_transit`, `arrival_review`, `accepted_intake`,
  with `transit_handoffs`, `arrival_intake_reviews`, `source_holding_stays` and
  `procurement_pc_handoffs`. Its add-goat write creates a `goats` row. Every step is a hard-coded
  Go service method and route; nothing is SOP-authored; the Procurement Director is **denied every
  one of its routes** (`procurementDirectorStockOnlyRoute`). It has no phone surface.
- **`animal_purchase_loads` / `animal_purchase_candidates`** (2026-09-13, phone module `vendors`
  → Animal purchases): the vendor-side inspection the manager actually runs, authored on
  `/procurement/sops` (`procurement.animal_purchase`), with the intake workflow
  (`procurement.animal_purchase_intake`: record → loading photo → CXO decision → arrival video →
  place in pen → condition → tell health). By its own rule 7 it **stops at the decision**: an
  accepted animal is never written to `goats`, gets no RFID and joins no load.

Neither one is the journey. The first has the right states and no authoring, no owners, no phone
and no money; the second has authoring, owners and a phone and ends at the decision. The journey
below is built on the SECOND (it is what the farm uses) and takes from the first only what is
worth keeping: the state vocabulary, the arrival-count reconciliation and the PC handoff.

## Decision 1: the journey is ONE subject with STAGES, each stage an SOP-authored workflow

A **purchase journey** (`procurement_journeys`, one row per vendor load that was fixed) is the
subject every stage is keyed on. It is born when a vendor is fixed for a request and lives until
the last animal is in a pen and the last rupee is paid. The CXO's request is its own, earlier
subject (`procurement_requests`); one request may produce several journeys (two vendors), and a
journey always names its request.

Each stage is a **module SOP** (`kind = module`, `module_key = procurement`) authored on
**Procurement › Procurement SOP** with the same List | Flow editor the sale and the existing intake
use. Each is its own document so the farm can publish warm-up changes without touching transport:

| stage | SOP code | subject | opened by |
|---|---|---|---|
| 0 Request | `procurement.purchase_request` | request | `procurement.request.raised` |
| 1 Sourcing | `procurement.sourcing` | request | same event, second track |
| 2 Stock verification | `procurement.stock_verification` | journey | `procurement.journey.opened` (vendor fixed) |
| 3 Selection and load approval | `procurement.selection` | journey | stock verification completed |
| 4 Warm-up at source | `procurement.source_warmup` | journey | load approved |
| 5 Transport preparation | `procurement.transport_prep` | journey | load approved (runs beside warm-up, anchored to the planned dispatch date) |
| 6 Loading | `procurement.loading` | journey | transport prep completed AND warm-up completed |
| 7 Transit | `procurement.transit` | journey | departure recorded |
| 8 Arrival and park warm-up | `procurement.arrival` | journey | arrival recorded |
| 9 Payments | `procurement.payments` | journey | vendor fixed; milestones unlock as stages complete |

Stages chain through **domain events, never through a hidden scheduler**: a stage's completion
hook emits `procurement.journey.stage_completed{stage}`, and the opener for the next stage consumes
it, exactly as `sales.deal.recorded` opens the sale today. This is the `triggers` phase-2 item of
`docs/decisions/sop-driven-herd-operations.md`, built generically (see `engine-gaps.md` G1).

The existing `procurement.animal_purchase_intake` document is **retired into stages 3 and 8**:
its record/loading-photo/decision steps become the selection stage's seed, its arrival steps
become the arrival stage's seed. The inspection form (`procurement.animal_purchase`) stays exactly
where it is and keeps being the questions stage 3 asks per animal.

## Decision 2: who does what is authored per step; two owners are new kinds of owner

Every step carries `owner` as today (designation code, catalog-checked at publish). Two things the
journey needs that the engine does not have:

1. **A step owned by a PERSON chosen earlier in the journey.** "Send a responsible AM" picks a
   person; every transit step is then that person's. The engine gets `owner_from_step: <key>`
   (the key of a `pick_person` step) and a `pick_person` answer kind whose options are the people
   holding a named designation at the journey's park. "Every transit has a transit manager" is then
   a publish-time rule on `procurement.transit` (a pick step must exist and every step must name
   it), not a column nobody fills.
2. **Two designations may own one step.** "Procurement Director OR Procurement Manager" sources;
   "Procurement Director with COO" approves. `owner` becomes a list; the first to act completes
   it. An approval that must be taken by two people is two steps.

Designations used, all already in `designation_catalog`: `ceo_internal` (CXO), `procurement_director`,
`procurement_manager`, `health_director`, `park_head`, `am_farming` / `am_health` (the AM pool a
transit manager is picked from). **No new designation is created.** A transit manager is a person
on a journey, not a job title.

## Decision 3: the numbers the farm decides up front live in a journey PROFILE, not in a step

Durations and money are not steps, and the whiteboard says they are decided "at the very starting"
per kind of purchase. They are the **journey profile** (`procurement_journey_profiles`, one per
`purpose`: fattening, breeding, non_breeding, …), edited on **Configuration › Items & settings**:

- source warm-up days (fattening 14, breeding 42), park warm-up days after arrival,
- journey days to pack feed for, transit check interval (hours), the loading medicine and the
  warm-up medicines/vaccines (references into the existing vaccine and medicine registers),
- the **payment milestone template** (ordered list of {label, percent, due_after_stage}),
- the inspection SOP version policy (always latest published).

A journey **snapshots its profile when the vendor is fixed** (`procurement_journeys.profile_snapshot`
jsonb, the same pin the SOP version gets). Changing the profile changes the next journey, never one
in flight. The Procurement Director may override durations and the milestone list on the journey at
fix time, inside the profile's allowed range; the override is stored on the journey.

The SOP documents reference profile values by name (`{profile.source_warmup_days}`) in schedules and
step titles, so the warm-up document says "Day {n} of {profile.source_warmup_days}" and the engine
resolves it at open. That is the schedule-anchor work in `engine-gaps.md` G2.

## Decision 4: money is a ledger per journey, and a milestone is a step that reads it

`procurement_journey_payments` (mirrors `feed_purchase_payments`: paid_on, amount, note,
proof, recorded_by) holds what was actually paid. The payments SOP's milestone steps are
`payment_milestone` engine-hook steps: each completes when the ledger reaches the milestone's
amount, never by a tap ("Paid 10% — ₹30,000 of ₹30,000"). A milestone is *due* when its
`due_after_stage` completes. The landed-cost lines (`procurement_load_cost_lines`) keep their own
meaning (what the load cost) and are written by the journey at arrival from the ledger plus the
transport/labour/feed lines, so Load wise and Sales keep working with no change.

## Decision 5: the Health Director decides animals; the office approves the LOAD

Today the CXO decides each candidate on `/procurement/animal-purchases`. The whiteboard puts the
per-animal health call on the **Health Director** ("Health SOP verification ⇒ Health Dir") and the
load acceptance on the **Procurement Director with COO**. So:

- `animal_purchase_decision` (per animal) is re-owned to `health_director` and the candidate
  review page is offered to that role; the CXO keeps the floor.
- A new engine-hook step `load_approval` completes on `procurement.journey.load_approved`,
  written from a **load approval screen** that shows the funnel (asked / brought / removed on
  sight / removed at weighing / rejected by health / accepted) and the money (animal cost at the
  fixed rate, estimated landed cost from the profile). Owners: `procurement_director` +
  `ceo_internal`. Rejecting the load closes the journey (`rejected`), cancels its open stages,
  and releases the request back to sourcing.

This is a CHANGE to a shipped decision (`docs/decisions/animal-purchases.md` rule 6, the CXO
decides) and is listed under conflicts below.

## Decision 6: accepted animals BECOME goats at loading, in the Warmup stage

Rule 7 of `animal-purchases.md` ("stops at the decision; no goats row, no RFID") was right for a
stage that ended at the decision. The journey continues, and the tagging step on loading day is
where an animal gets its RFID and its `goats` row: species, sex, breed, weight from stock
verification, `management_stage` = the purpose's warm-up stage (`Fattening Male Warmup`, `Warmup
Buck`, …), `shed_id` = the destination pen chosen on the load approval, origin = the journey (so
`procurement_load_goats` is written too and Load wise / origin filter keep working). Until then a
candidate is a candidate. Death or loss in transit is recorded on the journey and closes the goat
through the existing death path with reason `in_transit`.

## Decision 7: transit is bounded work with a server clock, like the toxin waits

"Every three hours a video" is a `series` on the transit SOP anchored at the **departure step's
completion** and ending at the **arrival step's completion** (engine-gaps G3: `series.from_step` /
`until_step`). Each check is one step, due at its time, late when missed, and the transit manager's
screen is the list of checks with the next one at the top. Stops (feed/water) are steps the SOP
authors with their own instructions and proof. Nothing on the phone computes a gate from its own
clock; every due time is the server's.

## Decision 8: pushes are catalog rows; lateness is the kernel's, not a procurement timer

Every upward push (request raised → Procurement Director/Manager; vendor fixed → CXO; load
approval pending → Procurement Director + CXO; load approved → Park Head of the destination park;
dispatch tomorrow → transit manager; transit check missed → Procurement Director; arrived → Park
Head + Health Director; milestone due → Procurement Director) is a row in the designation audience
catalog (`notificationaudience/domain`), switchable on People / HRMS → Notifications. Overdue
steps surface on the Work Board under a new **procurement** source (the lane exists and is hidden
today) and on the Alerts page; a per-stage escalation ladder is the kernel's standing contact
waterfall item, not something this feature builds privately.

## Decision 9: the old source-entry pages are retired once the journey covers them

`/procurement/source-entry` and its eleven routes serve nobody the farm has (the Procurement
Director is denied them). When stages 4–8 ship, the source-entry page is removed and the tables
stay (history), with the arrival-review count reconciliation and the PC handoff re-homed as journey
engine hooks. Until then nothing touches it.

## Conflicts to surface before building (maintainer lock: ambiguity is not approval)

| # | existing rule / source | new instruction | question |
|---|---|---|---|
| C1 | `animal-purchases.md` rule 6: the CXO decides each candidate on the web; `AnimalPurchaseDecide` is CEO-only | whiteboard: Health Director verifies the health SOP per animal; load approval is Procurement Director with COO | Does the Health Director now hold the per-animal decision, with the CXO only approving the load? Or does the CXO keep per-animal and the Health Director adds a prior gate? |
| C2 | `animal-purchases.md` rule 7: accepted animals are not goats | journey continues to tagging, transport, pen | Confirm tagging day is the goats-row birth (Decision 6), or arrival day. |
| C3 | `procurement.animal_purchase_intake` seed (CXO decision → park head arrival steps) | stages 3 and 8 | Retire that document into the two stages, or keep it as a short-form path for small buys? |
| C4 | `procurementDirectorStockOnly` lens hard-codes the Procurement Director's pages; `per-person-page-access.md` says lenses are retired except the verifier's | the journey gives that role requests, journeys, approvals | Delete the lens and let `/people` ticks govern (the recorded direction), or keep it? |
| C5 | `load-landed-cost-and-growth.md`: a load's purchase value is landed cost from `procurement_load_cost_lines` | journey has a payment ledger and a profile-estimated landed cost | Ledger writes the cost lines at arrival (Decision 4); confirm no second cost truth. |
| C6 | `glossary.md`: source warm-up 4–5 weeks (V1 holding farm) | voice brief: fattening about two weeks, breeding six weeks | Profile defaults 14 / 42 days; confirm per purpose. |
| C7 | leadership-tasks: a director asks a CXO | a CXO asks procurement | A purchase request is NOT a leadership task (opposite direction, needs steps). Confirm it is its own record. |
| C8 | `operator` designation retired 2026-09-23; AM tiers are `am_feed/health/farming/cleaning` | "send a responsible AM" | Which AM tiers may be picked as transit manager? Proposed: any AM at the destination park. |
| C9 | the loading injection is called "chocolate injection" on the board | copy firewall | It is a configurable medicine from the register; the on-screen name is whatever the register row says. No step title hard-codes a medicine. |

Build order and what each slice proves are in `delivery-plan.md`. Nothing from stage 2 onward
starts until C1–C3 are answered.
