# Procurement journey: the stage-by-stage map

Companion to `docs/decisions/procurement-journey.md`. Every table below is a **seeded v1
document** the farm edits on Procurement › Procurement SOP; nothing in a table is a Go or Kotlin
constant. `{profile.*}` values come from the journey's snapshotted profile (`configuration.md`).

Legend for the *task type* column: existing registry keys are plain; **bold** keys are new rows
(`data-model.md` → task types). *Owner* is a designation code; `[pick:riding_am]` means
"the Assistant Manager picked in the `pick_riding_am` step"; `[pick:transit_manager]` the person picked in `pick_transit_manager` (at the park or main office).

```
CXO ask ──► Request ──► Sourcing ──► vendor fixed ──► Stock verification ──► Selection
                                        │                                         │
                                        └──► Payments (milestones) ◄──────────────┤ load approved
                                                                                  ▼
                                   Transport prep ◄──────────── Source warm-up ──┘
                                        │ (anchored to planned dispatch)   │
                                        └──────────► Loading ◄─────────────┘ (both done)
                                                        │ departure
                                                        ▼
                                                     Transit ──► Arrival ──► Park warm-up ──► PC handoff
```

## Stage 0 — Purchase request (`procurement.purchase_request`, subject = request)

The CXO's ask. Raised on web (`/procurement/requests` → New request) or phone (Procurement module →
Requests → +). The request form is **authored** (`form_dsl.request_form`, same engine as the load
form), seeded with:

| field | kind | required | notes |
|---|---|---|---|
| species | choice (goat / sheep) | yes | configured animal vocabulary |
| sex | choice | yes | |
| purpose | choice (profile list) | yes | selects the journey profile |
| quantity | number | yes | animals |
| live weight band | number range (kg) | yes | e.g. 15–17 |
| price band | number range (₹ per kg live) or ₹ per animal | yes | unit is a choice |
| destination park | choice (parks) | yes | |
| needed by | date | yes | the deadline on the card |
| breed preference | choice (breed catalog) | no | |
| notes | text | no | |

