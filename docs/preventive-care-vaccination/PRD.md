# Preventive Care (PC) → Vaccination — Product Requirements (PRD)

**Status:** Draft v2 · **Date:** 2026-06-22
**Source hierarchy:** committed GoatOS migrations and protocol-engine docs for
repo state; `context/source-findings/preventive-care-vaccination-roster-stage-proposal.md`
and `context/source-findings/live-legacy-critical-guardrails-2026-06-28.md` for
accepted source findings; then `/Users/ravi/mesha/wiki` goatOS/Preventive Care/Health
handbook material, including the tracked vaccination rules source
[vaccination-rules.md](./vaccination-rules.md), and the product mock
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
| Park-level drive plans + shed/tag breakdown + SOP execution + proof video | Booster-chain interrupt policy | Other Preventive Care (PC) modules (engine-ready, not built) |
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
| Animal identifiers | Every herd animal has the internal immutable `animal_id` plus required species-neutral field/business identifier `animal_identifier_1`; `animal_identifier_2` is optional until double RFID tagging is live. These are parallel identifiers, not old/new IDs. When both current values are present, they must be different on the same animal. Each identifier value is globally single-use for life: one value can belong to exactly one animal ever, and is never reused after death, sale, transfer, tag breakage, or tag loss. UI/API/config/docs must call them Animal ID 1 and Animal ID 2; raw source column names stay import provenance only. The double RFID rollout must make Animal ID 2 mandatory in both application validation and DB constraints. |
| Shed/tag | Shared operational cohort/location state with explicit `allowed_species`. Goat and sheep can share the same kid shed/tag where the source/park data says so, but tags are not automatically universal across species. |
| Tag age policy | `animal_stage_lookup`/shed-tag policy stores source age range, normalized age days, purpose, allowed species, and max-stay/transition policy where known. |
| Vaccination rule | Selector over `species + breed/breed group + shed_tag/stage + age days + sex + lifecycle/health/reproductive/procurement state`. |

Path B is the base direction: implementation must migrate canonical naming to
`herd_animals` / `animal_id` and API/domain language to Animal/HerdAnimal. The
old physical `goats` / `goat_id` names are legacy model debt and must not be
the target for this base correction.

## 3. Actors & authority (CEO/COO-only config)

Role = **Vertical × Tier**, park-scoped (committed `user_scope_grants`: roles `admin/park_head/operator/verifier/ceo_internal`).

| Actor | Does | Authority |
|---|---|---|
| **CEO/CXO** | Creates, edits, previews, and publishes the vaccine ruleset. Published version is immutable + effective-dated. | `protocol.publish.vaccination` capability (category-specific) plus top-level Config route access |
| **Preventive Care (PC) Director** | Sees effective operational instructions, coverage, exceptions, and escalations only. | No raw Config visibility in V1 unless the product owner later grants a separate read-only config capability |
| **Park Head / Manager** | Sees their park's drives & overdue; assigns/approves | scoped |
| **Health worker (field)** | Executes the drive via SOP: scan, administer, record dose, upload proof | `vaccination.execute` |
| **Verifier** | Reviews proof submissions | `proof.verify` |
| **System (engine)** | Generates obligations, batches drives, fires reminders, moves stock via the inventory ledger, surfaces anomalies — **never invents rules** | — |

**Correction:** it is **not** "Preventive Care (PC) Director drafts and CEO/COO reviews."
For V1, the Config screen is visible only to CEO/CXO. The durable
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

The matrix must also carry the vaccination vaccination rules from
[vaccination-rules.md](./vaccination-rules.md): vaccine class and
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
| Procurement path, herd-entry date, warm-up, trusted procurement holding-park vaccination history | `procurement_pc_handoffs`, `procurement_hf_vaccination_evidence` (legacy table name; semantic trust is our supervised procurement holding park only), animal `origin_type`/`entry_date` |
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

### 4.3 Due → operator drive plan appears (the work unit)
The sweeper/planner batches due per-animal obligations into compatible drive
work, then the operator-drive planner assigns that work by date, available
operator, physical shed, partition, animal count, and vaccine bundle. The
medical obligation kernel remains unchanged: kid/adult rules, boosters, repeat
cycles, buffers, contraindications, and animal-state deferrals decide what is
due. The drive planning layer decides who can handle each due animal and when.

