# PHC → Vaccination — Product Requirements (PRD)

**Status:** Draft v2 · **Date:** 2026-06-22
**Source hierarchy:** committed GoatOS migrations and protocol-engine docs for
repo state; `context/source-findings/phc-vaccination-roster-stage-proposal.md`
and `context/source-findings/live-legacy-critical-guardrails-2026-06-28.md` for
accepted source findings; then `/Users/ravi/mesha/wiki` goatOS/PHC/Health
handbook material and the product mock where they do not conflict.
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
| Full vaccine-goat matrix as `protocol_*` config + admin UI | Vaccination coverage projection | Procurement intake saga |
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
> *Enterotoxaemia · goat · all sexes · shed-stage K1 · primary dose 1 · 0.5 ml · trigger birth_age day 21 · window 7d · booster +14d.*
COO/CEO reviews and **publishes** → version becomes immutable with an `effective_from`. The engine reads the published version at generation time. No code ships.

V1 is not complete with a generic "booster yes/no" form. It needs a practical
vaccine-goat matrix: for each vaccine and dose, the approved config must say
which goat types it applies to, at what age/stage, with what pregnancy/lactation
or health restrictions, what due window is safe, what repeat/booster rule
applies, what proof/SOP is required, and what source approval backs the row.
Purchased/intake goats and existing goats already in the database must be run
through the same matrix as farm-born goats.

### 4.2 Goat enters → obligations auto-generate
Birth report (`origin_type=birth`) or procurement (`origin_type=procured`) creates the goat. The engine reads published vaccination rules matching the goat's `sex × shed-stage × dose sequence` and **materializes `obligation_instances`** (one per due dose), `scheduled_date` computed from the trigger.

### 4.3 Due → shed drive appears (the work unit)
The sweeper batches due per-goat obligations into a **per-shed drive** (`sop_task`) and assigns it via `vaccination.execute` capability. Workers act on drives (hundreds of goats), not per-goat tickets — the scale lever.

### 4.4 Field worker executes (SOP + proof)
Worker runs the vaccination SOP: goat scan, administer, record vaccine, medicine
batch or vial/lot, dose, administered date/time, cold-chain/quantity checks
where required, proof media, adverse-reaction fields, and park-head/verifier
review. Per-goat completion recorded; verifier reviews.

### 4.5 Completion → cascade
Per `vaccination_completion`: obligation → `completed`; **FEFO inventory** consumed by writing `consume`/`release` rows to `inventory_stock_movements` (balance updates from the ledger in the same txn — no direct decrement); booster scheduled from *actual* administration date; coverage/overdue refresh.

### 4.6 Edge cases (must handle)
Shift (re-target + re-eval same vaccine), death/sale (cancel pending in same txn), missed vs blocked (distinct), double-submit (idempotent), stock-out (reserve-at-start, no negative). Detail in [TRD §6](./TRD.md).

### 4.7 V2 drive-planning algorithm
V1 must already answer: for every goat, what vaccine is due, by what date, in
which shed, and under which approved vaccine-goat matrix row. V2 starts after
that. It answers the operations question: when should the farm run a practical
drive, which goats go into it, and which vaccines can safely be done together.

The planner works like this:

1. Start from V1 due work generated from the completed vaccine-goat matrix. This
   includes new goat creation, purchased/intake goats, existing-goat backfill
   after a published rule, stage changes, shed shifts, trusted history
   suppression, and next-dose generation from accepted completions.
2. Convert one-goat rows into buckets such as:
   `park + shed + vaccine + dose + due-window + eligibility state`.
   This keeps one million goats manageable because the planner works on buckets
   and affected cohorts, not a full-herd recalculation every time.
3. Apply hard safety gates before scoring anything: lifecycle active, not
   dead/sold/transferred/lost/culled, health/defer state, pregnancy/lactation
   rule, quarantine/ICU rule, proof/SOP requirement, trained worker, stock,
   cold-chain, and vaccine compatibility.
4. Build a vaccine conflict graph for each shed/time window. Vaccines are nodes;
   unsafe same-day combinations or required 2-week/4-week gaps are edges. The
   planner separates unsafe combinations and keeps only safe vaccine groups.
5. Search candidate dates only inside the approved medical window
   (`earliest_safe_date`, `ideal_date`, `last_safe_date`). A date outside the
   safe window is rejected, not merely given a bad score.
