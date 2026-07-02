# Preventive Care (PC) → Vaccination — Product Requirements (PRD)

**Status:** Draft v2 · **Date:** 2026-06-22
**Source hierarchy:** committed GoatOS migrations and protocol-engine docs for
repo state; `context/source-findings/preventive-care-vaccination-roster-stage-proposal.md`
and `context/source-findings/live-legacy-critical-guardrails-2026-06-28.md` for
accepted source findings; then `/Users/ravi/mesha/wiki` goatOS/Preventive Care/Health
handbook material, including the tracked vaccination nuance source
[source-nuances-rules.md](./source-nuances-rules.md), and the product mock
where they do not conflict.
**Foundation:** [Generic Protocol & Obligation Engine](../protocol-engine/obligation-engine.md) — vaccination is the first module on a shared engine, not a one-off.
**Explicitly NOT a source:** the older `goatos/context/*` and `goatos/docs/phases/*` planning docs (scrapped new-dashboard effort).

---

## 1. Why this exists

Goat OS is a **config/ruleset-driven operations system**. Leadership sets the rules; the system turns every field event into the right downstream work automatically.

> **The promise:** Admin publishes a rule once → every herd-animal CRUD fires the cascade. Add a goat or sheep → its obligations generate from the same ruleset. A due date arrives → a shed-drive task appears for the right worker. A dose is recorded → stock is consumed via the inventory ledger (FEFO), coverage updates, the booster is scheduled, anomalies surface. Nobody maintains a spreadsheet of who-needs-what-when.

## 2. Preventive Care (PC) is a vertical — vaccination is module #1

Preventive Care (PC) is **not** just vaccination. The config engine must serve all of these — built on **one** obligation engine so they don't each reinvent rules/tasks/proof:

| Preventive Care (PC) modules (all reuse the engine) |
|---|
| **Vaccination** ← build this week · Deworming · Biosecurity / quarantine · Feed & water testing · Panel cleaning · Shed sanitization · Fire / safety checks · SOP-video verification · Stock anti-misuse / Preventive Care (PC) inventory checks |

> **Director reporting is cross-cutting**, not a Preventive Care (PC) module — it is an org-wide
> reporting cadence (daily/weekly EOD/rollups) that spans every vertical. Do not
> treat it as Preventive Care (PC) / Vaccination scope.

**This week's build = Vaccination only**, but the config shape (`protocol_definitions.category`) is generic so deworming/biosecurity/sanitization slot in later with zero engine changes. (Feed Direction is a module of the **separate Feed vertical** — on the same engine; "parks" is the scope dimension, not a vertical; see [feed-direction/PRD.md](../feed-direction/PRD.md).)

### Scope

| In scope (v1) | Fast-follow (P2) | Out of scope |
|---|---|---|
| Herd-animal identity delta (animal facts the cascade reads) | Deworming + FAMACHA-driven dosing | Breeding / milking / growth verticals |
| Full vaccine-animal matrix as one governed `vaccination` ruleset per scope, with company default + park overrides | Vaccination coverage projection | Procurement intake saga |
| Auto obligation generation on birth/procurement | Cold-chain excursion + quarantine | Full analytics warehouse / Cube |
| Per-shed drives + SOP execution + proof video | Booster-chain interrupt policy | Other Preventive Care (PC) modules (engine-ready, not built) |
| Generic inventory + FEFO ledger movements | Withdrawal-period sale-block automation | |
| Lifecycle cleanup (shift/death/sale) | | |

### 2.1 Mixed-species herd foundation is in scope

V1 must not carry forward a goat-only storage model. GoatOS must model the
operational herd as mixed-species animals that can share the same park, shed,
partition, and shed tag. Legacy count data and the Goats and Parks source show
goat breeds and `Anantapur Sheep` co-located in the same shed/tag groups. The
rule matrix must therefore target **animal facts**, not a goat-only table.

Clean base contract:

| Concept | Product rule |
|---|---|
| Herd animal | Canonical target entity for vaccination, feed, counts, procurement, shifting, and passport/history. |
| Species | Required animal fact from a governed `species_catalog`; seed at least goat and sheep, and allow future species without DDL/code branches. |
| Breed | Belongs to exactly one species through governed breed/reference data and aliases. |
| Shed/tag | Shared operational cohort/location state. It is not species-specific; goats and sheep can sit in the same shed/tag. |
| Tag age policy | `animal_stage_lookup`/shed-tag policy stores source age range, normalized age days, purpose, and max-stay/transition policy where known. |
| Vaccination rule | Selector over `species + breed/breed group + shed_tag/stage + age days + sex + lifecycle/health/reproductive/procurement state`. |