Operator capacity is consumed by unique animals handled per operator per
business date, not by vaccine doses or obligation rows. One animal receiving a
same-day bundle still consumes one operator animal slot. If available operator
capacity is insufficient, remaining animals spill to the next date and operator
availability is recomputed from timetable, leave/day-off, role, and scope.

The operational goal is to give the doctors the maximum safe animal count for
one park visit while preserving field execution clarity. The plan carries
physical shed, partition, species, vaccine-bundle, and animal lists for proof
and execution. Detailed planner rules live in
[operator-drive-planner-PRD.md](./operator-drive-planner-PRD.md).

Drive grouping is stage-aware:
- Kid shed/tag groups may combine goat and sheep kids in the same park drive
  when due windows, vaccine compatibility, stock, health, and warm-up rules are
  all safe.
- Adult groups stay species-specific inside the same park visit: adult goat work
  and adult sheep work are separate execution groups because their vaccine sets
  differ. This is not a requirement for doctors to visit twice; it is a safety
  rule so Goat Pox never leaks to sheep and Sheep Pox/Blue Tongue never leaks to
  goats.

### 4.4 Field worker executes (per-animal scan + proof, then shed acknowledgement)
The real work happens at ANIMAL level: the operator scans each animal (RFID/old tag)
and captures ONE live camera proof clip per animal at the moment of administration.
There is **no shed-level manual medical form** — the operator does not re-enter vaccine
batch, cold chain, dose, route/site, administered date/time, or adverse-reaction fields
on a recap screen. Those are either derived server-side (`administered_at` = the submit
time) or handled on separate paths (adverse events go through the health problem-report
workflow, not a vaccination form field).

Shed completion is a **final acknowledgement**: a read-only summary (see the
`ShedCompletionSummary` contract in [TRD §6](./TRD.md)) showing the human shed name, drive
name, expected/handled/proof-ready counts, and vaccine breakdown, plus a Submit button.
Submit is enabled only when **every expected animal in the shed is scanned AND has proof
ready** (`handled_count == expected_count AND proof_ready_count == expected_count`);
over-scanning or any unscanned/un-proofed animal blocks submit with a human
`blocking_reason`. Submitting records one per-animal completion derived from scan+proof;
the verifier then reviews. See ADR
[docs/decisions/vaccination-shed-ack-not-form.md](../decisions/vaccination-shed-ack-not-form.md).

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
rows to a park-drive batch, Calendar must show the drive as the active item and
must not duplicate every batched per-animal `dose_due` row as a separate active
Calendar event. Animal-level due status remains visible in Passport, Protocol
Adherence, Vaccination detail, and audit surfaces. Any Calendar drive target
drawer must list eligible **herd animals** with Animal ID 1 and Animal ID 2
fields; it must not expose RFID/old-tag/source-sheet identity columns or label
the list as goat-only work.

Drive planning has two different clocks. The **medical window** is the hard
safety window for a dose. The **batching hold** is an operations-only delay used
to merge compatible same-park shed/tag work into a larger doctor visit. GoatOS
may hold a due group up to 7 calendar days only once per obligation/dose cycle;
it must not keep postponing the same due item to chase a larger future drive.
If holding would cross the medical `last_safe_date`, if the group was already
held once, or if compatibility/stock/proof/worker gates fail, it becomes a
micro-drive or explicit exception now.

Recovery after a predefined defer state uses the same bounded operations clock.
If a sick, under-treatment, recovering, ICU, quarantine, late-pregnancy, or post-breeding
animal becomes eligible again after missing its drive, GoatOS must find the
nearest compatible same-park drive within 7 calendar days of the recovery/ready
date. If no compatible drive exists inside that buffer, it schedules a
micro-drive inside the buffer, even for one animal. The planner must never wait
10+ days just because a larger drive exists later.

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
   park/time window, with shed/tag retained as the execution breakdown. Shed
   count is not the batching goal; distinct safe animals are. Unsafe
   same-day combinations or required 2-week/4-week gaps are already represented
   by the V1 matrix and source policy; the optimizer separates them while
   planning routes and resources.
5. Search candidate dates only inside the approved medical window
   (`earliest_safe_date`, `ideal_date`, `last_safe_date`). A date outside the
   safe window is rejected, not merely given a bad score.
