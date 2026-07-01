# PHC → Vaccination — Technical Requirements / Design (TRD)

**Status:** Draft v2 (corrected against committed schema) · **Repo-state note:** 2026-06-29
**Companion:** [PRD.md](./PRD.md) · **Foundation:** [Generic Protocol & Obligation Engine](../protocol-engine/obligation-engine.md)
**Grounded in:** this TRD was originally grounded in `backend/migrations/postgres/000001…000060`; the repo now contains later protocol/obligation/inventory/vaccination migrations through `000117` at the Feed Direction correction. Use the live migrations and [Generic Protocol & Obligation Engine](../protocol-engine/obligation-engine.md) for current repo state. The wiki goatOS handbook §6, accepted source findings (`context/source-findings/phc-vaccination-roster-stage-proposal.md`, `context/source-findings/live-legacy-critical-guardrails-2026-06-28.md`), and the tracked nuance source [source-nuances-rules.md](./source-nuances-rules.md) are design/source references, **not** a replacement for committed schema — where they differ, committed wins.

> **v2 correction note.** v1 assessed the wiki DDL as if committed and was wrong: it assumed `parks`/`sheds`/`vaccine_stock` tables and `goats.dob`/`goats.gender` columns that **do not exist**. v2 separates **committed → target**, builds on the generic obligation engine, and drops the unverified cost figure.

---

## 1. Scope of this TRD

Vaccination is the **first module of the PHC vertical** (PHC also covers deworming, biosecurity, feed/water testing, panel cleaning, sanitization, fire-safety, SOP-video, stock checks — all on the **same** engine). Daily/weekly **director reporting is cross-cutting** (an org-wide reporting cadence), **not a PHC/Vaccination module**, and is out of scope here. This TRD covers:
1. The **goats current→target delta** (identity data the cascade reads).
2. The **shed/location profile delta** (the dosing anchor).
3. **Vaccination-specific tables** that link into the generic engine.
4. Generic **inventory** for vaccine stock (FEFO).
5. Migration plan, legacy parity, and import/replay mapping.

The engine itself (protocol_*, obligation_*, scheduler boundary, outbox, authority) is in the [engine doc](../protocol-engine/obligation-engine.md) — not repeated here.

---

## 2. `goats` — current → target DELTA

Committed `goats` (`000001` 227-283, reuse-as-is) already has: `goat_id, tenant_id, display_id, species, breed/breed_id, sex(female/male/unknown), approx_dob, age_band, lifecycle_status, reproductive_status, growth_cohort_tag, management_stage, health_status, identity_state, custodian_party_id, current_location_id, farm_id, park_id, shed_id, cohort_id, merged_into_goat_id, source_confidence, row_version`. No native ENUMs (text+CHECK). Triggers: `prevent_merged_write`, `prevent_hard_delete`.

| Field | Committed? | Target | Action |
|---|---|---|---|
| `approx_dob date` | **YES** | keep | none — v1's `dob` was wrong; canonical is **`approx_dob`** (carries estimate semantics) |
| `dob date NULL` (exact) | NO | precise birth if ever known | **add** separate column; do **not** rename `approx_dob` |
| `dob_estimated boolean` | NO | estimate vs exact | **add** `NOT NULL DEFAULT true` |
| `sex` | **YES** (CHECK female/male/unknown) | keep `sex` | none — v1's `gender` was wrong. Do **not** rename (`goats_breed_sex_lifecycle_idx`, counts snapshot depend on it) |
| `origin_type` | NO | birth/procured/imported | **add** `text CHECK IN ('birth','procured','imported','unknown')` |
| `entry_date date` | NO | herd-entry date (≠ `created_at`) | **add** |
| `is_tagged`/`tagging_status` | NO | tagging state | **derived, NOT stored** — a read-model/view over `goat_identifiers` (has active rfid? old-tag only? none?). No column on `goats`; avoids a denormalized flag drifting from the identifier truth. |
| `exited_at`/`exit_reason` | NO | lifecycle exit | **add** `exited_at timestamptz`, `exit_reason text CHECK('sold','died','culled','transferred','lost')`; CHECK: `exited_at` non-null ⇒ `lifecycle_status` in exited set |
| `lifecycle_status` | **YES** (NN, **no CHECK**) | constrain | **add CHECK** (free text today) |
| `health_status, management_stage, reproductive_status, age_band` | **YES** (no CHECK) | constrain | **add CHECK** enums |
| animal stage (K0/K1/K2/K3/F2/Mother/Pregnant) | partial (`shed_id`, `management_stage`, `growth_cohort_tag`) | **default-derive from shed/cohort, allow override** (§3, §2.1) | **none stored on goats** — default = shed's `animal_stage`; explicit per-goat overrides exist |
| withdrawal (`withdrawal_until_date`) | NO | meat/milk sale-block after dose | **add to completion record (§4)**, NOT goats — event-scoped, not identity-scoped |
| `current_weight_kg`, `sale_status`, `gender`, `current_shed_id` | NO | — | do **not** add — weight = scale-capture event; sale_status = lifecycle-derived; use `sex`/`shed_id` |

