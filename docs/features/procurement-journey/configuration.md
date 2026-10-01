# Procurement journey: everything configurable, and where it lives

"Everything should be always configurable." This file is the complete list. If a value the
farm might want to change is not in one of the four homes below, that is a defect in the design,
not a shortcut.

## The four homes

| home | what goes there | edited on | changes take effect |
|---|---|---|---|
| **SOP documents** (`sop_versions.form_dsl`) | steps, their order, who does each, proof counts, questions and branches, schedules, instructions, the request form, the NDA/terms checklist, the truck checklist | Procurement › Procurement SOP (List \| Flow) | journeys opened after the publish; a running journey keeps everything it started with (profile snapshot + every stage SOP version pinned at journey open) |
| **Journey profiles** (`procurement_journey_profiles`) | the numbers and references per kind of purchase: durations, medicines, feeds, milestone template, check intervals | Configuration › Items & settings › Procurement journey profiles | the NEXT journey fixed; an open journey keeps its snapshot |
| **Registers** (existing catalogs) | purposes, species/sex/breed, vendors, vendor record types, parks and pens, vaccines, medicines, feed items, designations, people | the register's own page (Configuration › Items, People, Vendors, …) | immediately for new picks; stored picks keep their reference |
| **Notification audiences** (`notification_alert_audiences`) | who hears each push | People / HRMS → Notifications | immediately |

Nothing is a fifth place. In particular there is **no `/procurement/config` page**: the IA guard
allows exactly three Config entries (feed, health, sales) and adding a fourth needs its own
recorded decision; the profile editor is a Configuration › Items & settings register, which is
where "numbers decided up front" already live for other modules.

## Journey profile (one per purpose)

| key | type | seeded default | used by |
|---|---|---|---|
| `purpose` | choice code (`fattening`, `breeding`, `non_breeding`, …) | the four purposes `procurement_load_goats.purpose` knows | request form, profile selection |
| `label` | text | "Fattening sheep" | request chip, journey header |
| `source_warmup_days` | int, 0–90 | fattening 14, breeding 42 | stage 4 length, earliest dispatch |
| `park_warmup_days` | int, 0–90 | 14 (the glossary's park warm-up) | stage 8 series, management stage switch |
| `journey_feed_days` | int, 1–10 | 3 | stage 5 pack step title |
| `transport_prep_days` | int, 1–14 | 5 | stage 5 anchors ("before intended date of dispatch") |
| `transit_check_hours` | int, 1–12 | 3 | stage 7 series |
| `transit_stop_hours` | int, 2–24 | 6 | stage 7 stop series |
| `transit_check_grace_minutes` | int | 30 | late marking + missed-check push |
| `warmup_vaccine` | reference → vaccine register row | ET+TT | stage 4 step title and the administer record |
| `warmup_medicines[]` | references → medicine register | — | extra administer steps if the document names them |
| `warmup_feed` | reference → `feed_item_catalog` | warm-up concentrate | stage 4 daily step |
| `loading_medicine` | reference → medicine register | the loading injection the board calls "chocolate" | stage 6 step title |
| `warmup_stage_by_sex` | map sex → management stage | male → `Fattening Male Warmup` / `Warmup Buck`, female → … | the goats row at tagging |
| `payment_milestones[]` | ordered {label, percent, due_after_stage} | 10% on fixing, 20% after selection, 60% on departure, 10% after arrival | stage 9 seed |
| `price_unit_default` | `per_kg_live` or `per_animal` | per_kg_live | request form default |
| `override_ranges` | per numeric key {min, max} the director may override at fix time | ±50% | fix screen validation |

A profile is versioned the way protocols are (draft → publish → retire), because a journey pins a
snapshot and the farm must be able to read what a 2026 journey ran on. `profile_snapshot` on the
journey is the resolved values plus the override the director typed.

## What the SOP documents resolve at open

A step title, detail or schedule may reference `{profile.<key>}`, `{journey.<field>}`
(`agreed_count`, `accepted_count`, `destination_pen`, `planned_dispatch_on`, `vendor_name`) and
`{milestone.<field>}`. The compiler substitutes at open from the snapshot and refuses at publish a
reference the profile schema does not have (`unknown_profile_key`), so a typo cannot ship a title
reading `{profile.warmup_vacine}` to a phone.

## What is deliberately NOT configurable

- **Which stage opens which** (the chain in `journey-map.md`). Order is the engine's (the
  `procurement_journey` Temporal workflow, see `docs/decisions/procurement-journey-orchestration-engine.md`); the
  farm authors the steps inside a stage, and may delete a whole stage's steps down to the engine
  hooks, but cannot put transit before loading.
- **The engine-hook steps** (`vendor_fixed`, `load_approval`, `journey_tag_animals`,
  `departure_recorded`, `arrival_recorded`, `arrival_reconcile`, `place_in_pen`, `pc_handoff`,
  `payment_milestone`, `animal_purchase_decision`). They are the facts the system records on its
  own; a version that drops one is refused at publish (`engine_step_removed`, the existing rule).
- **"Every transit has a transit manager."** The transit document must contain two `pick_person`
  steps, the riding AM (with the load) and the transit manager (at the park or main office), and
  every other step in it must be owned by one of them (`transit_roles_required` at publish).
- **The free-flow shape of stock weighing** (temp tag + weight, no roster, no gate). Weighing at
  the vendor is procurement's own table, not the Weighing module's; it knows nothing about
  `goats`.
- **The money arithmetic** (milestone amount = percent × agreed value at that moment; landed cost
  = ledger + lines). The percentages are configurable; the formula is not.
- **The per-animal inspection questions' TYPED ids** (`weight_kg`, `field_verdict`, …), as
  `procurement-sop-driven.md` already records.

## Where each whiteboard item lands

| whiteboard | home |
|---|---|
| ₹400/kg live; 75 male sheep; 15–17 kg; in a week | request form (stage 0) |
| NDA & terms | fix-vendor checklist, authored on `procurement.sourcing` |
| Procurement Director final call | `fix_vendor` owner |
| Vendor stock verification — individual weights in the evening | stage 2, `weigh_stock` |
| Selection — fill health SOP only for non straight rejects | stage 2 `sight_removed` + stage 3 `inspect_candidates` |
| Health SOP verification ⇒ Health Director | `animal_purchase_decision` owner |
| Load approval ⇒ Procurement Director with COO | `load_approval` owners |
| Warm-up SOP starts | stage 4 + profile `source_warmup_days`, `warmup_vaccine`, `warmup_feed` |
| Before intended date of dispatch: truck + its SOP, labour, responsible AM | stage 5, anchored on `planned_dispatch_on` |
| Loading: injection, feed pack, tubs, tarps | stage 6 + profile `loading_medicine`, `journey_feed_days` |
| Route planning by Procurement Director | stage 5 `route_plan` |
| Send a responsible AM (rides with the load) | `pick_riding_am` |
| Every transit has a transit manager (at the park or main office, in contact with the AM) | `pick_transit_manager` + publish rule |
| Video every three hours; where to stop | stage 7 series + profile `transit_check_hours`, `transit_stop_hours` |
| Payments 10% / 20% at each step | profile milestone template, editable per journey at fix |
| Two weeks fattening, six weeks breeding | profile per purpose |