6. Score the remaining safe plans by operational value: distinct animals
   covered first, then urgency, disease priority, stock expiry,
   route/resource efficiency, and fairness to small sheds/tags. Obligation row
   count, vaccine count, and shed count must not beat distinct animal output. A
   1-2 animal group can be held if waiting is medically safe, but it becomes a
   micro-drive only if waiting would break the window or no compatible same-park
   work exists between that group's due/ready date and safe-until date.
   Example: if CBE has 5 K1-compatible animals due in one shed today and 15
   compatible animals in another shed whose safe medical window overlaps the next
   week, the planner may hold the smaller shed and create one 20-animal park drive
   only if every animal remains inside its medical `last_safe_date`. If the
   smaller shed's medical window ends before the batching hold date, it must run
   as a micro-drive now. The batching window is an operations hold, never a
   medical override.
7. Enforce the one-time hold rule. A due item can use the configured batching
   hold once, defaulting to at most 7 calendar days. If it has already been held
   once for this obligation/dose cycle, the next decision is execute, micro-drive,
   defer while a real temporary blocker is active; when that blocker clears,
   recovery/clearance becomes the new ready anchor and the planner schedules
   inside that anchor's +7-day buffer. It is never moved again just because
   another larger group appears.
8. Enforce the per-animal shot cap before finalizing a same-day plan. The default
   cap is 2 shots per animal per drive/doctor visit. If 3+ vaccines are due, the
   planner chooses the highest-priority compatible pair that is medically safe
   today and schedules the remainder from that session date using live/killed
   gap rules, priority, and the +7-day safe scheduling buffer. Operator-cap
   overflow is never permission to give a next-day third vaccine. The normal
   per-drive animal cap is
   operational and soft on a last-safe-day overflow: move overflow to the next
   feasible day when safe; keep the animal in the current/last-safe drive even
   over that normal cap when moving would cross its safe-until date.
9. Create the drive with its animal list, vaccine list, lot/stock reservation,
   SOP/proof requirements, worker, verifier, and route. A route may contain
   multiple sheds, but each shed keeps its own animal list, proof, and
   reconciliation.
10. On execution day, reconcile the scan against the plan: missing animals,
   shifted-in animals, shifted-out animals, newly sick/pregnant/quarantined animals,
   unreadable tags, deaths, sales, proof rejection, and cold-chain failure all
   create explicit cancel/defer/rework/replan actions. Nothing silently
   disappears from the process.
11. Replan incrementally. If one animal dies, moves shed, becomes sick, gets sold,
   or a proof fails, only that animal and its affected shed/vaccine bucket are
   invalidated. The system does not recompute the full million-animal herd.

Trusted procurement holding-park vaccination is part of the normal course, not
a loose "source record." Animals may be held 4–5 weeks in our procurement
holding parks near the buying region; our team administers/validates vaccines
there under SOP/video/physical proof. Those doses are trusted and the regular
shed schedule continues from them. Any vaccination claim outside our parks or
our supervised procurement holding parks is untrusted; after accepted intake
into a normal shed, the animal starts/restarts through GoatOS rules after the
warm-up and health gates.

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

Control Tower and Protocol Adherence are command surfaces, not 200-row client
dumps. They must use backend-owned filters, sort, and pagination (default page
size 25; supported sizes 10/25/50) for severity, state, park, date window,
owner, and search. A filtered-empty page must say that no rows match the current
filters; it must not show the global healthy/no-risk message.

**Mock nav correction:** Vaccination lives under **Preventive Care (PC)**, not Health.

## 6. Success metrics
Coverage % within window (per vaccine/park/species/shed tag) · on-time drive rate · stock integrity (zero negative, zero expired-lot use) · **zero ghost-overdue** (dead/sold never overdue) · engine latency (obligation generated promptly after herd-animal CRUD).

## 7. Source-derived baseline and remaining production inputs
1. **Vaccination Rules matrix selected** — use the tracked matrix in
   [vaccination-rules.md](./vaccination-rules.md) for V1 Config presets:
   ET+TT at 4 and 7 weeks with 2 ml, PPR at 16 weeks, FMD/HS at 12 weeks, Goat
   Pox shifted to 20 weeks for live-live spacing, plus adult revaccination
   intervals. Older ET/K1/day-21/0.5 ml fixture language is legacy local proof
   context only and must not be the active ruleset contract.