Path B is the base direction: implementation must migrate canonical naming to
`herd_animals` / `animal_id` and API/domain language to Animal/HerdAnimal. The
old physical `goats` / `goat_id` names are legacy model debt and must not be
the target for this base correction.

## 3. Actors & authority (CEO/COO-only config)

Role = **Vertical × Tier**, park-scoped (committed `user_scope_grants`: roles `admin/park_head/operator/verifier/ceo_internal`).

| Actor | Does | Authority |
|---|---|---|
| **COO / CEO / superadmin** | Creates, edits, previews, and publishes the vaccine ruleset. Published version is immutable + effective-dated. | `protocol.publish.vaccination` capability (category-specific) plus top-level Config route access |
| **Preventive Care (PC) Director** | Sees effective operational instructions, coverage, exceptions, and escalations only. | No raw Config visibility in V1 unless the product owner later grants a separate read-only config capability |
| **Park Head / Manager** | Sees their park's drives & overdue; assigns/approves | scoped |
| **Health worker (field)** | Executes the drive via SOP: scan, administer, record dose, upload proof | `vaccination.execute` |
| **Verifier** | Reviews proof submissions | `proof.verify` |
| **System (engine)** | Generates obligations, batches drives, fires reminders, moves stock via the inventory ledger, surfaces anomalies — **never invents rules** | — |

**Correction:** it is **not** "Preventive Care (PC) Director drafts and CEO/COO reviews."
For V1, the Config screen is visible only to CEO/COO/superadmin. The durable
audit is normal protocol version audit: `version`, `created_by`/`created_at`,
`published_by`/`published_at`, `retired_by`/`retired_at`, and the generated
impact preview reviewed before publish. A rule change = a new version, never an
in-place edit of a published one.

## 4. Core user flows

### 4.1 CEO/COO authors the scoped vaccination ruleset → activates

Config is not a list of separate vaccine rules. Vaccination has one logical
ruleset family, for example `vaccination.matrix`, and each active version holds
the whole vaccine-animal matrix for one scope. The matrix includes ET+TT, PPR,
Goat Pox, Sheep Pox, Blue Tongue, FMD, HS, future rows, timing,
compatibility, proof policy, and exceptions together.

CEO/COO creates a draft protocol version (`protocol_versions.rule_dsl`,
expanded to `protocol_rules` under the `vaccination` ruleset family). Publishing
or activating sets `effective_from`; the version becomes immutable. A later
change creates a new version and makes the previous active version inactive for
that same scope. No code ships for a rule change.

V1 is not complete with a generic "booster yes/no" form. It needs a practical
vaccine-animal matrix: for each vaccine and dose, the approved config must say
which animal cohorts it applies to, at what species/breed/shed-tag/age/stage, with what pregnancy/lactation
or health restrictions, what due window is safe, what repeat/booster rule
applies, what proof/SOP is required, and which version/audit entry owns the
row. Source docs remain engineering evidence for the preset values; they are
not product UI columns or runtime approval fields.
Purchased/intake animals and existing animals already in the database must be
run through the same matrix as farm-born animals.

The matrix must also carry the vaccination nuance rules from
[source-nuances-rules.md](./source-nuances-rules.md): vaccine class and
pathogen class, post-procurement warm-up hold, live/killed spacing metadata,
same-day allowance metadata, quarantine/ICU/sick defer states, pregnancy and
post-delivery policy, and adult prior-vaccination policy. V1 enforces the
eligibility, defer, schedule, trusted-history, catch-up, compatibility
spacing, and Calendar drive-first parts. A later operations optimizer may use
the same V1-authored compatibility fields for route/resource planning, but it
does not own the medical rule matrix.

#### 4.1.1 Rule JSON is policy, herd facts are database facts

Do **not** store herd-animal required fields inside the rule JSON. The rule JSON
stores only authored policy: dimensions, matrix rows, vaccine cells, schedule
rows, compatibility/gap rules, and defer rules. Version/audit data lives on the
protocol version row, not inside herd-animal JSON. Herd-animal properties stay
in canonical tables or read models:

