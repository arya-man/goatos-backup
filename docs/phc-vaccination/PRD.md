# PHC → Vaccination — Product Requirements (PRD)

**Status:** Draft v2 · **Date:** 2026-06-22
**Source of truth:** `/Users/ravi/mesha/wiki` (goatOS handbook, PHC/Health Director handbooks) + the product mock.
**Foundation:** [Generic Protocol & Obligation Engine](../protocol-engine/obligation-engine.md) — vaccination is the first module on a shared engine, not a one-off.
**Explicitly NOT a source:** the older `goatos/context/*` and `goatos/docs/phases/*` planning docs (scrapped new-dashboard effort).

---

## 1. Why this exists

Goat OS is a **config/ruleset-driven operations system**. Leadership sets the rules; the system turns every field event into the right downstream work automatically.

> **The promise:** Admin publishes a rule once → every goat CRUD fires the cascade. Add a goat → its obligations generate. A due date arrives → a shed-drive task appears for the right worker. A dose is recorded → stock is consumed via the inventory ledger (FEFO), coverage updates, the booster is scheduled, anomalies surface. Nobody maintains a spreadsheet of who-needs-what-when.

## 2. PHC is a vertical — vaccination is module #1

PHC (Preventive Health Care) is **not** just vaccination. The config engine must serve all of these — built on **one** obligation engine so they don't each reinvent rules/tasks/proof:

| PHC modules (all reuse the engine) |
|---|
| **Vaccination** ← build this week · Deworming · Biosecurity / quarantine · Feed & water testing · Panel cleaning · Shed sanitization · Fire / safety checks · SOP-video verification · Stock anti-misuse / PHC inventory checks |

> **Director reporting is cross-cutting**, not a PHC module — it is an org-wide
> reporting cadence (daily/weekly EOD/rollups) that spans every vertical. Do not
> treat it as PHC/Vaccination scope.

**This week's build = Vaccination only**, but the config shape (`protocol_definitions.category`) is generic so deworming/biosecurity/sanitization slot in later with zero engine changes. (Feed Direction is a module of the **separate Feed vertical** — on the same engine; "parks" is the scope dimension, not a vertical; see [feed-direction/PRD.md](../feed-direction/PRD.md).)

### Scope

| In scope (v1) | Fast-follow (P2) | Out of scope |
|---|---|---|
| Goats-config delta (identity the cascade reads) | Deworming + FAMACHA-driven dosing | Breeding / milking / growth verticals |
| Vaccination rules as `protocol_*` + admin UI | Vaccination coverage projection | Procurement intake saga |
| Auto obligation generation on birth/procurement | Cold-chain excursion + quarantine | Full analytics warehouse / Cube |
| Per-shed drives + SOP execution + proof video | Booster-chain interrupt policy | Other PHC modules (engine-ready, not built) |
| Generic inventory + FEFO ledger movements | Withdrawal-period sale-block automation | |
| Lifecycle cleanup (shift/death/sale) | | |

## 3. Actors & authority (corrected)

Role = **Vertical × Tier**, park-scoped (committed `user_scope_grants`: roles `admin/park_head/operator/verifier/ceo_internal`).

| Actor | Does | Authority |
|---|---|---|
| **PHC Director** | **Drafts / proposes** the vaccine ruleset (`protocol_version` status=`draft`) | `protocol.draft.vaccination` capability (category-specific, scoped tenant/park) |
| **COO / CEO** | **Approves & publishes** the rule — published version is **immutable + effective-dated** | `protocol.publish.vaccination` capability (category-specific) |
| **Park Head / Manager** | Sees their park's drives & overdue; assigns/approves | scoped |
| **Health worker (field)** | Executes the drive via SOP: scan, administer, record dose, upload proof | `vaccination.execute` |
| **Verifier** | Reviews proof submissions | `proof.verify` |
| **System (engine)** | Generates obligations, batches drives, fires reminders, moves stock via the inventory ledger, surfaces anomalies — **never invents rules** | — |

**Correction from v1:** it is **not** "PHC Director sets config." PHC Director **drafts**, COO/CEO **publishes**. A rule change = a new version, never an in-place edit of a published one.

## 4. Core user flows

### 4.1 Admin authors the rule → leadership publishes
PHC Director drafts a protocol rule (`protocol_rules` under a `vaccination` protocol):
> *Enterotoxaemia · goat · all sexes · shed-stage K1 · primary dose 1 · 0.5 ml · trigger age_based day 21 · window 7d · booster +14d.*
COO/CEO reviews and **publishes** → version becomes immutable with an `effective_from`. The engine reads the published version at generation time. No code ships.

### 4.2 Goat enters → obligations auto-generate
Birth report (`origin_type=birth`) or procurement (`origin_type=procured`) creates the goat. The engine reads published vaccination rules matching the goat's `sex × shed-stage × dose sequence` and **materializes `obligation_instances`** (one per due dose), `scheduled_date` computed from the trigger.

### 4.3 Due → shed drive appears (the work unit)
The sweeper batches due per-goat obligations into a **per-shed drive** (`sop_task`) and assigns it via `vaccination.execute` capability. Workers act on drives (hundreds of goats), not per-goat tickets — the scale lever.

### 4.4 Field worker executes (SOP + proof)
Worker runs the vaccination SOP (scan → administer → record dose → upload shed + vial video → verify quantity). Per-goat completion recorded; verifier reviews.

### 4.5 Completion → cascade
Per `vaccination_completion`: obligation → `completed`; **FEFO inventory** consumed by writing `consume`/`release` rows to `inventory_stock_movements` (balance updates from the ledger in the same txn — no direct decrement); booster scheduled from *actual* administration date; coverage/overdue refresh.

### 4.6 Edge cases (must handle)
Shift (re-target + re-eval same vaccine), death/sale (cancel pending in same txn), missed vs blocked (distinct), double-submit (idempotent), stock-out (reserve-at-start, no negative). Detail in [TRD §6](./TRD.md).

## 5. Surfaces (per the mock)

| Surface | Shows | Nav location |
|---|---|---|
| **Control Tower** | Live coverage %, drives due today, overdue, stock-low/expiring, breaches — per-park | top |
| **Action Center** | Worker queue: drives, catch-ups, verifications | top |
| **Vaccination** (module detail) | Config editor (draft/publish), schedule calendar, drive list, passport history, stock by lot | **under PHC** (moved from Health) |
| **Goat Passport** | One goat's full vaccination record + next due | per-goat |

**Mock nav correction:** Vaccination lives under **PHC**, not Health.

## 6. Success metrics
Coverage % within window (per vaccine/park) · on-time drive rate · stock integrity (zero negative, zero expired-lot use) · **zero ghost-overdue** (dead/sold never overdue) · engine latency (obligation generated promptly after goat CRUD).

## 7. Open questions (need PHC/vet input — not schema)
1. **Schedule values** — vaccine × age × interval roster to seed `protocol_rules` is **not** in the wiki (structure only). Need PHC protocol (legacy Slack `Vaccines.config`).
2. **K2 age band** — 45 (mock) vs 42 (SOP). Reconcile; make it config on `shed_profiles`.
3. **Vaccine naming drift** — canonical roster (Enterotox/Enterotoxaemia, Deworm/Dewormer).