2. **Shed tag age policy** — use the Goats and Parks shed-tag age ranges as
   base reference data: K0 source days 1-2, K1 3-9, K2 10-77, K3 78-84,
   ICU milk kid tags 3-77, fattening and ICU/quarantine fattening kid tags
   120-240, and adult tags 300+. Store the source display range and a normalized
   zero-based `min_age_days`/`max_age_days` on `animal_stage_lookup`/tag policy.
   Keep max-stay/residence policy separate from display age range, e.g. K1 max
   seven days and K2 about 42 days/six weeks are operational stay notes, not the
   medical vaccine schedule. Store `allowed_species` per tag: K0-K3 can support
   mixed kid herds when the park data says so. Mother/lactation is a biological
   state that can apply to goat and sheep mothers for K0 and vaccination:
   sheep mothers can produce enough milk for the newborn around K0, and their
   vaccination obligations follow the normal adult/sheep repeat and catch-up
   policy. Do not convert that biological state into sheep milking tags.
   Accepted herd animals must always carry real sex (`female` or `male`).
   Blank/unknown/inferred-only/conflicting legacy sex is rejected before
   canonical creation/import and must never become a vaccination rule selector
   or obligation target.
   Commercial milking tags such as `Mother Milking Waiting`, `Milking Warmup`,
   and `Milking` are goat/doe-only because they represent a goat milk-production
   workflow, unless a future approved sheep dairy policy creates sheep
   equivalents. `BUCK` is treated as the shared adult-male breeder tag for
   vaccination eligibility across goat and sheep unless operations later creates
   species-specific male-breeder tags.
   GoatOS must enforce this at every entry point: animal creation/import,
   current shed/stage changes, rule authoring, API writes, and UI option lists
   must reject or hide species/tag pairs that are not allowed by the Goats and
   Parks tag policy. A sheep cannot be created, moved, selected, or matched
   into commercial goat-milking tags.
3. **No mother-vaccination-status category** — vaccination V1 must not create,
   store, ask, import, seed, show, or match a shed tag, category tag, matrix
   dimension, JSON selector, API field, rule row, fallback schedule, or UI
   option based on missing/unknown/not-vaccinated mother evidence. If a source
   workbook/DOCX includes that branch, GoatOS ignores it and always uses the
   approved standard kid schedule. The business policy is that mothers are kept
   vaccinated before/through the breeding and pregnancy workflow; scheduling
   never branches on dam vaccination status.
4. **Vaccination Rules roster** — [vaccination-rules.md](./vaccination-rules.md)
   now carries the V1 schedule/dose/vial/revaccination source for ET+TT, PPR,
   Goat Pox, Sheep Pox, Blue Tongue, FMD, and HS rows. Goat-specific vaccines
   apply to goat species rows; sheep-specific vaccines apply to sheep species
   rows; shared vaccines apply to both only when the active matrix says so.
5. **Clean-slate V1 seed, not dirty-row repair** — V1 implementation starts
   from the governed model `parks -> sheds -> shed/tag policy -> herd_animals`,
   then reseeds local/dev/test data through that model. Do not try to preserve
   bad goat-only table state or patch existing dirty rows in place. The source
   rows are evidence for the reseed, not the runtime schema. Every seeded animal
   must be a mixed-species herd animal with species, breed, real sex
   (`female`/`male` only), DOB/age, required `animal_identifier_1`, optional
   `animal_identifier_2`, current park, current shed/tag, health/reproductive/
   procurement state, and vaccination history/protocol facts attached to
   `animal_id`. The second external identifier becomes required for goats,
   sheep, and future species only after double RFID tagging is live and enforced
   in both code and DB constraints; identifiers must not be named or modeled as
   old/new identifiers.
   The seed/import duplicate check runs against all current and historical
   identifier values, not only active animals. If an identifier value has ever
   belonged to any animal, it cannot be assigned to another animal.
   Death/sale/exit does not release an identifier.
6. **Dev/test fixture defaults are explicit and non-production** — until the
   production-grade importer is approved, local/dev/test reseed may fill missing
   values only with deterministic, rule-valid fixture values so the system can
   be tested end to end. Missing sex must become either `female` or `male` by a
   documented seed rule; it must never become `unknown`. Missing species/breed/
   tag/identifier values must resolve through the governed goat/sheep species
   catalog, breed aliases, Goats and Parks tag policy, and current one-required/
   one-optional animal identity rules or land in a blocked seed review bucket.
   For local/dev/test only, missing identifier values may be deterministic
   fixture IDs with seed/test provenance; production rows block when Animal ID 1
   is missing. After double RFID tagging is live, production rows must also
   block until Animal ID 2 is known and DB constraints enforce that invariant.
   Vaccination fixture history is seeded only from known source evidence or from
   explicit synthetic test scenarios derived from the active matrix; do not
   invent production completions. These fixture assumptions are marked as
   seed/test provenance and are not production truth.