| Rule needs this dimension | Canonical/current fact source |
|---|---|
| Species, breed, sex, DOB/age, lifecycle, health, reproductive state | `herd_animals` plus typed lifecycle/reproductive deltas where current columns are not precise enough |
| Current park/shed/cohort and canonical shed tag/stage | `herd_animals.current_location_id` / `herd_animals.shed_id`, `locations`, `shed_profiles.animal_stage_id`, `animal_stage_lookup.stage_code` |
| Procurement path, herd-entry date, warm-up, trusted source vaccination history | `procurement_phc_handoffs`, `procurement_hf_vaccination_evidence`, animal `origin_type`/`entry_date` |
| Accepted vaccination history and booster anchor | `vaccination_completions` joined to `obligation_instances.rule_id` |
| UI impact preview at scale | indexed animal/protocol fact read models, not full-herd scans |

The common join key is always `tenant_id` plus the target identity
(`animal_id` for vaccination, `shed_id`/`cohort_id` for feed-style protocols) and
stable lookup IDs/codes (`breed_id`, `animal_stage_id`/`stage_code`,
`location_id`, `rule_id`, `protocol_version_id`). See
[Rule Matrix Authoring Handoff](./RULE-MATRIX-AUTHORING-HANDOFF.md) for the UI
prompt, persisted JSON sample, and target-facts/read-model contract.

#### 4.1.2 Company default, park override, and active-version resolution

Vaccination's V1 scope rule is deliberately stricter than the generic protocol
engine:

- At company scope, only one vaccination ruleset version can be active now.
- At park scope, only one vaccination ruleset version can be active now for a
  given park.
- A park active version overrides, and therefore excludes that park from, the
  company active version.
- Draft, scheduled-future, inactive, retired, and historical versions remain
  auditable, but they do not generate new obligations until activated.

Example:

| Active config | Applies to |
|---|---|
| Company vaccination v3 | Every park without its own active vaccination override |
| CBE park vaccination v1 | CBE only |
| HYD park vaccination v2 | HYD only |

In that example, company v3 is not disabled globally. It still applies to all
other parks, but CBE and HYD are excluded because they have park-specific active
versions. If the CBE override is retired, CBE inherits the company active
version again from the next generation/recompute run.

The Config list must therefore group by ruleset family and scope, not by
vaccine row. For `category=vaccination`, the first row is the company default
(`scope_type=tenant`, scope label "Company-wide"), followed by active park
overrides. Each row shows current active version, who activated it, activation
time, effective window, applies-to summary, excluded/overridden park count, and
history. The history drawer shows inactive and retired versions for that same
scope. A search result like "ET+TT all/all" may appear inside the matrix detail,
but it must not be its own top-level protocol row.

When a park override is activated, the impact preview must show:

- which open company-version obligations in that park will be superseded and
  regenerated under the park version;
- which in-progress batches are left to finish unless the operator explicitly
  cancels/reissues them;
- whether completed history stays attached to the version that produced it;
- how many animals/sheds now resolve to the park version instead of the company
  version.

This is the one point where I would counter a literal "auto-disable" wording:
the company version should not become inactive everywhere. It should become
non-applicable only for the park with an active override. Completed obligations
and accepted vaccination history should never be rewritten just because a new
scope override was activated.

### 4.2 Herd animal enters → obligations auto-generate
Birth report (`origin_type=birth`) or procurement (`origin_type=procured`) creates the herd animal. The engine reads published vaccination rules matching the animal's `species × breed × sex × shed-tag/stage × age × dose sequence` and **materializes `obligation_instances`** (one per due dose), `scheduled_date` computed from the trigger.

### 4.3 Due → shed drive appears (the work unit)
The sweeper batches due per-animal obligations into a **per-shed drive** (`sop_task`) and assigns it via `vaccination.execute` capability. Workers act on drives (hundreds of mixed-species animals), not per-animal tickets — the scale lever.

### 4.4 Field worker executes (SOP + proof)
Worker runs the vaccination SOP: animal scan, administer, record vaccine, medicine
batch or vial/lot, dose, administered date/time, cold-chain/quantity checks
where required, proof media, adverse-reaction fields, and park-head/verifier
review. Per-animal completion recorded; verifier reviews.

### 4.5 Completion → cascade
Per `vaccination_completion`: obligation → `completed`; **FEFO inventory** consumed by writing `consume`/`release` rows to `inventory_stock_movements` (balance updates from the ledger in the same txn — no direct decrement); booster scheduled from *actual* administration date; coverage/overdue refresh.

### 4.6 Edge cases (must handle)
Shift (re-target + re-eval same vaccine), death/sale (cancel pending in same txn), missed vs blocked (distinct), double-submit (idempotent), stock-out (reserve-at-start, no negative). Detail in [TRD §6](./TRD.md).