Raising writes `procurement_requests` and emits `procurement.request.raised`, which opens two
tracks. Track `purchase_request` (the request's own card):

| # | key | title | task type | owner | proof | schedule |
|---|---|---|---|---|---|---|
| 1 | acknowledge | Acknowledge this request | do_and_confirm | procurement_director, procurement_manager | — | immediately |
| 2 | sourcing_done | Vendor fixed | **vendor_fixed** (engine hook) | — | — | — |
| 3 | all_journeys_closed | Every load for this request has arrived or been closed | **request_fulfilled** (engine hook) | — | — | — |

A request is **fulfilled** when the sum of accepted animals across its journeys reaches the
quantity, or when the CXO closes it short with a reason.

## Stage 1 — Sourcing (`procurement.sourcing`, subject = request)

The manager's call sheet. "Whomever he calls, everything he notes on one page."

| # | key | title | task type | owner | proof | schedule |
|---|---|---|---|---|---|---|
| 1 | call_vendors | Call vendors and record each quote | **record_quotes** (opens the quote sheet; completes by hand when ≥1 quote exists) | procurement_manager, procurement_director | — | immediately |
| 2 | shortlist | Shortlist the vendors worth visiting | record_multiselect (options = this request's quotes) | procurement_manager | — | after call_vendors |
| 3 | fix_vendor | Fix the vendor | **fix_vendor** (opens the fix screen; engine-completed on `procurement.journey.opened`) | procurement_director | — | after shortlist |

The **quote sheet** (`procurement_vendor_quotes`) is a repeatable ledger, not a questionnaire
(the same reasoning as the landed-cost ledger): one row per call — vendor (register pick or new),
offered count, ₹/kg or ₹/animal, average weight, available from, warm-up possible at vendor (yes/no),
notes, voice note. A vendor not yet in the register is created from the sheet with `status =
negotiating`.

The **fix screen** writes the journey: the chosen quote, agreed count (75) and buffer (+X), agreed
rate, agreed weight band, **NDA and terms** (an authored checklist: NDA signed, payment terms,
rejection terms, transport responsibility — each a yes/no with a document attachment), the
**payment milestones** (pre-filled from the profile template, editable), **planned dispatch date**,
and the profile's durations (editable within range). Confirming emits
`procurement.journey.opened`, which completes `fix_vendor`, opens stages 2 and 9, and creates the
`animal_purchase_loads` row the phone inspection already uses (journey ↔ load is 1:1).

## Stage 2 — Stock verification (`procurement.stock_verification`, subject = journey)

At the vendor's farm. "Weigh all the animals in the evening."

| # | key | title | task type | owner | proof | schedule |
|---|---|---|---|---|---|---|
| 1 | reach_vendor | Reach the vendor's farm | do_and_confirm | procurement_director, procurement_manager | 1 photo | immediately |
| 2 | sight_removed | How many animals were removed on sight, before weighing? | record_number | same | — | after reach_vendor |
| 3 | weigh_stock | Weigh every remaining animal | **weigh_stock** (opens the stock weighing screen; completes by hand when the operator says "done") | same | — | after reach_vendor |
| 4 | weigh_removed | Animals removed after weighing | **mark_removed** (opens the removal pick; records count + reason per animal) | same | — | after weigh_stock |
| 5 | stock_verified | Stock verified | do_and_confirm | procurement_director | — | after weigh_removed |

The **stock weighing screen** is free-flow: temp tag (typed or a temporary RFID scan), weight, an
optional photo; one row per animal into `animal_purchase_candidates` with `stage = weighed`. An
animal removed at step 4 gets `field_verdict = removed_at_weighing` and never reaches selection.
The counts at steps 2 and 4 are what the load approval funnel shows.

## Stage 3 — Selection and load approval (`procurement.selection`, subject = journey)

Next day. The inspection form (`procurement.animal_purchase`, unchanged) is answered for every
weighed animal that was not removed.

| # | key | title | task type | owner | proof | schedule |
|---|---|---|---|---|---|---|
| 1 | inspect_animals | Inspect every animal still in the load | **inspect_candidates** (deep-links to the existing inspection flow; completes by hand when every weighed animal has an inspection or a removal) | procurement_manager, procurement_director | — | immediately |
| 2 | health_decision | Decision on every animal | animal_purchase_decision (engine hook, existing; re-owned) | health_director | — | — |
| 3 | pen_for_load | Which pen will receive this load? | record_select (options = destination park's pens from the partition catalog) | procurement_director | — | after health_decision |
| 4 | load_approval | Approve or reject this load | **load_approval** (engine hook; opens the approval screen) | procurement_director, ceo_internal | — | after pen_for_load |

The **load approval screen** (web drawer and phone) shows the funnel and the money and takes one
decision: approve (with an optional accepted-count override for animals the office removes) or
reject with a reason. Approving emits `procurement.journey.load_approved` → opens stages 4 and 5,
unlocks the milestone with `due_after_stage = selection`. Rejecting closes the journey and reopens
`fix_vendor` on the request (the sourcing track gets a new `fix_vendor` action with the reason on it).

## Stage 4 — Warm-up at source (`procurement.source_warmup`, subject = journey)

"Warm-up SOP starts." Seeded for `{profile.source_warmup_days}` days; the medicines and feed are
profile references so the document never names a product.

| # | key | title | task type | owner | proof | schedule |
|---|---|---|---|---|---|---|
| 1 | warmup_start | Confirm warm-up has started at the vendor | do_and_confirm | procurement_manager | 1 video | immediately |
| 1b | tag_animals | Tag every accepted animal | **journey_tag_animals** (engine hook; deep-links to the tagging screen; completes when every accepted candidate has an RFID) | procurement_manager, procurement_director | — | after warmup_start |
| 2 | warmup_vaccine | Give {profile.warmup_vaccine} to every animal | administer | procurement_manager | 1 video | day 1, 09:00 |
| 3 | warmup_feed_day_{n} | Day {n}: give {profile.warmup_feed} | administer | procurement_manager | 1 photo | series: daily, days = {profile.source_warmup_days} |
| 4 | warmup_check | Any animal sick or dead during warm-up? | record_yes_no | procurement_manager | — | day {profile.source_warmup_days} |
| 5 | warmup_losses | Record the animals lost or removed | mark_removed | procurement_manager | — | only if warmup_check = yes |
| 6 | warmup_done | Warm-up complete | do_and_confirm | procurement_director | — | after warmup_check |

Tagging happens here, at the vendor's place during warm-up and before loading (maintainer,
2026-10-01): every accepted animal gets its RFID on the first warm-up day, so the vaccine and feed
steps that follow are recorded against tagged animals and the loading day only loads. This is
where an accepted candidate becomes a goat (decision 6). `warmup_done` completing emits
`stage_completed{source_warmup}`. The dispatch date on the journey
is validated at fix time to be ≥ load approval + warm-up days, and the warm-up track's last day is
the earliest the loading stage can open.

## Stage 5 — Transport preparation (`procurement.transport_prep`, subject = journey)

Anchored to `journey.planned_dispatch_on` ("before intended date of dispatch").

| # | key | title | task type | owner | proof | schedule |
|---|---|---|---|---|---|---|
| 1 | route_plan | Plan the route (stops, overnight halt, water points) | record_text | procurement_director | — | dispatch − {profile.transport_prep_days} |
| 2 | arrange_truck | Arrange the truck | **arrange_vehicle** (opens the vehicle form: transporter vendor, vehicle number, driver name/phone, capacity, agreed charge) | procurement_manager | — | dispatch − {profile.transport_prep_days} |
| 3 | truck_sop | Truck checklist | authored checklist (bedding, partitions, ventilation, tarp, tubs, ramp) — each item a yes/no with a photo | procurement_manager | 1 photo per item | after arrange_truck |
| 4 | arrange_labour | Arrange loading labour | do_and_confirm | procurement_manager | — | dispatch − 2 days |
| 5 | pick_riding_am | Which Assistant Manager rides with this load? | **pick_person** (options = AMs at the destination park) | procurement_director, park_head | — | dispatch − 2 days |
| 6 | pick_transit_manager | Who is the transit manager for this load? | **pick_person** (options = people holding procurement_manager, procurement_director or park_head) | procurement_director | — | dispatch − 2 days |
| 7 | pack_journey_feed | Pack feed for {profile.journey_feed_days} days | do_and_confirm | park_head | 1 photo | dispatch − 1 day |
| 8 | riding_am_departs | The Assistant Manager has left for the vendor | do_and_confirm | [pick:riding_am] | — | dispatch − 1 day |
| 9 | prep_done | Transport ready | do_and_confirm | procurement_director | — | after all |

Two people, two roles (maintainer, 2026-10-01): the **riding AM** is with the load and does every
field step from tagging to arrival; the **transit manager** stays at the park or the main office,
is the one person in contact with the AM, and owns the monitoring steps in stage 7. Both are
picked per journey; neither is a new designation.

## Stage 6 — Loading (`procurement.loading`, subject = journey)

Opens when stages 4 and 5 are both complete (engine-gaps G1, closed by the `procurement_journey`
workflow, which waits on both stage signals; see the engine ADR).

| # | key | title | task type | owner | proof | schedule |
|---|---|---|---|---|---|---|
| 1 | tags_checked | Check every tag reads before loading | **scan_roll_call** (scan each RFID; completes when every tagged animal is scanned or marked removed) | [pick:riding_am], procurement_manager | — | immediately |
| 2 | loading_injection | Give {profile.loading_medicine} to every animal | administer | [pick:riding_am] | 1 video | after tags_checked |
| 3 | load_animals | Load the animals | video_record | [pick:riding_am] | 1 video | after loading_injection |
| 4 | load_feed_and_kit | Load feed, tubs and tarps | photo_record | [pick:riding_am] | 2 photos | after load_animals |
| 5 | loaded_count | How many animals are on the truck? | record_number | [pick:riding_am] | — | after load_animals |
| 6 | departure | Truck has left | **departure_recorded** (engine hook; the tap writes `departed_at` and the loaded count on the journey) | [pick:riding_am] | 1 photo | after loaded_count |

The animals are already tagged (stage 4); the roll call is a scan of every tag so the loaded list
is the tagged list. A loaded count lower than the accepted count forces a
`mark_removed` for the difference before `departure` can complete (publish rule on the document).

## Stage 7 — Transit (`procurement.transit`, subject = journey)

| # | key | title | task type | owner | proof | schedule |
|---|---|---|---|---|---|---|
| 1 | transit_check_{n} | Transit check {n} — how are the animals? | video_record + record_select (fine / some distress / need to stop) | [pick:riding_am] | 1 video | series: every {profile.transit_check_hours} h from departure until arrival |
| 2 | feed_water_stop | Stop for feed and water | do_and_confirm | [pick:riding_am] | 1 video | series: every {profile.transit_stop_hours} h from departure until arrival |
| 3 | transit_loss | Any animal down or dead? | record_yes_no | [pick:riding_am] | — | with each check (only if check = distress) |
| 4 | transit_loss_detail | Which animals | mark_removed (reason = in_transit) | [pick:riding_am] | 1 photo | only if transit_loss = yes |
| 5 | review_check_{n} | Review transit check {n} | record_select (all fine / called the AM / escalated) | [pick:transit_manager] | — | after transit_check_{n} |
| 6 | check_missed_{n} | Check {n} is overdue — contact the AM | do_and_confirm (opened by the kernel lateness ladder, G6) | [pick:transit_manager] | — | transit_check_{n} due + grace |
| 7 | distress_call | Decide what the AM should do (continue / stop and rest / divert to a vet) | record_select | [pick:transit_manager] | — | only if any transit_check = distress |
| 8 | arrival | Truck has reached the park | **arrival_recorded** (engine hook; writes `arrived_at`) | [pick:riding_am], park_head | 1 video | — |
| 9 | handover_confirmed | Transit manager confirms the handover to the Park Head | do_and_confirm | [pick:transit_manager] | — | after arrival |

The AM records; the transit manager watches. Every check the AM records opens a review step for
the transit manager, so the office has a step of its own for every three hours on the road, and a
check not recorded within `{profile.transit_check_grace_minutes}` of its due time mints
`check_missed_{n}` for the transit manager and pushes `procurement.transit_check_missed` to them
(the Procurement Director gets the second push if that step is itself late). Arrival ends both
series (no further checks or reviews are minted).

## Stage 8 — Arrival and park warm-up (`procurement.arrival`, subject = journey)

The existing intake's arrival half, plus the count reconciliation the old source-entry slice had
and the configured park warm-up.

| # | key | title | task type | owner | proof | schedule |
|---|---|---|---|---|---|---|
| 1 | unload | Unload the animals | video_record | park_head | 1 video | immediately |
| 2 | arrived_count | How many animals came off the truck? | record_number | park_head | — | after unload |
| 3 | reconcile | Reconcile loaded vs arrived | **arrival_reconcile** (engine hook: completes when arrived + losses = loaded; otherwise blocks with the gap and opens a mark_removed) | — | — | — |
| 4 | place_in_pen | Place the animals in {journey.destination_pen} | **place_in_pen** (engine hook; moves the goats rows into the pen, stage stays warm-up) | park_head | 1 photo | after reconcile |
| 5 | arrival_condition | Was any animal hurt or sick on arrival? | record_yes_no | park_head | — | after place_in_pen |
| 6 | tell_health | Tell the health team which animals arrived unwell | do_and_confirm | park_head | — | only if yes |
| 7 | park_warmup_day_{n} | Day {n}: park warm-up feed | administer | park_head | 1 photo | series: daily, days = {profile.park_warmup_days} |
| 8 | pc_handoff | Hand the load to Preventive Care | **pc_handoff** (engine hook: writes `procurement_pc_handoffs`, emits the event vaccination already consumes) | — | — | after the series |
| 9 | journey_closed | Journey complete | do_and_confirm | procurement_director | — | after pc_handoff and after the payments stage |

`place_in_pen` is where the arrival-day landed-cost lines are written from the ledger (decision 4).

## Stage 9 — Payments (`procurement.payments`, subject = journey)

Seeded from the journey's milestone list at open; one step per milestone.

| # | key | title | task type | owner | proof | schedule |
|---|---|---|---|---|---|---|
| n | milestone_{n} | Pay {milestone.label}: ₹{amount} ({percent}% of ₹{journey.agreed_value}) | **payment_milestone** (engine hook; completes when the ledger covers it) | procurement_director | receipt photo on the ledger entry | due when `due_after_stage` completes |
| last | final_settlement | All dues settled with the vendor | record_yes_no | procurement_director | — | after the last milestone |
| last+1 | settlement_note | What is outstanding and why | record_text | procurement_director | — | only if no |

The amount of a percentage milestone is computed against the **agreed value at that moment**
(accepted count × agreed rate × average verified weight for ₹/kg deals), so a milestone due after
selection reflects rejections. The card shows paid / due / outstanding; the ledger is edited on the
journey's Money tab.

## What completes a journey

`journey_closed` requires every other stage complete or skipped, the ledger settled (or the
settlement note recorded), and every accepted animal either placed in the pen or recorded as lost.
Completing emits `procurement.journey.closed`, which the request's `request_fulfilled` hook
consumes.

## Branches the seeds carry (answer-driven, all authorable)

- selection: `load_approval = rejected` → the journey closes and sourcing reopens.
- warm-up: `warmup_check = yes` → `warmup_losses`.
- transit: `transit_check = distress` → `transit_loss`; `= yes` → `transit_loss_detail`.
- arrival: `arrival_condition = yes` → `tell_health`.
- payments: `final_settlement = no` → `settlement_note`.