7. **No old-dashboard or BQ-port runtime leftovers** — V1 is not a legacy
   dashboard mirror, import-review product, conflict-resolution queue, or
   spreadsheet/BQ sync runtime. The clean-slate seed creates only valid GoatOS
   herd animals, parks, sheds, tags, vaccination history, and protocol facts.
   Bad source rows are fixed at source or blocked from seed; GoatOS must not
   carry `legacy_import_*`, `legacy_sync_*`, BQ dashboard source contexts,
   freshness widgets, identity review/dispute states, `source_confidence`, or
   old tag/sheet/external-system identifier names as product concepts,
   OpenAPI fields, UI copy, or final schema.
8. **Tag loss/replacement keeps the animal operational** — the point of two IDs
   is field resilience. If one physical tag/identifier falls off, breaks, or is
   replaced, the old value is marked broken/retired in identifier history and is
   dead forever; it must never be refitted or issued to another animal. The
   animal continues to operate, scan, and receive vaccination work through the
   surviving identifier while replacement is pending. The replacement tag must
   use a brand-new globally unused value and fill the vacant identifier slot in
   the same audited transaction that records who replaced it and when.

## 8. Legacy capability parity, proof policy, and import mapping

Vaccination replaces legacy SOP/form behavior with GoatOS protocol, SOP, proof,
completion, inventory, and verification records. Capability parity means
preserve useful source signals and close legacy gaps; it does not mean copying
weak proof assumptions. Known legacy gaps to close: row/header existence cannot
count as dose proof, and verifier/park-head review must be durable. The shed SOP
is deliberately **not** a manual medical form — the real evidence is the per-animal
scan + per-animal camera proof, and shed completion is an acknowledgement over that
state. See ADR
[docs/decisions/vaccination-shed-ack-not-form.md](../decisions/vaccination-shed-ack-not-form.md).

| Legacy/source signal | GoatOS contract |
| --- | --- |
| SOP playground labels `PPR`, `ET`, `FMD`, `HS`, `BQ` | Keep as tracked SOP/vocabulary labels. Schedule-bearing obligations come from the active vaccination matrix version, not labels or one-vaccine protocol rows. |
| SOP proof fields: scheduled date, operator, animal scan, vaccine name, medicine batch, dose ml, administered date, proof photo/media, adverse reaction, verifier, notes | The **only** operator-collected SOP field is the per-animal scan roster (`goat_ids`), each scanned row carrying its own live camera proof. Vaccine + dose come from the drive/obligation (config), `administered_at` is derived server-side, and adverse events are filed on the health problem-report path — none are shed-form answers. Manual fields (`vaccine_lot_id`, `cold_chain_verified`, `dose_ml_given`, `route_site`, `administered_at`, `adverse_reaction`, `adverse_reaction_notes`) are **banned** from `sop_versions.form_dsl`; `vaccination_completions` keeps those columns but populates them server-side (dose/route NULL, adverse false). Verifier/park-head review stays durable. |
| Committed draft SOP skeleton (`shed_video`, `vial_lot`, `cold_chain`, `dose`, `route_site`, `administered_at`, `adverse_reaction`, `est_vs_used`, `verifier_review`) | Superseded. These batch-level videos and manual fields were stripped from the vaccination SOP form_dsl (migration `000006` removed the batch videos; `000007` removed the remaining manual medical fields). Do not reintroduce them — enforced by the `vaccination-shed-ack-guard` CI check. |
| Procurement/legacy rows that mention vaccination | Treat as source evidence with confidence/proof semantics only. The live legacy guardrail found no reliable first-class vaccination evidence field in cleaned BigQuery tables, so a procurement row/header alone is not an administered dose. |
| Reliable historical vaccination record, if later proven | Import to staging, reconcile into `vaccination_completions`, mark matching obligations completed, and schedule boosters from the actual administered date. |
| Missing or untrusted history | Do not invent completions. For older animals whose old dose windows are already past, create one safe catch-up/review action first, not every missed historical dose as same-day work. After Preventive Care (PC) approval, generate baseline/catch-up shed drives per `docs/protocol-engine/migration-and-cutover.md` instead of fabricating administered history. |