### 4.7 Later drive-planning optimizer
V1 must already answer: for every herd animal, what vaccine is due, by what date, in
which shed, and under which approved vaccine-animal matrix row. The later
optimizer starts after that. It answers the operations question: when should
the farm run a practical drive, which animals go into it, and how to allocate
route/resources around the V1-approved vaccine groups.

Basic Calendar aggregation is V1. Once the sweeper attaches animal due
rows to a shed-drive batch, Calendar must show the drive as the active item and
must not duplicate every batched per-animal `dose_due` row as a separate active
Calendar event. Animal-level due status remains visible in Passport, Protocol
Adherence, Vaccination detail, and audit surfaces.

The planner works like this:

1. Start from V1 due work generated from the completed vaccine-animal matrix. This
   includes new animal creation, purchased/intake animals, existing-animal backfill
   after a published rule, stage changes, shed shifts, trusted history
   suppression, and next-dose generation from accepted completions.
2. Convert one-animal rows into buckets such as:
   `park + shed + vaccine + dose + due-window + eligibility state`.
   This keeps one million herd animals manageable because the planner works on buckets
   and affected cohorts, not a full-herd recalculation every time.
3. Apply hard safety gates before scoring anything: lifecycle active, not
   dead/sold/transferred/lost/culled, health/defer state, pregnancy/lactation
   rule, quarantine/ICU rule, proof/SOP requirement, trained worker, stock,
   cold-chain, and the V1-authored vaccine compatibility policy.
4. Build operational drive groups from the V1-safe vaccine groups for each
   shed/time window. Unsafe same-day combinations or required 2-week/4-week gaps
   are already represented by the V1 matrix and source policy; the optimizer
   separates them while planning routes and resources.
5. Search candidate dates only inside the approved medical window
   (`earliest_safe_date`, `ideal_date`, `last_safe_date`). A date outside the
   safe window is rejected, not merely given a bad score.
6. Score the remaining safe plans by operational value: animals covered, urgency,
   disease priority, stock expiry, route/resource efficiency, and fairness to
   small sheds. A one-animal shed can be held if waiting is medically safe, but it
   becomes a micro-drive if waiting would break the window.
   Example: if CBE has 5 K1-compatible animals due in one shed today and 15
   compatible animals in another shed whose safe medical window overlaps the next
   week, the planner may hold the smaller shed and create one 20-animal park drive
   only if every animal remains inside its medical `last_safe_date`. If the
   smaller shed's medical window ends before the batching hold date, it must run
   as a micro-drive now. The batching window is an operations hold, never a
   medical override.
7. Create the drive with its animal list, vaccine list, lot/stock reservation,
   SOP/proof requirements, worker, verifier, and route. A route may contain
   multiple sheds, but each shed keeps its own animal list, proof, and
   reconciliation.
8. On execution day, reconcile the scan against the plan: missing animals,
   shifted-in animals, shifted-out animals, newly sick/pregnant/quarantined animals,
   unreadable tags, deaths, sales, proof rejection, and cold-chain failure all
   create explicit cancel/defer/rework/replan actions. Nothing silently
   disappears from the process.
9. Replan incrementally. If one animal dies, moves shed, becomes sick, gets sold,
   or a proof fails, only that animal and its affected shed/vaccine bucket are
   invalidated. The system does not recompute the full million-animal herd.

The later optimizer is therefore a constraint-based shed-drive planner:
per-animal due generation plus cohort bucketing, V1-safe vaccine grouping,
bounded date search,
deterministic scoring, resource assignment, execution reconciliation, and
incremental replanning.

The split is deliberate: V1 owns the complete vaccine-animal matrix and per-animal
due generation plus basic shed-drive batching and Calendar drive-first
projection. The later optimizer consumes those due rows, batches, and
V1-authored compatibility fields to optimize routes, resources, timing, and
multi-vaccine drive plans. Do not treat drive planning as a substitute for the
V1 matrix or V1 Calendar de-duplication.

## 5. Surfaces (per the mock)

| Surface | Shows | Nav location |
|---|---|---|
| **Control Tower** | Live coverage %, drives due today, overdue, stock-low/expiring, breaches — per-park | top |
| **Action Center** | Worker queue: drives, catch-ups, verifications | top |
| **Vaccination** (module detail) | Link to generic Config filtered by `category=vaccination`, schedule calendar, drive list, animal passport history, stock by lot | **under Preventive Care (PC)** (moved from Health) |
| **Config — Protocol Rules** | Company default + park override active rulesets, version history, impact preview, and activation audit | **Admin / Data Ops** |
| **Animal Passport** | One herd animal's full vaccination record + next due | per-animal |