### 2.1 Eligibility is multi-factor, not shed-derived alone
Shed/cohort `animal_stage` is the **default** anchor, but eligibility combines **age + sex + lifecycle_status + health_status + shed/cohort animal_stage**. Exceptions the engine must honor: ICU / quarantine (defer, don't fire), pregnant / lactating (different rule or skip), and sick / under_treatment (defer). Phase 0 does **not** expose per-goat individual override generation; PHC-approved catch-up uses the manual campaign path so it still creates canonical obligations/batches. Encode the predicate in `protocol_rules.eligibility_json`; tracked exceptions become `deferred` obligations with a reason — never silently inherit only the shed value.

---

## 3. Shed / location profile DELTA (the dosing anchor)

**No `parks`/`sheds` tables exist** and none will be created. `locations` (self-FK tree, `location_type` farm/park/shed/cohort/pen) is the base. Add typed **1:1 profile extensions** (same pattern as committed `location_operational_attributes`), each guarded so the parent's `location_type` matches:

- **`farm_profiles`** (location_type='farm') — **disambiguates "farm"** (fix): `location_id PK→locations, tenant_id, farm_kind text CHECK('contract_farmer','fodder_farm','internal'), operator_party_id NULL, row_version`. Resolves the ambiguity that `location_type='farm'` mixes contract-farmer/fodder farms with internal sites; keeps Feed Direction from leaking back into "Farms" (feed sources from `fodder_farm`, operations run in parks).
- **`park_profiles`** (location_type='park'): `location_id PK→locations, tenant_id, region_code, manager_workforce_member_id NULL→workforce_members, feed_prep_location_id NULL→locations, total_shed_count, row_version`.
- **`shed_profiles`** (location_type='shed'): `location_id PK→locations, tenant_id, sex_grouping CHECK('male','female','mixed'), animal_stage_id NULL→animal_stage_lookup, shed_lifecycle_status_id NULL→shed_lifecycle_status_lookup, row_layout CHECK('L_C_R','single','custom'), has_icu_corner bool, is_feed_prep_at_end bool, transport_group text, ordinal_in_park int, row_version`. ← aeroplane L/C/R rows, ICU corner, feed-prep end here.

**Two separate lookups (fix — don't mix stage with operational status):**
- **`animal_stage_lookup`** (drives vaccination eligibility; **committed schema = `backend/migrations/postgres/000071_location_profiles.sql`**): `animal_stage_id uuid PK, tenant_id, stage_code text (NO static enum — stage codes are tenant config data, not a DDL CHECK), name, min_age_days, max_age_days, status CHECK('active','inactive','retired'), sort_order`, UNIQUE `(tenant_id, stage_code)`. **Eligibility uses THIS.** _(An earlier TRD draft sketched `stage_id`/`label`/`min_weight_kg`/`max_weight_kg` + a hardcoded `stage_code` CHECK enum; the shipped migration is the data-driven shape above — no static stage-code enum, `name` not `label`, age bands only, no weight columns. The migration is source of truth; add weight bands later only via a new migration if PHC needs them.)_
- **`shed_lifecycle_status_lookup`** (operational state of the shed, NOT the animals): `status_id PK, tenant_id, status_code CHECK('commissioning','active','cleaning','decommissioned'), label, is_operational, sort_order`. Does **not** drive dosing.
- ICU/quarantine/holding booleans already live on committed `location_operational_attributes` (reuse).

**Capacity:** do not add a column — `location_capacity_records` (temporal, no-overlap trigger) is the committed home. **Cohort:** reuse `location_type='cohort'` rows + `goats.cohort_id`; a cohort can also carry an `animal_stage_id` for stage-by-cohort.

Age/weight bands that decide stage live on `animal_stage_lookup` as **config** — the mock's hardcoded `SHIFT_THRESH` (K1=7/K2=45) must be removed and read from here. For local/dev, **K2 = 42 days / six weeks** is the selected source-derived baseline; the old 45 is treated as legacy mock drift unless a later PHC source-backed config version overrides it. The Config authoring stage picker reads these rows via `GET /protocols/animal-stages` (active rows, `sort_order`), never hardcoded `K0/K1/K2` literals; when the lookup is empty the picker is disabled-with-reason (Data Ops must seed stages) rather than falling back to code-defined bands.

---

## 4. Vaccination-specific tables — link INTO the engine

The protocol/obligation/SOP/escalation tables are the [engine](../protocol-engine/obligation-engine.md). Vaccination adds:

**Config (multi-dose / multi-phase — NOT trigger-day + booster-offset):** a `protocol_definitions` row `category='vaccination'`; the schedule is authored in `protocol_versions.rule_dsl` as a **`schedule[]` array — one entry per dose/phase** (`primary`, `booster_1`, `booster_2`, `annual`, `catch_up`, …), each with `trigger_type` (birth_age/post_arrival/calendar/after_previous_completion/manual_campaign) · `offset_days` · `due_window_days` · `min_gap_days` · `repeat` (none/every_n_days/yearly; age-window repeat authoring is rejected until generator support lands) · `repeat_until_after_age` · `catch_up` · per-dose `sop_label` (display only — the executable SOP binds at `protocol_versions.sop_version_id`; a genuine per-dose executable override uses `protocol_rules.sop_version_id`, never a free-text label) + `proof_policy`. The engine **expands each `schedule[]` row into one `protocol_rules` row**. Rule-level: multi-factor `eligibility_json` (age_band + animal_stage + sex + breed + lifecycle + health + reproductive[exclude pregnant/lactating] + defer_states[ICU/quarantine/sick]), `missed_dose_policy` (immediate/next_cycle/phc_approval/defer), `withdrawal_days`. Per-park override = a park-scoped `protocol_version` (no `park_id` column on a vaccine table — scope is the obligation's `scope_id`). **No `vaccine_config` table** — it *is* `protocol_rules`. _Lifecycle example:_ `0–12mo` → `primary` (birth_age) + `booster_1` (after_previous_completion) + repeat every N months; `>12mo` → `annual` (`repeat:yearly`, modeled as its own eligible lifecycle row); next due is derived from trigger/repeat/catch-up logic and trusted accepted completions, not a separate DSL field.

### 4.0 V1 vaccine-goat matrix acceptance

The first voice-note requirement maps to V1, not V2: GoatOS must support a
reviewable matrix that says which vaccine applies to which goat type and when.
The matrix is authored through protocol/version/rule config and must be usable
for goat creation, purchased/intake goats, existing-goat backfill, stage change,
location/shed change, health change, and accepted-completion next-dose
generation.

Each source-approved vaccine matrix row must carry:

| Matrix field | V1 requirement |
|---|---|
| Vaccine identity | vaccine code/name, inventory item, manufacturer/detail linkage where known |
| Vaccine property | vaccine type/class such as live, killed, toxoid, combo, or unknown-review-needed; pathogen class such as bacterial, viral, mixed, or unknown-review-needed |
| Dose row | dose code, sequence, dose amount, route/site if required, proof/SOP binding |
| Trigger | birth-age, post-arrival/intake, calendar, manual campaign, or after previous completion |
| Medical window | earliest safe date, ideal/offset date, latest safe date, min gap, max delay, missed-dose policy |
| Repeat/lifetime | none, every N days, yearly, booster sequence, lifetime/age cutoff, course-lapse/restart policy or explicit unsupported/review-needed |
| Goat category | age range, animal stage/shed tag, sex, breed if relevant, lifecycle state, source confidence |
| Reproductive state | allowed/blocked/review-needed for pregnant, lactating, mother, buck, flushing, breeding, warm-up |
| Health/defer state | allowed/deferred/blocked for sick, under treatment, ICU, quarantine, recovery, adverse-event review |
| History handling | trusted accepted history suppresses or advances the row; untrusted/unknown history produces catch-up/review, with older-goat anti-flood behavior that creates one safe next catch-up/review action before any further historical dose rows |
| Source approval | source system/ref, reviewer, approval status, approved_at, effective dates |
| Nuance policy | procurement warm-up days, kid normal-schedule cutoff weeks, adult source vaccination flag, live/killed/live spacing days, same-day allowance metadata, pregnancy skip/catch-up windows |

Without those rows, GoatOS can only prove reusable engine plumbing; it cannot
honestly claim the full practical vaccine matrix is complete. `PPR`, `FMD`,
`HS`, `BQ`, Goat Pox, and ET+TT-style combinations must not become live
schedule-bearing rules from labels alone; each needs source-approved matrix
rows before V1 can demo it as real config. Cross-vaccine same-day/gap
compatibility from the source is part of the V1 matrix contract. Later drive
optimization may use those fields for route/resource grouping, but it must not
invent or postpone the medical rules.

The V1 `rule_dsl` stores the nuance policy in first-class JSONB objects:
`vaccine.pathogen_class`, `compatibility_policy`,
`procurement_policy`, and `pregnancy_policy`. Generation enforces the pieces
that are local to one published version today: eligibility, defer states,
post-arrival offsets, missed-dose/catch-up policy, trusted-history suppression,
older-goat anti-flood, schedule min gaps, and source compatibility spacing for
the V1-authored matrix. When multiple source rows are loaded together, V1
authoring applies safe-date offsets for conflicts such as live-live spacing; the
later drive optimizer uses the same stored policy fields only for operational
partitioning.

**Source / review metadata (nested `source` object on `rule_dsl` — canonical shape):** every rule carries provenance + an approval gate under `source:{ … }` — `source_system` (vaccinations_db / phc / vet / manual_admin), `source_ref`, `imported_at`, `reviewed_by`, `review_status` (extracted → reviewed → approved), `approved_by`, `approved_at`. **Publish gate:** a version may be published **only when `review_status='approved'`** and source-backed. **Dev policy:** values that come from a real source (Vaccinations DB / PHC / vet-approved) and are marked `approved` are **real config in `goatos-dev` and publishable there** — the **dev-real path**. Unsourced / `extracted` rows stay `status='draft'` with a **`not source-backed`** warning and cannot be published. Never hand-invent vaccine schedule values.

### 4.1 Legacy parity, proof policy, and import replay

When this slice replaces legacy Slack/Sheets/App Script vaccination SOP behavior,
the parity floor is the useful source signal plus stronger GoatOS validation, not
bug-for-bug row copying. Known legacy gaps to close are row/header evidence being
treated as dose proof, optional/weak medicine-batch capture, adverse reactions
without durable notes/follow-up, and review confidence not being first-class.

- `rule_dsl.proof_policy` and `sop_versions.form_dsl` normalize the source SOP
  shape: scheduled date, operator, goat scan, vaccine name, medicine
  batch/vial-lot, dose ml, administered date/time, proof media, adverse reaction
  + notes/follow-up, verifier/park-head review, and cold-chain/quantity checks
  where the SOP version requires them.
- The committed `000075` SOP version is only a draft skeleton
  (`shed_video`, `vial_lot`, `cold_chain`, `dose`, `route_site`,
  `administered_at`, `adverse_reaction`, `est_vs_used`, `verifier_review`). It
  must be upgraded or superseded by a source-normalized SOP/proof-policy version
  before vaccination SOP parity is called closed.
- Procurement or legacy vaccination mentions are source evidence with
  `source_ref` + confidence/proof semantics. The live legacy guardrail found no
  first-class trusted vaccination evidence field in the cleaned BigQuery tables,
  so a procurement row/header alone must not create a completed dose.
- Reliable historical vaccination records, if later proven, import through
  staging, reconcile into `vaccination_completions`, complete matching
  obligations, and schedule boosters from actual `administered_at`. Missing or
  untrusted history first creates one safe catch-up/review action for older goats
  whose historical windows are already past, not every old dose as same-day
  work; after PHC approval it becomes catch-up shed drives via
  [migration-and-cutover.md](../protocol-engine/migration-and-cutover.md), never
  fabricated completions.
- `PPR`, `FMD`, `HS`, and `BQ` are source-backed SOP/vocabulary labels only
  until source extracts provide timing/dose/booster policy plus approval
  metadata. Only ET/K1/day-21 is schedule-bearing today.

**Due state:** per-goat doses = `obligation_instances` rows (`target_type='goat'`, `scope_type='shed'`, `rule_id` set so two vaccines/doses due the same day on one goat don't collide). `after_previous_completion` / booster doses generate on the prior dose's **actual `administered_at`** (`trigger_type='after_previous_completion'`, respecting `min_gap_days`) — see SM-7.

**Shed drive = an `obligation_batches` row** (the generic work-unit, [engine §4](../protocol-engine/obligation-engine.md)), NOT just a `sop_task`. The batch carries drive-level fields the per-goat obligation can't — generic columns: `estimated_targets`, `planned_quantity`/`reserved_quantity`/`used_quantity` + `quantity_unit` (= doses/ml for vaccination), `primary_inventory_lot_id`, `conducted_by`, `proof_ref`, `context jsonb`. The sweeper groups a shed's due obligations into the batch (`obligation_instances.batch_id`), which spawns **one** `sop_task` (assigned via `vaccination.execute`) — many obligations → one batch → one task.

**Completion (net-new module table):**
`vaccination_completions`: `completion_id PK · tenant_id · obligation_id NOT NULL→obligation_instances · batch_id NULL→obligation_batches · goat_id→goats · sop_submission_item_id NULL→sop_submission_items · vaccine_inventory_lot_id NULL→inventory_stock(§5) · doses int · dose_ml_given · route_site text · adverse_reaction bool · adverse_reaction_problem_id NULL · cold_chain_verified bool · administered_at · withdrawal_until_date date NULL · recorded_by · idempotency_key`.
- **UNIQUE `(tenant_id, obligation_id, goat_id)`** — a dose recorded once; double-submit = no-op (the idempotency the mock assumes but had no backing column for).
- On insert (one txn): write `obligation_status_events('completed')` + `outbox_messages`. **Stock is NOT decremented per completion** in a group drive — see §5 for the mode split.

**Side effects:** vaccination has no shifting side effect → do **not** touch `movement_commands` (its CHECK is `'shifting.apply'` only). If a future protocol needs a typed side-effect dispatch, add an obligation-scoped command table, don't overload it.

---

## 5. Vaccine stock = generic inventory (FEFO), not an island

Inventory is **committed as the shared generic kernel** (starting with `000072`);
PHC vaccination must reuse/enhance it, not rebuild a vaccination-only island (per
[engine §6](../protocol-engine/obligation-engine.md)):
- `inventory_items` `category` includes `'vaccine'`, `base_unit` = `dose`/`ml`; `vaccines` detail table (`manufacturer, default_dose_ml, requires_booster, booster_interval_days, storage_temp_min/max, withdrawal_period_days`) FK→`inventory_items`.
- `inventory_stock` lots (running balances, **`numeric` + `quantity_unit`**, never int — engine is shared with feed which is kg/litre): `lot_number, expiry_date, quantity_in_stock numeric, quantity_reserved numeric, quantity_consumed numeric, quantity_unit`, park scope via `location_id`, **`CHECK(quantity_in_stock >= 0)`**.
- **`inventory_stock_movements`** append-only ledger (reserve/release/consume/adjust/transfer/expire; `quantity numeric`) — the audit/reconciliation truth; balances are its projection. FEFO pick in app, expiry-gated (`expiry_date >= administered_at`).

**Consumption mode (resolves the v1 contradiction — pick by path, never both):**
- **Group shed drive:** at **batch start** record one `reserve` movement for `planned_quantity` against the FEFO lot (`reserved_quantity += n`). At **drive close / verification** record `consume` for actual used + `release` for the remainder. **No per-goat decrement.**
- **Manual campaign / catch-up**: PHC-approved catch-up creates canonical obligations/batches before execution; ad hoc per-goat individual override generation is not exposed in Phase 0.

- `[P2]` `inventory_stock.is_quarantined` + a cold-chain excursion table for the breach→quarantine cascade.

---

## 6. Edge-case matrix (must pass before "done")

| Case | Handling |
|---|---|
| Goat shifted mid-schedule | obligation `scope_id`/`batch_id` re-target to destination shed; eligibility re-evaluated against new shed's `animal_stage`; same-vaccine only |
| Goat dies/sold | `goat.exited_at` set ⇒ same-txn `obligation_instances` for that goat → `canceled`; status event logged |
| Missed vs blocked | `missed` (window passed) vs `waived`/blocked with reason (ICU/quarantine/stock-out) — distinct statuses |
| Double-submit completion | `UNIQUE(tenant_id,obligation_id,goat_id)` + `idempotency_key` → no-op |
| Stock-out mid-drive | reserve-at-start surfaces shortfall; `CHECK(>=0)` blocks negative |
| Expired lot | FEFO pick gated by `expiry_date` |
| Config change in flight | new `protocol_version` (immutable) + `effective_from`; live obligations keep their version |
| Multi-park, different rule | park-scoped `protocol_version`, not a per-table `park_id` |
| Far-future booster | `obligation_instances` row in Postgres (queryable), enqueued to Cloud Tasks only near due |

## 6A. Later drive planner algorithm

V1 generation remains the source of truth for due work: the complete
source-approved vaccine-goat matrix plus goat facts creates or updates
`obligation_instances` for each goat. The later planner consumes those rows and
produces optimized execution-ready `obligation_batches` without recomputing the
whole herd.

V1 also owns basic shed-drive batching and Calendar de-duplication. When V1
attaches due rows to an `obligation_batches` shed drive, Calendar projects the
drive row as active operations work and suppresses the batched per-goat
`vaccination_dose_due` projections from active Calendar. The per-goat rows stay
available to detail and audit surfaces.

**Inputs**
- `obligation_instances` for vaccination rules, including `rule_id`,
  `protocol_version_id`, `due_at`, `window_start`, `window_end`, target goat,
  scope shed/cohort, status, and trusted completion suppression.
- Goat facts used by eligibility and replanning: lifecycle, exit reason,
  health, reproductive status, management stage, approximate DOB/age, breed,
  sex, current park/shed/cohort, identity/tag state, and source confidence.
- Rule config: schedule rows, eligibility JSON, defer states, min gaps,
  due-window policy, missed-dose policy, proof policy, SOP binding, withdrawal
  days, and the V1 source vaccine compatibility policy.
- Operational facts: available stock/lot/cold-chain, trained worker/verifier,
  route capacity, proof requirements, and active drive locks.

**Algorithm**
1. Select due candidates through indexed windows, never an unbounded goat scan:
   `(tenant_id, status, due_at)` plus scope filters. Candidates are active
   `scheduled`/`due` obligations, plus `deferred` obligations whose current goat
   facts now satisfy a derived ready-again predicate. Readiness after defer is a
   computed predicate, not a stored canonical status.
2. Revalidate each candidate against current goat facts. Dead, sold,
   transferred, culled, or lost goats cancel open work. Sick, quarantine, ICU,
   pregnancy, and lactation states apply the rule's allow/defer/block/review
   policy. Shed shifts re-scope only same-vaccine open work. V1 already enforces
   pregnancy/lactation from current facts during generation/backfill; a
   standalone `goat.reproductive_status.changed` recheck event is a V2/provenance
   extension until a producer and consumer are shipped.
3. Bucket candidates by:
   `(tenant_id, park, shed_or_cohort, protocol_version_id, rule_id, dose_code,
   due_window_bucket, eligibility/defer_state)`. This reduces million-goat
   planning from animal-level iteration to bounded cohort work.
4. For each shed/time bucket, build a vaccine conflict graph. Each vaccine or
   dose family is a node. An edge means the two nodes cannot be executed in the
   same drive because of live/killed compatibility, same-vaccine gap, required
   2-week/4-week separation, route/site restriction, or unknown compatibility
   that must fail closed. Graph partitioning/coloring yields safe same-day
   vaccine groups.
5. Generate candidate drive dates only inside each group's medical window:
   `earliest_safe_date <= planned_date <= last_safe_date`. Dates outside the
   window are rejected before scoring. The planner must also carry an operations
   hold window, for example `batch_hold_until = min(first_due_at + max_safe_wait_days,
   last_safe_date)`, plus policy fields for `minimum_drive_size`,
   `force_micro_drive_below_last_safe_date`, shed-tag compatibility, park route
   scope, operator capacity, verifier capacity, stock/lot expiry, and cold-chain
   duration.
6. Decide hold-vs-micro-drive before scoring. Waiting to combine sheds/cohorts
   is allowed only when every goat in the merged candidate remains inside its
   medical safe window. If a +1 week hold would pass any goat's `last_safe_date`,
   the small shed becomes a micro-drive now. Example: 5 due K1 goats in one CBE
   shed and 15 compatible K1 goats in another CBE shed may become one 20-goat
   drive only if the 5 goats can medically wait through that hold.
7. Score safe candidates deterministically. Hard constraints are not scores.
   Scored factors are urgency/earliest deadline, goats covered, disease
   priority, stock expiry, worker/route efficiency, cold-chain route duration,
   and fairness to small sheds that have already waited.
8. Pick the best candidate with stable tie-breakers:
   earliest deadline, higher medical priority, more goats covered, expiring
   stock, lower route cost, then oldest waiting shed bucket.
9. Assign resources under transaction/lease control: create or update
   `obligation_batches`, attach obligations, reserve stock where vaccination
   policy requires it, create the SOP task, and store worker/verifier/proof
   requirements. Concurrent planners must use idempotency keys and row locks so
   two workers cannot claim the same obligations.
10. Execute and reconcile by scan. Missing goats stay open/missed/follow-up;
   shifted-in eligible goats become explicit extras; shifted-out goats move to
   the destination bucket; newly sick/pregnant/quarantined goats defer or block;
   deaths/sales cancel; unreadable tags create identity exceptions; proof
   rejection and cold-chain failure create rework.
11. Replan incrementally. Events such as `goat.exited`,
    `goat.location.changed`, `goat.health.changed`, future/proven
    `goat.reproductive_status.changed`, `proof.rejected`, `stock.shortfall`, or
    `cold_chain.failed` invalidate only the affected goat, bucket, vaccine
    group, and batch. The whole herd is never recalculated for one state change.

**Boundary**
The later optimizer requires the V1-authored vaccine compatibility fields plus
policy-approved drive thresholds. V1 owns per-goat due work, source matrix
rules, compatibility spacing, basic shed batching, and Calendar drive-first
projection. The later optimizer owns route/resource scoring, multi-shed
planning, and large-scale incremental replanning. Separately, V1 itself must
not be called complete until the source-approved vaccine-goat matrix in §4.0 is
configured and proven.

---

## 7. Migration plan (historical; later implemented as `000070+`)

| Migration | Contents |
|---|---|
| `000070_goats_provenance_lifecycle.sql` | ALTER `goats`: add `dob`, `dob_estimated`, `origin_type`, `entry_date`, `exited_at`, `exit_reason`; add CHECKs to lifecycle/health/stage. **Tagging state is a derived view over `goat_identifiers`, NOT a stored column.** |
| `000071_location_profiles.sql` | `farm_profiles`, `park_profiles`, `shed_profiles`, **`animal_stage_lookup`** + **`shed_lifecycle_status_lookup`** (split — not one mixed table), (+ location_type guard triggers). |
| `000072_inventory_foundation.sql` | `inventory_items`, `vaccines`, `inventory_stock` (+ `CHECK(quantity_in_stock>=0)`), **`inventory_stock_movements`** ledger. |
| `000073_protocol_engine.sql` | `protocol_definitions/versions/rules/triggers`; **`EXCLUDE` (GiST) non-overlap** on published effective windows (needs `btree_gist`); **category-specific capability seeds — explicit, no wildcard:** grant COO/CEO `protocol.publish.vaccination` (+ Director `protocol.draft.vaccination`). Every new category later adds its own draft/publish seeds. |
| `000074_obligation_engine.sql` | `obligation_instances` (guard incl. `rule_id` + `NULLS NOT DISTINCT`, deterministic idempotency_key, scope-validate trigger), **`obligation_batches`**, `obligation_status_events` (RANGE-partitioned), `obligation_escalations`. |
| `000075_vaccination_module.sql` | `vaccination_completions` (+ UNIQUE, batch + inventory FK); ALTER `feature_coverage_registry` CHECK to add `'vaccination'`; vaccination SOP definition/version seed. |

**Reuse exemplars:** `000001` (RANGE partition + outbox + idempotency), `000030` (projection contract + `COALESCE` unique precedent), `000050` (RBAC + capabilities), `000060` (SOP engine, task state machine).

**Spec the 7 state machines before coding** — see [state-machines.md](../protocol-engine/state-machines.md). **PHC/Vaccination uses SM-1/2/3/4/5/7**; **Feed adds SM-6**: SM-1 schedule generation, SM-2 shift recompute, SM-3 death/sale cancel, SM-4 batch lifecycle, SM-5 stock reserve/consume/release, SM-6 feed generation, SM-7 booster generation. Agree these before SQL.

---

## 8. Keep / enhance / ditch
- **KEEP (reuse-as-is):** goats, goat_identifiers, goat_location_history, goat_identity_events, locations + operational_attributes + capacity_records, workforce_*, user_scope_grants, audit_log, outbox_messages, idempotency_keys, sop_*.
- **ENHANCE:** goats (provenance/lifecycle cols + CHECKs), locations (+profiles), feature_coverage_registry (+vaccination).
- **DITCH from runtime (reference/migration-only):** legacy_import_*, legacy_sync_* as read source, BQ reconcile, dashboard-parity-with-BigQuery thinking. Control Tower reads Postgres/projection only.

## 9. Source-derived local/dev baseline and later inputs
- Local/dev schedule baseline: `Enterotoxaemia` / ET, K1, day 21, 0.5 ml, 7d
  window, +14d booster clue, sourced from the PRD example and recorded in
  `context/source-findings/phc-vaccination-roster-stage-proposal.md`.
- Local/dev stage baseline: K0 max 1d, K1 max 7d, K2 max 42d, K3 from day 43;
  seed/read this from `animal_stage_lookup`.
- Later production expansion: add PPR/FMD/HS/BQ schedule-bearing rows only when
  source extracts provide timing/dose/booster values. These are roster/SOP labels
  today, not blockers for local/dev or E2E.