6. Score the remaining safe plans by operational value: goats covered, urgency,
   disease priority, stock expiry, route/resource efficiency, and fairness to
   small sheds. A one-goat shed can be held if waiting is medically safe, but it
   becomes a micro-drive if waiting would break the window.
7. Create the drive with its goat list, vaccine list, lot/stock reservation,
   SOP/proof requirements, worker, verifier, and route. A route may contain
   multiple sheds, but each shed keeps its own goat list, proof, and
   reconciliation.
8. On execution day, reconcile the scan against the plan: missing goats,
   shifted-in goats, shifted-out goats, newly sick/pregnant/quarantined goats,
   unreadable tags, deaths, sales, proof rejection, and cold-chain failure all
   create explicit cancel/defer/rework/replan actions. Nothing silently
   disappears from the process.
9. Replan incrementally. If one goat dies, moves shed, becomes sick, gets sold,
   or a proof fails, only that goat and its affected shed/vaccine bucket are
   invalidated. The system does not recompute the full million-goat herd.

V2 is therefore a constraint-based shed-drive planner: per-goat due generation
plus cohort bucketing, vaccine conflict partitioning, bounded date search,
deterministic scoring, resource assignment, execution reconciliation, and
incremental replanning.

The split is deliberate: V1 owns the complete vaccine-goat matrix and per-goat
due generation. V2 consumes those due rows and the vaccine compatibility fields
to optimize shed drives. Do not treat V2 drive planning as a substitute for the
V1 matrix.

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

## 7. Source-derived baseline and remaining production inputs
1. **Local/dev schedule baseline selected** — use the source-derived ET row from
   §4.1 (`Enterotoxaemia`, K1, day 21, 0.5 ml, 7d window, +14d booster clue) for
   local/dev protocol proof; see
   `context/source-findings/phc-vaccination-roster-stage-proposal.md`.
2. **K2 age band** — use 42 days / six weeks for local/dev because the wiki,
   glossary, and legacy identity seed agree; the legacy dashboard's 45 is mock
   drift. Keep it data-driven on `animal_stage_lookup`.
3. **Production roster expansion** — PPR/FMD/HS/BQ are valid SOP/roster labels
   from source artifacts, but only ET has schedule/dose evidence in committed
   PRD text today. Add more schedule-bearing protocol rows when source extracts
   provide timing/dose/booster values.

## 8. Legacy capability parity, proof policy, and import mapping

Vaccination replaces legacy SOP/form behavior with GoatOS protocol, SOP, proof,
completion, inventory, and verification records. Capability parity means
preserve useful source signals and close legacy gaps; it does not mean copying
weak proof assumptions. Known legacy gaps to close: row/header existence cannot
count as dose proof, medicine batch/vial-lot cannot be optional, adverse
reactions need notes/follow-up, and verifier/park-head review must be durable.

| Legacy/source signal | GoatOS contract |
| --- | --- |
| SOP playground labels `PPR`, `ET`, `FMD`, `HS`, `BQ` | Keep as source-backed SOP/vocabulary labels. Only ET currently has schedule-bearing protocol evidence; labels alone do not generate obligations. |
| SOP proof fields: scheduled date, operator, goat scan, vaccine name, medicine batch, dose ml, administered date, proof photo/media, adverse reaction, verifier, notes | Normalize into `protocol_versions.rule_dsl.proof_policy`, `sop_versions.form_dsl`, `sop_submissions`, and `vaccination_completions`. Required first-slice fields are goat scan, vaccine, medicine batch/vial-lot, dose, administered date/time, proof media, adverse-reaction flag/notes, and verifier/park-head review. |
| Committed `000075` draft SOP skeleton (`shed_video`, `vial_lot`, `cold_chain`, `dose`, `route_site`, `administered_at`, `adverse_reaction`, `est_vs_used`, `verifier_review`) | Treat as the committed starting skeleton, not the final source contract. Upgrade the SOP version/proof policy to the source-normalized shape before calling vaccination SOP parity closed. |
| Procurement/legacy rows that mention vaccination | Treat as source evidence with confidence/proof semantics only. The live legacy guardrail found no reliable first-class vaccination evidence field in cleaned BigQuery tables, so a procurement row/header alone is not an administered dose. |
| Reliable historical vaccination record, if later proven | Import to staging, reconcile into `vaccination_completions`, mark matching obligations completed, and schedule boosters from the actual administered date. |
| Missing or untrusted history | Do not invent completions. After PHC approval, generate baseline/catch-up shed drives per `docs/protocol-engine/migration-and-cutover.md` instead of fabricating administered history. |