**Mock nav correction:** Vaccination lives under **Preventive Care (PC)**, not Health.

## 6. Success metrics
Coverage % within window (per vaccine/park/species/shed tag) · on-time drive rate · stock integrity (zero negative, zero expired-lot use) · **zero ghost-overdue** (dead/sold never overdue) · engine latency (obligation generated promptly after herd-animal CRUD).

## 7. Source-derived baseline and remaining production inputs
1. **Source-nuance matrix selected** — use the tracked matrix in
   [source-nuances-rules.md](./source-nuances-rules.md) for V1 Config presets:
   ET+TT at 4 and 7 weeks with 2 ml, PPR at 16 weeks, FMD/HS at 12 weeks, Goat
   Pox shifted to 20 weeks for live-live spacing, plus adult revaccination
   intervals. Older ET/K1/day-21/0.5 ml fixture language is legacy local proof
   context only and must not be the active ruleset contract.
2. **Shed tag age policy** — use the Goats and Parks shed-tag age ranges as
   base reference data: K0 source days 1-2, K1 3-9, K2 10-77, K3 78-84,
   fattening kid tags 120-240, and adult tags 300+. Store the source display
   range and a normalized zero-based `min_age_days`/`max_age_days` on
   `animal_stage_lookup`/tag policy. Keep max-stay/residence policy separate
   from display age range, e.g. K1 max seven days and K2 about 42 days/six
   weeks are operational stay notes, not the medical vaccine schedule.
3. **Source Nuance roster** — [source-nuances-rules.md](./source-nuances-rules.md)
   now carries the V1 schedule/dose/vial/revaccination source for ET+TT, PPR,
   Goat Pox, Sheep Pox, Blue Tongue, FMD, and HS rows. Goat-specific vaccines
   apply to goat species rows; sheep-specific vaccines apply to sheep species
   rows; shared vaccines apply to both only when the active matrix says so.

## 8. Legacy capability parity, proof policy, and import mapping

Vaccination replaces legacy SOP/form behavior with GoatOS protocol, SOP, proof,
completion, inventory, and verification records. Capability parity means
preserve useful source signals and close legacy gaps; it does not mean copying
weak proof assumptions. Known legacy gaps to close: row/header existence cannot
count as dose proof, medicine batch/vial-lot cannot be optional, adverse
reactions need notes/follow-up, and verifier/park-head review must be durable.

| Legacy/source signal | GoatOS contract |
| --- | --- |
| SOP playground labels `PPR`, `ET`, `FMD`, `HS`, `BQ` | Keep as tracked SOP/vocabulary labels. Schedule-bearing obligations come from the active vaccination matrix version, not labels or one-vaccine protocol rows. |
| SOP proof fields: scheduled date, operator, animal scan, vaccine name, medicine batch, dose ml, administered date, proof photo/media, adverse reaction, verifier, notes | Normalize into `protocol_versions.rule_dsl.proof_policy`, `sop_versions.form_dsl`, `sop_submissions`, and `vaccination_completions`. Required first-slice fields are animal scan, vaccine, medicine batch/vial-lot, dose, administered date/time, proof media, adverse-reaction flag/notes, and verifier/park-head review. |
| Committed `000075` draft SOP skeleton (`shed_video`, `vial_lot`, `cold_chain`, `dose`, `route_site`, `administered_at`, `adverse_reaction`, `est_vs_used`, `verifier_review`) | Treat as the committed starting skeleton, not the final source contract. Upgrade the SOP version/proof policy to the source-normalized shape before calling vaccination SOP parity closed. |
| Procurement/legacy rows that mention vaccination | Treat as source evidence with confidence/proof semantics only. The live legacy guardrail found no reliable first-class vaccination evidence field in cleaned BigQuery tables, so a procurement row/header alone is not an administered dose. |
| Reliable historical vaccination record, if later proven | Import to staging, reconcile into `vaccination_completions`, mark matching obligations completed, and schedule boosters from the actual administered date. |
| Missing or untrusted history | Do not invent completions. For older animals whose old dose windows are already past, create one safe catch-up/review action first, not every missed historical dose as same-day work. After Preventive Care (PC) approval, generate baseline/catch-up shed drives per `docs/protocol-engine/migration-and-cutover.md` instead of fabricating administered history. |
