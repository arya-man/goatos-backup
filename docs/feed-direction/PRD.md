# Feed → Feed Direction — Product Requirements (PRD)

**Status:** Draft v1 · **Date:** 2026-06-22
**Vertical:** Feed (NOT PHC). Feed is a vertical with many modules — Feed Direction is module #1 (later: Feed Stock/Loads, Wastage/Variance, Ration Library). **Parks is NOT part of this vertical** — "parks" is the *scope* dimension (single/multiple park selectable via Company-wide ↔ Park-wise), which applies to every vertical. **Source of truth:** wiki goatOS handbook §12 (Feed) + Feed Director handbook + mock.
**Foundation:** [Generic Protocol & Obligation Engine](../protocol-engine/obligation-engine.md) — Feed Direction is the second module on the same engine as [PHC Vaccination](../phc-vaccination/PRD.md). It is built this week alongside vaccination.

---

## 1. Why this exists

Same engine, different domain. Where vaccination answers *"which goat needs which dose when"*, feed direction answers *"how much of which feed each shed gets, each session, each day"* — and recomputes when goats move.

> **The promise:** Leadership publishes ration rules per goat-class. Every night the system computes each shed's feed plan from live headcounts, raises packing tasks for the right worker, consumes stock via the inventory ledger at packing, records consumption with proof — and **at 2 PM re-computes after the day's shiftings** so the plan tracks reality.

## 2. Scope

| In scope (v1) | Fast-follow (P2) | Out of scope |
|---|---|---|
| Feed config: ration patterns + energy requirements (draft→publish) | Wastage/variance analytics | Other Feed modules (fodder farm supply, procurement) |
| Session templates per park | Step-change ration auto-ramp tuning | Full feed cost accounting |
| **Next-day direction generation** (nightly) | Transport optimization | |
| **2 PM recompute after shiftings** | Water-quality integration | |
| Packing (SOP + video + **FEFO stock ledger: reserve→consume**) | | |
| Consumption recording (SOP + proof) | | |
| Escalation on missed/flagged | | |

## 3. Actors & authority

Same role model as PHC ([engine §2](../protocol-engine/obligation-engine.md)).

| Actor | Does | Authority |
|---|---|---|
| **Feed Director** | **Drafts** ration patterns + energy requirements + session templates | draft (`park_head`/`admin`) |
| **COO / CEO** | **Publishes** ration config (immutable, effective-dated) | `protocol.publish.feed_direction` capability (category-specific, scoped tenant or park) |
| **Park Head / Manager** | Sees packing/consumption status; assigns/approves | scoped |
| **Feed worker (field)** | Packs per direction, uploads video; records consumption | `feed.report` |
| **Verifier** | Fills packed/consumed quantity, flags discrepancy | `proof.verify` |
| **System** | Computes supply_planning → directions, raises tasks, moves stock via the inventory ledger, escalates | — |

## 4. Core flow (the reviewer's pipeline, grounded in wiki §12)

```
ration config (feed_pattern + feed_pattern_feeds + goat_class_energy_requirement)   [draft→publish]
  → nightly schedule trigger → supply_planning (per shed×breed×age×feed, from live headcounts)
  → feed_directions v1 (midnight, pushed through feed_session_templates)
  → 2 PM recompute after shiftings → supply_planning regenerated → feed_directions v2 (supersedes v1)
  → packing task (SOP, video) → FEFO stock ledger (reserve→consume rows in inventory_stock_movements)
  → distribution / consumption (SOP, 3 videos: quantity / distribution / water)
  → verifier fills packed & consumed qty, flags variance
  → transport consolidation → proof + escalation on missed/flagged
```

### Daily timeline (ops SLA — **confirm exact clock times with Feed Director**)
| Time | Step |
|---|---|
| Day-before / early morning | **Direction generation** → publish `feed_directions` v1 (no stock locked yet) |
| **2 PM** | **Recompute** after the day's shiftings → `feed_directions` v2 supersedes v1 |
| **~3 PM** | **Packing & distribution placement** → packing task starts → **stock reserved/consumed here** (not at generation) |
| **~9 AM & ~3 PM** *(if confirmed)* | **Feeding sessions** → consumption recording + proof |

Session count and exact clock times are **config** (`feed_session_templates` per park), not hardcoded — the times above are the handbook default pending Feed Director sign-off.

- **Direction generation = obligation generation.** Each `feed_direction` row (date × shed × session × feed) becomes a per-shed-per-session work unit (`obligation_instances` → `sop_task`). **Stock is reserved at packing, not at generation.**
- **2 PM recompute** is a re-run triggered by the day's shiftings (`location_event`), producing **version 2** directions that supersede version 1 — the wiki's explicit v1=midnight / v2=2PM model.
- **Stock moves at packing** (not at direction generation) — `feed_packing` status `packed` writes `consume`/`release` rows to `inventory_stock_movements` against the FEFO lot; `inventory_stock` balance updates from the ledger in the same txn.

## 5. Surfaces (per mock)

| Surface | Shows | Nav |
|---|---|---|
| **Control Tower** | Today's directions, packing/consumption status per park, stock-low, flagged variance | top |
| **Action Center** | Worker queue: packing tasks, consumption recording, verifications | top |
| **Feed Direction** (module detail) | Ration config (draft/publish), today's plan per shed, packing/consumption board, stock | **under the Feed vertical** (moved from Farms) |

**Mock nav correction:** Feed Direction lives under the **Feed** vertical, not Farms. (Parks = scope selector, not a nav vertical.)

## 6. Success metrics
Directions generated on time (midnight + 2 PM) · packing completion rate within session window · stock integrity (zero negative, FEFO honored) · variance (packed vs planned, consumed vs packed) within threshold · zero stale directions after a shift (v2 always reflects current headcount).

## 7. Open questions (Feed Director input)
1. **Ration values** — feed_pattern × feeds × quantities per goat-class are config; need the Feed protocol to seed (legacy Template sheet → `feed_session_templates`).
2. **Session count per park** — was hardcoded; now `feed_session_templates` per park (Morning/Afternoon/Evening?). Confirm per park.
3. **Wastage model** — wiki defers wastage tracking; confirm v1 records consumed-vs-packed variance only.
