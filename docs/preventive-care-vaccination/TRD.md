# Preventive Care (PC) → Vaccination — Technical Requirements / Design (TRD)

**Status:** Draft v2 (corrected against committed schema) · **Repo-state note:** 2026-06-29
**Companion:** [PRD.md](./PRD.md) · **Foundation:** [Generic Protocol & Obligation Engine](../protocol-engine/obligation-engine.md)
**Grounded in:** this TRD was originally grounded in `backend/migrations/postgres/000001…000060`; the repo now contains later protocol/obligation/inventory/vaccination migrations through `000117` at the Feed Direction correction. Use the live migrations and [Generic Protocol & Obligation Engine](../protocol-engine/obligation-engine.md) for current repo state. The wiki goatOS handbook §6, accepted source findings (`context/source-findings/preventive-care-vaccination-roster-stage-proposal.md`, `context/source-findings/live-legacy-critical-guardrails-2026-06-28.md`), and the tracked nuance source [source-nuances-rules.md](./source-nuances-rules.md) are design/source references, **not** a replacement for committed schema — where they differ, committed wins.

> **v2 correction note.** v1 assessed the wiki DDL as if committed and was wrong: it assumed `parks`/`sheds`/`vaccine_stock` tables and animal DOB/sex columns that **do not exist in that shape**. v2 separates **committed → target**, builds on the generic obligation engine, and drops the unverified cost figure.

---

## 1. Scope of this TRD

Vaccination is the **first module of the Preventive Care (PC) vertical** (Preventive Care (PC) also covers deworming, biosecurity, feed/water testing, panel cleaning, sanitization, fire-safety, SOP-video, stock checks — all on the **same** engine). Daily/weekly **director reporting is cross-cutting** (an org-wide reporting cadence), **not a Preventive Care (PC) / Vaccination module**, and is out of scope here. This TRD covers:
1. The **herd-animal current→target delta** (identity data the cascade reads).
2. The **shed/location profile delta** (the dosing anchor).
3. **Vaccination-specific tables** that link into the generic engine.
4. Generic **inventory** for vaccine stock (FEFO).
5. Migration plan, legacy parity, and import/replay mapping.

The engine itself (protocol_*, obligation_*, scheduler boundary, outbox, authority) is in the [engine doc](../protocol-engine/obligation-engine.md) — not repeated here.

---

## 2. Herd animals — Path B target model

The committed `goats` table (`000001`) is the current implementation, but it is
not the target model. It hardcodes the wrong concept at the storage boundary and
currently carries `goats_species_check CHECK (species = 'goat')`, which blocks
the mixed goat/sheep herd reality from legacy count data and the Goats and Parks
source. V1 base correction uses **Path B**: migrate canonical storage,
contracts, domain types, events, and UI language to `herd_animals` /
`animal_id`. The old `goats` / `goat_id` naming is legacy model debt and must
not remain as the new architecture.

### 2.1 Required canonical tables

| Table | Purpose |
|---|---|
| `species_catalog` | Tenant/configured species reference. Seed `goat` and `sheep`; support future species without DDL or code branches. |
| `breeds` | Breed/reference rows belong to exactly one species through `species_id`; aliases map dirty legacy labels to reviewed breed rows. |
| `herd_animals` | Canonical animal identity and current state. This replaces `goats` as the target entity. |
| `animal_identifiers` | Current and historical identifier lookup for any species. Every canonical herd animal starts with two required current field/business identifier slots: `animal_identifier_1` and `animal_identifier_2`; both are mandatory and must be different on the same animal. Identifier values are globally single-use for life: one value maps to exactly one `animal_id` ever and is never reused after death, sale, transfer, tag breakage, or tag loss. Raw source column names are provenance only and must not leak into canonical DB/API/UI names as old/new identity. |
| `animal_location_history` | Temporal movement history for any species. |
| `animal_identity_events` | Identity/audit events for any species. |

`herd_animals` target columns:

| Field | Target rule |
|---|---|
| `animal_id uuid` | Immutable internal ID. Replaces `goat_id`. |
| `tenant_id uuid` | Mandatory tenant boundary. |
| `animal_identifier_1 text`, `animal_identifier_2 text` | Required current external field/business IDs for every goat, sheep, and future species at creation/seed. They are parallel IDs, not old/new IDs; UI labels are Animal ID 1 and Animal ID 2. The two current values on one animal must be different, and every value must be globally unique across current and historical identifier tables. |
| `display_id text` | Optional read/display label derived from `animal_identifier_1`/`animal_identifier_2`; it is not a third business identifier and must not imply goat-only identity. |
| `species_id uuid` / `species_code text` | Required FK/reference to `species_catalog`; no goat-only CHECK. |
| `breed_id uuid` / `breed text` | Breed belongs to the selected species; dirty text is alias/provenance only. |
| `sex text` | Required canonical animal sex: `female` or `male` only. `unknown`, blank, inferred, or conflicting sex is a blocking validation error, not an accepted herd value and not a vaccination selector. |
| `dob date`, `dob_confidence text` | Date of birth with confidence (`exact`, `estimated`, `unknown`). |
| `approx_dob date` | Keep only as migrated provenance/compat input until replaced by `dob` + confidence. |
| `origin_type text`, `entry_date date` | `birth`, `procured`, or an explicit approved source/import type plus herd-entry date. `unknown` is not allowed in the accepted clean-slate herd; unresolved source rows block from seed/import. |
| `lifecycle_status`, `health_status`, `reproductive_status`, `lactation_state` | Typed/current animal states used by protocols. |
| `current_location_id`, `park_id`, `shed_id`, `cohort_id` | Current physical scope. Every clean current animal must resolve to one current shed/tag. |
| `current_shed_tag_id` / `animal_stage_id` | Current operational cohort tag derived from shed profile unless explicitly reviewed. |
| `exited_at`, `exit_reason` | Death/sale/transfer/cull/lost lifecycle exit; cancels open obligations. |
| `merged_into_animal_id`, `row_version`, audit columns | Merge/optimistic-lock/audit support. |

Do not store `current_weight_kg` or `sale_status` directly on
`herd_animals`: weight is an event/read-model fact, and sale status is
lifecycle/promise state. Do not keep `age_band` as a loose animal text column;
age eligibility comes from DOB + shed-tag age policy + rule-specific age
selectors.

### 2.2 Shed tag age policy and mixed-species placement

Goats and sheep can share the same physical shed and shed tag where the
source/park data allows it. Species is an animal fact; shed tag is a shared
operational cohort fact with explicit `allowed_species`. Every current herd
animal must belong to exactly one current shed/tag in the clean read model, and
the animal's species must be allowed by that tag policy.

`animal_stage_lookup` / shed-tag policy must store:

| Field | Purpose |
|---|---|
| `stage_code` | K0, K1, K2, K3, FATTENING_MALE, WARMUP_BUCK, etc. |
| `name` | Display label from Goats and Parks. |
| `kid_adult_class` | `kid` or `adult`. |
| `source_age_range_label` | Human/source display, e.g. `10-77 days`. |
| `source_age_min_day_number`, `source_age_max_day_number` | Source day numbering where DOB is Day 1. |
| `min_age_days`, `max_age_days` | Normalized zero-based age days for queries/validation. |
| `allowed_species_codes` | Read-model/API projection of the species that can use the tag, backed by a governed stage-species policy table or equivalent normalized config. Kid tags may allow goat + sheep where source/park data says so. Mother/lactation can be a sheep biological state for K0 and vaccination, but commercial milking workflow tags (`Mother Milking Waiting`, `Milking Warmup`, `Milking`) are goat/doe-only unless a future approved sheep dairy policy adds equivalents. |
| `max_residence_days` | Optional operational stay limit when source purpose states one, e.g. K1 max seven days. |
| `purpose` | Source-backed operational meaning. |
| `status`, `sort_order` | Governance/order. |

Stage/species invariants:

- Goats and sheep must both use the governed Goats and Parks tag catalog; do
  not create separate sheep-only shadow tags or a goat-only tab. Species
  eligibility lives on each tag/stage.
- Accepted herd animals must always have real sex (`female` or `male`). Legacy
  source rows with blank, unknown, inferred-only, or conflicting sex are rejected
  before canonical creation/import and cannot generate vaccination obligations.
  Rule authoring must not offer or store `unknown` sex selectors.
- V1 dev/test uses clean-slate wipe + reseed, not dirty-row repair. Existing
  goat-only rows, sheet/BQ snapshots, or source files are evidence for a new
  seed/import run; they are not patched in place as accepted herd rows. A row
  without real sex is not accepted into GoatOS. Local/dev/test may choose a
  deterministic `female` or `male` fixture value only under an explicit seed
  rule with seed/test provenance; production blocks until the real sex is
  provided at source. No database table, API, UI, seed, or rule selector may
  store or offer an unknown sex value.
- `MOTHER` / lactating adult is a biological reproductive state. It can match
  goat and sheep mothers for vaccination and should use the species-appropriate
  adult repeat/catch-up vaccine cells.
- `BUCK` is treated as the shared adult-male breeder tag for vaccination
  eligibility across goat and sheep unless operations later creates
  species-specific male-breeder tags.
- `MOTHER_MILKING_WAITING`, `MILKING_WARMUP`, and `MILKING` are commercial
  goat/doe milk-production workflow tags. Do not show or match them for sheep
  unless an approved sheep dairy policy adds explicit sheep equivalents.
- The database/config layer must store stage/species eligibility as governed
  data, preferably `animal_stage_species_policy(tenant_id, animal_stage_id,
  species_id/species_code, status, created_at, updated_at)` with unique
  `(tenant_id, animal_stage_id, species)` and FKs to `animal_stage_lookup` and
  `species_catalog`. If a JSONB bridge is used temporarily, expose the same
  shape through the API and replace it with the normalized table before V1
  production cutover.
- Animal creation/import, current shed assignment, animal stage changes, and
  rule publish/activation must validate species against this policy. Invalid
  combinations should fail with a stable error such as
  `invalid_species_for_stage`; legacy bad rows, if discovered, become Data Ops
  exceptions and are not eligible for automatic obligation generation.
- Rule matching must enforce `allowed_species_codes` / stage-species policy
  before vaccine cells are evaluated. A JSON row that combines sheep with a
  goat-only commercial milking stage is invalid even if the vaccine itself is
  otherwise valid for sheep.
- Admin UI and API option endpoints must filter stage/tag options by selected
  species. Sheep users should not see `MOTHER_MILKING_WAITING`,
  `MILKING_WARMUP`, or `MILKING` as selectable category/tag values; rule impact
  preview must report invalid existing combinations as blockers.
- Required tests: API/import rejects sheep + commercial milking stage, UI option
  contract omits commercial milking stages for sheep, rule publish rejects a
  sheep row containing goat-only commercial milking stage codes, allows sheep +
  `BUCK`, and generation skips/blocks any legacy invalid row instead of creating
  vaccination obligations.

Seed baseline from Goats and Parks:

| Stage | Source age range | Normalized age days | Notes |
|---|---:|---:|---|
| K0 - Newborn | 1-2 days | 0-1 | with mother; max one day stay. |
| K1 - Milk Training | 3-9 days | 2-8 | milk training; max seven days stay. |
| K2 - Milk Drinking | 10-77 days | 9-76 | milk drinking; generally about 42 days/six weeks stay. |
| K3 - Weaning | 78-84 days | 77-83 | milk ration cut and grain encouragement. |
| ICU Milk Kids / Quarantine Milk Kids | 3-77 days | 2-76 | milk-kid health/isolation tags. |
| Fattening Male/Female and warm-up variants | 120-240 days | 119-239 | post-weaning or procured warm-up/fattening. |
| ICU Fattening Kids / Quarantine Fattening Kids | 120-240 days | 119-239 | fattening-kid health/isolation tags. |
| Adult warm-up, non-pregnant, flushing, breeding, pregnancy, mother, milking, buck, ICU, quarantine | 300+ days | 299+ | adult operational/reproductive/health tags. |

Vaccination rules can target both `stage_code` and medical `age_days` windows.
Those are not the same thing: K2 is an operational shed tag; FMD/HS at 12 weeks
is a medical due rule.

There is no valid stage, category, rule dimension, seed, or UI option based on
mother vaccination-status. Any source branch that separates mother schedules by
vaccination status is ignored for V1; the canonical policy assumes mothers are
kept vaccinated and treats missing evidence as catch-up/review work under the
normal adult/mother tag, not as a separate cohort.

### 2.3 Eligibility is multi-factor, not shed-derived alone

Shed/cohort `animal_stage` is the default operational anchor, but eligibility
combines **species + breed + age + sex + lifecycle_status + health_status +
reproductive/lactation state + procurement/warm-up state + shed/cohort
animal_stage**. Exceptions the engine must honor: ICU / quarantine (defer,
don't fire), pregnant / lactating (different rule or skip), and sick /
under_treatment (defer). Phase 0 does **not** expose per-animal individual
override generation; Preventive Care approved catch-up uses the manual campaign
path so it still creates canonical obligations/batches. Encode the predicate in
`protocol_rules.eligibility_json`; tracked exceptions become `deferred`
obligations with a reason — never silently inherit only the shed value.

---

## 3. Shed / location profile DELTA (the dosing anchor)

**No `parks`/`sheds` tables exist** and none will be created. `locations` (self-FK tree, `location_type` farm/park/shed/cohort/pen) is the base. Add typed **1:1 profile extensions** (same pattern as committed `location_operational_attributes`), each guarded so the parent's `location_type` matches:

- **`farm_profiles`** (location_type='farm') — **disambiguates "farm"** (fix): `location_id PK→locations, tenant_id, farm_kind text CHECK('contract_farmer','fodder_farm','internal'), operator_party_id NULL, row_version`. Resolves the ambiguity that `location_type='farm'` mixes contract-farmer/fodder farms with internal sites; keeps Feed Direction from leaking back into "Farms" (feed sources from `fodder_farm`, operations run in parks).
- **`park_profiles`** (location_type='park'): `location_id PK→locations, tenant_id, region_code, manager_workforce_member_id NULL→workforce_members, feed_prep_location_id NULL→locations, total_shed_count, row_version`.
- **`shed_profiles`** (location_type='shed'): `location_id PK→locations, tenant_id, sex_grouping CHECK('male','female','mixed'), animal_stage_id NULL→animal_stage_lookup, shed_lifecycle_status_id NULL→shed_lifecycle_status_lookup, row_layout CHECK('L_C_R','single','custom'), has_icu_corner bool, is_feed_prep_at_end bool, transport_group text, ordinal_in_park int, row_version`. ← aeroplane L/C/R rows, ICU corner, feed-prep end here.

**Two separate lookups (fix — don't mix stage with operational status):**
- **`animal_stage_lookup`** (drives vaccination eligibility; **committed schema = `backend/migrations/postgres/000071_location_profiles.sql`**): `animal_stage_id uuid PK, tenant_id, stage_code text (NO static enum — stage codes are tenant config data, not a DDL CHECK), name, min_age_days, max_age_days, status CHECK('active','inactive','retired'), sort_order`, UNIQUE `(tenant_id, stage_code)`. **Eligibility uses THIS.** _(An earlier TRD draft sketched `stage_id`/`label`/`min_weight_kg`/`max_weight_kg` + a hardcoded `stage_code` CHECK enum; the shipped migration is the data-driven shape above — no static stage-code enum, `name` not `label`, age bands only, no weight columns. The migration is source of truth; add weight bands later only via a new migration if Preventive Care (PC) needs them.)_
- **`shed_lifecycle_status_lookup`** (operational state of the shed, NOT the animals): `status_id PK, tenant_id, status_code CHECK('commissioning','active','cleaning','decommissioned'), label, is_operational, sort_order`. Does **not** drive dosing.
- ICU/quarantine/holding booleans already live on committed `location_operational_attributes` (reuse).

**Capacity:** do not add a column — `location_capacity_records` (temporal, no-overlap trigger) is the committed home. **Cohort:** reuse `location_type='cohort'` rows + `herd_animals.cohort_id`; a cohort can also carry an `animal_stage_id` for stage-by-cohort.

Shed-tag age policy lives on `animal_stage_lookup` as **config** — the mock's hardcoded `SHIFT_THRESH` must be removed and read from here. The source display ranges from Goats and Parks are the baseline, with normalized zero-based age days and allowed-species policy stored for validation/querying. The Config authoring stage picker reads these rows via `GET /protocols/animal-stages` (active rows, `sort_order`), never hardcoded `K0/K1/K2` literals; when the lookup is empty the picker is disabled-with-reason (Data Ops must seed stages) rather than falling back to code-defined bands.

---

## 4. Vaccination-specific tables — link INTO the engine

The protocol/obligation/SOP/escalation tables are the [engine](../protocol-engine/obligation-engine.md). Vaccination adds:

**Config (scoped ruleset, multi-dose / multi-phase — NOT one vaccine per protocol):** a stable `protocol_definitions` row represents the logical vaccination ruleset family, for example `code='vaccination.matrix'`, `category='vaccination'`. Do not create one top-level protocol definition per vaccine, stage, species, breed, or copied row. The schedule is authored in `protocol_versions.rule_dsl` as the whole matrix for one scope. It contains **`schedule[]` / cell rows — one entry per dose/phase** (`primary`, `booster_1`, `booster_2`, `annual`, `catch_up`, …), each with `trigger_type` (birth_age/post_arrival/calendar/after_previous_completion/manual_campaign) · `offset_days` · `due_window_days` · `min_gap_days` · `repeat` (none/every_n_days/yearly; age-window repeat authoring is rejected until generator support lands) · `repeat_until_after_age` · `catch_up` · per-dose `sop_label` (display only — the executable SOP binds at `protocol_versions.sop_version_id`; a genuine per-dose executable override uses `protocol_rules.sop_version_id`, never a free-text label) + `proof_policy`. The engine **expands each matrix cell / `schedule[]` row into `protocol_rules` rows**. Rule-level: multi-factor `eligibility_json` (species + breed + age_days + shed_tag/animal_stage + sex + lifecycle + health + reproductive/lactation + procurement/warm-up + defer_states[ICU/quarantine/sick]), `missed_dose_policy` (immediate/next_cycle/phc_approval/defer), `withdrawal_days`. Per-park override = a park-scoped active `protocol_version` for the same `protocol_id` (no `park_id` column on a vaccine table — scope is `protocol_versions.scope_id` and the obligation's `scope_id`). **No `vaccine_config` table** — the matrix lives in `protocol_versions.rule_dsl` and expands to `protocol_rules`. _Lifecycle example:_ `0–12mo` → `primary` (birth_age) + `booster_1` (after_previous_completion) + repeat every N months; `>12mo` → `annual` (`repeat:yearly`, modeled as its own eligible lifecycle row); next due is derived from trigger/repeat/catch-up logic and trusted accepted completions, not a separate DSL field.

### 4.0A Rule JSON vs. herd facts boundary

`protocol_versions.rule_dsl` is the persisted authoring payload. It stores
policy, not animal rows. It must not contain a list like
embedded herd-animal required-field checklists, current animal snapshots, or copied shed rows. The
rule JSON stores:

- matrix dimensions and allowed selectors;
- row criteria expressed by stable lookup IDs/codes such as `species_code`,
  `breed_id`, `animal_stage_id`/`stage_code`, `sex`, lifecycle, health,
  reproductive state, procurement path, and age bounds;
- vaccine/dose cells, schedule rows, repeat policy, proof/SOP binding,
  compatibility/gap rules, defer/blocked rules, and catch-up policy;
- protocol version/audit metadata exposed from `protocol_versions`, not a
  source-review workflow embedded in the UI.

The data plane evaluates the rule against canonical facts:

| Current fact | Storage/read source |
|---|---|
| Herd identity and animal dimensions | `herd_animals`: `animal_id`, `tenant_id`, `species_id`/`species_code`, `breed_id`/`breed`, `sex`, `dob`/`approx_dob`, `lifecycle_status`, `health_status`, `reproductive_status`, `lactation_state` |
| Current physical scope | `herd_animals.current_location_id`, `herd_animals.park_id`, `herd_animals.shed_id`, `herd_animals.cohort_id`, `locations` |
| Shed tag/stage dimensions | `shed_profiles.animal_stage_id`, `animal_stage_lookup.stage_code`, `shed_profiles.sex_grouping`, `shed_profiles.has_icu_corner`, `location_operational_attributes.is_quarantine/is_icu/is_holding` |
| Procurement/intake dimensions | `herd_animals.origin_type`, `herd_animals.entry_date`, `procurement_phc_handoffs.entry_date`, warm-up/handoff state, trusted HF evidence |
| Vaccination history | `vaccination_completions` plus `obligation_instances.rule_id`, not a JSON list in the rule config |

For million-animal scale, V1 should add or maintain an indexed target-facts read
model for animal-targeted protocols, for example `animal_protocol_facts`
(`tenant_id`, `animal_id`, species, `breed_id`, sex, DOB/age bucket, lifecycle,
health, reproductive state, pregnancy month when available, lactation state
when available, origin/procurement path, warmup_until, park/shed/cohort IDs,
`animal_stage_id`, `stage_code`, and `facts_hash`). That table is a derived
read model: it is rebuilt from canonical animal/location/procurement/completion
facts and invalidated by animal CRUD, shed changes, health/reproductive changes,
procurement accepted-intake, and accepted vaccination completion events. Feed
Direction can use the same pattern with shed/cohort target-facts instead of
animal facts.

If impact preview or generation needs SQL-selectable predicates instead of
JSON parsing, compile each published matrix cell into a generic
`protocol_rule_dimension_values` table:
`tenant_id`, `protocol_version_id`, `rule_id`, `dimension_key`,
`include_exclude`, `value_kind`, `uuid_value`, `text_value`, `int_min`,
`int_max`, `sort_order`. That table is derived from `rule_dsl`; it is not the
authoring source. This keeps the engine generic for vaccination today and Feed
Direction tomorrow.

### 4.0B Scoped activation and override resolution

Vaccination has a module-specific active-cardinality policy:

```text
category = vaccination
ruleset_family = vaccination.matrix
scope_resolution_mode = tenant_default_with_park_overrides
active_cardinality = single_active_ruleset_per_scope
override_semantics = park_replaces_tenant_for_that_park
```

This policy is generic metadata, not hardcoded into the vaccination generator.
Future categories can choose different policies, for example additive multiple
active templates per scope or merge semantics. Vaccination does **not**: one
company active version and one active version per park are the only active
rulesets that can generate new vaccination obligations.

Recommended schema/contract direction:

| Object | Purpose |
|---|---|
| `protocol_definitions` | Stable ruleset family: `vaccination.matrix`, one row per category/family, not one row per vaccine. |
| `protocol_versions` | Immutable draft/active/inactive/retired versions of that family for either company scope (`scope_type='tenant', scope_id NULL`) or park scope (`scope_type='park', scope_id=<park location_id>`). |
| `protocol_category_scope_policies` | Category/family policy: resolution mode, active cardinality, override semantics, allowed scope levels, and whether activation supersedes open obligations. |
| `protocol_scope_resolutions` (table or materialized read model) | Fast resolution by tenant/category/park: which active version applies to this park now, whether it comes from company or park, and which company version is excluded by the park override. |

For vaccination, enforce:

- partial unique active row per `(tenant_id, protocol_id, scope_type,
  coalesce(scope_id, sentinel))`;
- no more than one active company version for `vaccination.matrix`;
- no more than one active park version for the same park;
- company version applies only where no active park override exists;
- draft and inactive versions never generate obligations.

If the DB keeps `status='published'` for historical immutability, expose a
separate `activation_state` (`draft`, `active`, `inactive`, `retired`,
`scheduled`) in API contracts. Product/UI wording should be "active/inactive",
because the user action is choosing the active ruleset for a scope. The old
`published` boolean is not enough for the Config list.

Resolution algorithm for a herd animal in park `P`:

1. Look for active `vaccination.matrix` where `scope_type='park'` and
   `scope_id=P`.
2. If found, use that park version only.
3. Otherwise use active company version where `scope_type='tenant'`.
4. If neither exists, no vaccination obligations generate and Config/Control
   Tower surfaces a missing-ruleset gap.

Activation side effects:

- Activating a new company version deactivates the prior company active version
  for future generation, but does not overwrite active park overrides.
- Activating a park version deactivates the prior active version for that park
  and excludes that park from the company version.
- Retiring a park version makes that park inherit the active company version on
  the next generation/recompute run.
- Completed obligations and accepted vaccination history keep their original
  `protocol_version_id`.
- Open scheduled/due company-version obligations for a newly overridden park are
  shown in impact preview and then superseded/recomputed under the park version
  if the operator activates with the default cutover behavior.
- In-progress batches are not silently rewritten. The impact preview must ask
  whether to let them finish, cancel/reissue, or create follow-up work.

Frontend/API contract for Config list:

| Field | Meaning |
|---|---|
| `category`, `protocol_code`, `protocol_name` | Stable family, e.g. `vaccination`, `vaccination.matrix`, `Vaccination Rule Matrix` |
| `scope_type`, `scope_id`, `scope_label` | `tenant`/Company-wide or `park`/specific park |
| `active_version_id`, `active_version_label`, `activation_state` | Current active/scheduled/inactive version state |
| `effective_from`, `effective_to`, `activated_by`, `activated_at` | Who set what, when, and which version |
| `applies_to_summary` | "All parks except 2 overrides" or "CBE only" |
| `excluded_scope_count`, `excluded_scopes` | Company row exclusions caused by active park overrides |
| `overrides_version_id` | Park row points to the company version it replaces for that park |
| `open_obligations_to_supersede`, `in_progress_batches_to_review` | Activation impact counts |
| `history_count` | Inactive/retired/scheduled versions available in the scope drawer |

The UI table should show one company row plus active park override rows. It
must not show ET+TT, PPR, FMD, HS, or Goat Pox as separate top-level protocol
rows. Those belong inside the matrix detail for the active version.

### 4.0 V1 vaccine-animal matrix acceptance

The first voice-note requirement maps to V1, not V2: GoatOS must support a
reviewable matrix that says which vaccine applies to which animal cohort and when.
The matrix is authored through protocol/version/rule config and must be usable
for animal creation, purchased/intake animals, existing-animal backfill, stage change,
location/shed change, health change, and accepted-completion next-dose
generation.

Each evidence-derived, CEO/COO-published vaccine matrix row must carry:

| Matrix field | V1 requirement |
|---|---|
| Vaccine identity | vaccine code/name, inventory item, manufacturer/detail linkage where known |
| Vaccine property | vaccine type/class such as live, killed, toxoid, combo, or unknown-review-needed; pathogen class such as bacterial, viral, mixed, or unknown-review-needed |
| Dose row | dose code, sequence, dose amount, route/site if required, proof/SOP binding |
| Trigger | birth-age, post-arrival/intake, calendar, manual campaign, or after previous completion |
| Medical window | earliest safe date, ideal/offset date, latest safe date, min gap, max delay, missed-dose policy |
| Repeat/lifetime | none, every N days, yearly, booster sequence, lifetime/age cutoff, course-lapse/restart policy or explicit unsupported/review-needed |
| Animal cohort | species, breed/breed group, shed tag/stage, source/display age range, normalized age days, sex, lifecycle state, and current health/reproductive/procurement facts |
| Reproductive state | allowed/blocked/review-needed for pregnant, lactating, mother, buck, flushing, breeding, warm-up |
| Health/defer state | allowed/deferred/blocked for sick, under treatment, ICU, quarantine, recovery, adverse-event review |
| History handling | trusted accepted history suppresses or advances the row; untrusted/unknown history produces catch-up/review, with older-animal anti-flood behavior that creates one safe next catch-up/review action before any further historical dose rows |
| Version audit | version number, created_by/created_at, published_by/published_at, effective dates, retired_by/retired_at where applicable |
| Nuance policy | procurement warm-up days, kid normal-schedule cutoff weeks, adult prior-vaccination flag, live/killed/live spacing days, same-day allowance metadata, pregnancy skip/catch-up windows |

Without those rows, GoatOS can only prove reusable engine plumbing; it cannot
honestly claim the full practical vaccine matrix is complete. `PPR`, `FMD`,
`HS`, `BQ`, Goat Pox, and ET+TT-style combinations must not become live
schedule-bearing rules from labels alone; each needs matrix
rows before V1 can demo it as real config. Cross-vaccine same-day/gap
compatibility from the tracked rules is part of the V1 matrix contract. Later drive
optimization may use those fields for route/resource grouping, but it must not
invent or postpone the medical rules.

The V1 `rule_dsl` stores the nuance policy in first-class JSONB objects:
`vaccine.pathogen_class`, `compatibility_policy`,
`procurement_policy`, and `pregnancy_policy`. Generation enforces the pieces
that are local to one published version today: eligibility, defer states,
post-arrival offsets, missed-dose/catch-up policy, trusted-history suppression,
older-animal anti-flood, schedule min gaps, and compatibility spacing for
the V1-authored matrix. When multiple matrix rows are loaded together, V1
authoring applies safe-date offsets for conflicts such as live-live spacing; the
later drive optimizer uses the same stored policy fields only for operational
partitioning.

**Audit metadata, not a source-review UI:** the Config UI is visible only to
CEO/COO/superadmin in V1. Do not add source-system/reviewer/approved-by fields
to the authoring surface or persisted rule JSON. The rule version is audited by
the protocol tables: `version`, `created_at`, `drafted_by`/creator,
`published_by`, `published_at`, `effective_from`, `effective_to`, `retired_at`,
plus the immutable `rule_dsl` that was published. Source documents and extracted
nuance tables remain engineering evidence in this repo; they are not runtime
approval columns, and publish is gated by CEO/COO authority, JSON-schema
validation, SOP binding, impact preview, and non-overlapping effective dates.

### 4.1 Legacy parity, proof policy, and import replay

When this slice replaces legacy Slack/Sheets/App Script vaccination SOP behavior,
the parity floor is the useful source signal plus stronger GoatOS validation, not
bug-for-bug row copying. Known legacy gaps to close are row/header evidence being
treated as dose proof, optional/weak medicine-batch capture, adverse reactions
without durable notes/follow-up, and review confidence not being first-class.

- `rule_dsl.proof_policy` and `sop_versions.form_dsl` normalize the source SOP
  shape: scheduled date, operator, animal scan, vaccine name, medicine
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
  untrusted history first creates one safe catch-up/review action for older animals
  whose historical windows are already past, not every old dose as same-day
  work; after Preventive Care (PC) approval it becomes catch-up shed drives via
  [migration-and-cutover.md](../protocol-engine/migration-and-cutover.md), never
  fabricated completions.
- `PPR`, `FMD`, `HS`, Goat Pox, and ET+TT schedule-bearing behavior now comes
  from the active scoped vaccination matrix version. `BQ` remains a tracked
  SOP/vocabulary label until evidence-derived timing/dose/booster policy is
  added to a future matrix version.

**Due state:** per-animal doses = `obligation_instances` rows (`target_type='herd_animal'`, `target_id=animal_id`, `scope_type='shed'`, `rule_id` set so two vaccines/doses due the same day on one animal don't collide). `after_previous_completion` / booster doses generate on the prior dose's **actual `administered_at`** (`trigger_type='after_previous_completion'`, respecting `min_gap_days`) — see SM-7.

**Shed drive = an `obligation_batches` row** (the generic work-unit, [engine §4](../protocol-engine/obligation-engine.md)), NOT just a `sop_task`. The batch carries drive-level fields the per-animal obligation can't — generic columns: `estimated_targets`, `planned_quantity`/`reserved_quantity`/`used_quantity` + `quantity_unit` (= doses/ml for vaccination), `primary_inventory_lot_id`, `conducted_by`, `proof_ref`, `context jsonb`. The sweeper groups a shed's due obligations into the batch (`obligation_instances.batch_id`), which spawns **one** `sop_task` (assigned via `vaccination.execute`) — many obligations → one batch → one task.

**Completion (net-new module table):**
`vaccination_completions`: `completion_id PK · tenant_id · obligation_id NOT NULL→obligation_instances · batch_id NULL→obligation_batches · animal_id→herd_animals · sop_submission_item_id NULL→sop_submission_items · vaccine_inventory_lot_id NULL→inventory_stock(§5) · doses int · dose_ml_given · route_site text · adverse_reaction bool · adverse_reaction_problem_id NULL · cold_chain_verified bool · administered_at · withdrawal_until_date date NULL · recorded_by · idempotency_key`.
- **UNIQUE `(tenant_id, obligation_id, animal_id)`** — a dose recorded once; double-submit = no-op (the idempotency the mock assumes but had no backing column for).
- On insert (one txn): write `obligation_status_events('completed')` + `outbox_messages`. **Stock is NOT decremented per completion** in a group drive — see §5 for the mode split.

**Side effects:** vaccination has no shifting side effect → do **not** touch `movement_commands` (its CHECK is `'shifting.apply'` only). If a future protocol needs a typed side-effect dispatch, add an obligation-scoped command table, don't overload it.

---

## 5. Vaccine stock = generic inventory (FEFO), not an island

Inventory is **committed as the shared generic kernel** (starting with `000072`);
Preventive Care (PC) vaccination must reuse/enhance it, not rebuild a vaccination-only island (per
[engine §6](../protocol-engine/obligation-engine.md)):
- `inventory_items` `category` includes `'vaccine'`, `base_unit` = `dose`/`ml`; `vaccines` detail table (`manufacturer, default_dose_ml, requires_booster, booster_interval_days, storage_temp_min/max, withdrawal_period_days`) FK→`inventory_items`.
- `inventory_stock` lots (running balances, **`numeric` + `quantity_unit`**, never int — engine is shared with feed which is kg/litre): `lot_number, expiry_date, quantity_in_stock numeric, quantity_reserved numeric, quantity_consumed numeric, quantity_unit`, park scope via `location_id`, **`CHECK(quantity_in_stock >= 0)`**.
- **`inventory_stock_movements`** append-only ledger (reserve/release/consume/adjust/transfer/expire; `quantity numeric`) — the audit/reconciliation truth; balances are its projection. FEFO pick in app, expiry-gated (`expiry_date >= administered_at`).

**Consumption mode (resolves the v1 contradiction — pick by path, never both):**
- **Group shed drive:** at **batch start** record one `reserve` movement for `planned_quantity` against the FEFO lot (`reserved_quantity += n`). At **drive close / verification** record `consume` for actual used + `release` for the remainder. **No per-animal decrement.**
- **Manual campaign / catch-up**: Preventive Care approved catch-up creates canonical obligations/batches before execution; ad hoc per-animal individual override generation is not exposed in Phase 0.

- `[P2]` `inventory_stock.is_quarantined` + a cold-chain excursion table for the breach→quarantine cascade.

---

## 6. Edge-case matrix (must pass before "done")

| Case | Handling |
|---|---|
| Animal shifted mid-schedule | obligation `scope_id`/`batch_id` re-target to destination shed; eligibility re-evaluated against new shed's `animal_stage`; same-vaccine only |
| Animal dies/sold | `herd_animals.exited_at` set ⇒ same-txn `obligation_instances` for that animal → `canceled`; status event logged |
| Missed vs blocked | `missed` (window passed) vs `waived`/blocked with reason (ICU/quarantine/stock-out) — distinct statuses |
| Double-submit completion | `UNIQUE(tenant_id,obligation_id,animal_id)` + `idempotency_key` → no-op |
| Stock-out mid-drive | reserve-at-start surfaces shortfall; `CHECK(>=0)` blocks negative |
| Expired lot | FEFO pick gated by `expiry_date` |
| Config change in flight | new inactive/draft version until activated; activation supersedes only open future work per preview; completed history keeps its version |
| Multi-park, different rule | park-scoped active `protocol_version` overrides company for that park only; company remains active for non-overridden parks |
| Far-future booster | `obligation_instances` row in Postgres (queryable), enqueued to Cloud Tasks only near due |

## 6A. Later drive planner algorithm

V1 generation remains the source of truth for due work: the complete
CEO/COO-published vaccine-animal matrix plus animal facts creates or updates
`obligation_instances` for each animal. The later planner consumes those rows and
produces optimized execution-ready `obligation_batches` without recomputing the
whole herd.

V1 also owns basic shed-drive batching and Calendar de-duplication. When V1
attaches due rows to an `obligation_batches` shed drive, Calendar projects the
drive row as active operations work and suppresses the batched per-animal
`vaccination_dose_due` projections from active Calendar. The per-animal rows stay
available to detail and audit surfaces.

**Inputs**
- `obligation_instances` for vaccination rules, including `rule_id`,
  `protocol_version_id`, `due_at`, `window_start`, `window_end`, target animal,
  scope shed/cohort, status, and trusted completion suppression.
- Animal facts used by eligibility and replanning: species, breed, lifecycle,
  exit reason, health, reproductive status, management stage, DOB/age, sex,
  current park/shed/cohort, Animal ID 1/2 replacement state, and governed
  shed/tag validity.
- Rule config: schedule rows, eligibility JSON, defer states, min gaps,
  due-window policy, missed-dose policy, proof policy, SOP binding, withdrawal
  days, and the V1 vaccine compatibility policy.
- Operational facts: available stock/lot/cold-chain, trained worker/verifier,
  route capacity, proof requirements, and active drive locks.

**Algorithm**
1. Select due candidates through indexed windows, never an unbounded animal scan:
   `(tenant_id, status, due_at)` plus scope filters. Candidates are active
   `scheduled`/`due` obligations, plus `deferred` obligations whose current animal
   facts now satisfy a derived ready-again predicate. Readiness after defer is a
   computed predicate, not a stored canonical status.
2. Revalidate each candidate against current animal facts. Dead, sold,
   transferred, culled, or lost animals cancel open work. Sick, quarantine, ICU,
   pregnancy, and lactation states apply the rule's allow/defer/block/review
   policy. Shed shifts re-scope only same-vaccine open work. V1 already enforces
   pregnancy/lactation from current facts during generation/backfill; a
   standalone `animal.reproductive_status.changed` recheck event is a V2/provenance
   extension until a producer and consumer are shipped.
3. Bucket candidates by:
   `(tenant_id, park, shed_or_cohort, protocol_version_id, rule_id, dose_code,
   due_window_bucket, eligibility/defer_state)`. This reduces million-animal
   planning from animal-level iteration to bounded cohort work.
4. For each shed/time bucket, build a vaccine conflict graph. Each vaccine or
   dose family is a node. An edge means the two nodes cannot be executed in the
   same drive because of live/killed compatibility, same-vaccine gap, required
   2-week/4-week separation, route/site restriction, or unreviewed compatibility
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
   is allowed only when every animal in the merged candidate remains inside its
   medical safe window. If a +1 week hold would pass any animal's `last_safe_date`,
   the small shed becomes a micro-drive now. Example: 5 due K1 animals in one CBE
   shed and 15 compatible K1 animals in another CBE shed may become one 20-animal
   drive only if the 5 animals can medically wait through that hold.
7. Score safe candidates deterministically. Hard constraints are not scores.
   Scored factors are urgency/earliest deadline, animals covered, disease
   priority, stock expiry, worker/route efficiency, cold-chain route duration,
   and fairness to small sheds that have already waited.
8. Pick the best candidate with stable tie-breakers:
   earliest deadline, higher medical priority, more animals covered, expiring
   stock, lower route cost, then oldest waiting shed bucket.
9. Assign resources under transaction/lease control: create or update
   `obligation_batches`, attach obligations, reserve stock where vaccination
   policy requires it, create the SOP task, and store worker/verifier/proof
   requirements. Concurrent planners must use idempotency keys and row locks so
   two workers cannot claim the same obligations.
10. Execute and reconcile by scan. Missing animals stay open/missed/follow-up;
   shifted-in eligible animals become explicit extras; shifted-out animals move to
   the destination bucket; newly sick/pregnant/quarantined animals defer or block;
   deaths/sales cancel; unreadable tags create identity exceptions; proof
   rejection and cold-chain failure create rework.
11. Replan incrementally. Events such as `animal.exited`,
    `animal.location.changed`, `animal.health.changed`, future/proven
    `animal.reproductive_status.changed`, `proof.rejected`, `stock.shortfall`, or
    `cold_chain.failed` invalidate only the affected animal, bucket, vaccine
    group, and batch. The whole herd is never recalculated for one state change.

**Boundary**
The later optimizer requires the V1-authored vaccine compatibility fields plus
policy-approved drive thresholds. V1 owns per-animal due work, the authored matrix
rules, compatibility spacing, basic shed batching, and Calendar drive-first
projection. The later optimizer owns route/resource scoring, multi-shed
planning, and large-scale incremental replanning. Separately, V1 itself must
not be called complete until the evidence-derived vaccine-animal matrix in §4.0 is
configured and proven.

---

## 7. Migration plan (Path B base correction)

V1 dev/test cutover is **clean slate**. Build the target model first, then wipe
and reseed local/dev/test operational data through the new model. The order is:
`parks -> sheds -> animal_stage_lookup/shed-tag species policy -> species_catalog
-> breeds -> herd_animals -> vaccination history/protocol facts -> active
vaccination matrix -> obligations`. Existing goat-only rows are treated as
source evidence for the reseed; they are not preserved as runtime truth and are
not patched in place. For production cutover later, the same importer rules
apply, but unresolved source rows block instead of receiving fixture defaults.

Clean slate also means the final V1 schema, generated SQLC snapshots, OpenAPI
contracts, generated clients, seed data, and admin-web copy must not carry the
old dashboard/BQ-port model. Do not recreate or expose:

- `legacy_import_*`, `legacy_sync_*`, import-review queues, sync run conflicts,
  watermarks, or old dashboard source freshness/status widgets.
- BQ snapshot mirror tables such as `counts_current_snapshot_rows`,
  `counts_sync_runs`, and `mortality_sync_runs` for the vaccination/herd base.
- `goat_identity_counters` and counter projection tables/views from the old
  dashboard/reporting port.
- `identity_state` review/dispute/clean states, `source_confidence`, or
  identifier-policy review/reject machinery on canonical herd animals.
- `origin_type='unknown'` on accepted/canonical herd animals.
- Identifier names/types such as `old_tag`, `sheet_row_id`, and
  `external_system_id` in canonical APIs. Use `animal_identifier_1` and
  `animal_identifier_2`.
- Location/source enums such as `legacy_bq_dashboard_shed`,
  `legacy_bq_counts`, `legacy_bq_mortality`, or `legacy_bq` in active contracts.

The only allowed duplicate/identity handling in the target herd model is hard
validation before creation/import and an explicit replacement/merge history if a
human-corrected canonical duplicate must be retired. It must not be modeled as a
long-lived dirty-data state on the animal row. Fresh procurement/admin create
flows reject invalid or conflicting input immediately; they do not park bad
animals in GoatOS for later conflict resolution.

Identifier lifetime/replacement rules:

- `animal_identifier_1` and `animal_identifier_2` values are globally unique
  forever. The uniqueness check is against current identifiers plus all
  historical retired/broken/lost identifiers, including animals that are dead,
  sold, transferred, merged, or otherwise exited.
- The two current identifier slots on the same accepted animal are required and
  must contain two different values.
- Do not scope new identifiers by park, shed, source sheet, or species. If the
  same value appears anywhere in history, it already belongs to that animal and
  cannot be assigned to another one.
- If a physical tag falls off or breaks, mark that identifier slot as
  `replacement_required`/`broken` in identifier history, keep the old value
  linked to the same animal forever, and keep the animal operational through the
  surviving identifier. Vaccination, shifting, feed, and health workflows must
  not block merely because one of the two tags is awaiting replacement.
- Replacement issues a brand-new globally unused identifier value into the
  vacant slot and writes an atomic audit event with old value, new value, actor,
  time, reason, and proof where available. Broken/fallen values are never
  refitted, reissued, or reused.
- Database enforcement must include both checks: current slot 1 and slot 2
  cannot normalize to the same value, and identifier value uniqueness must be a
  lifetime reservation across active plus historical rows. An active-only unique
  index is not enough because death, sale, transfer, merge, or tag replacement
  must not release the value.
- Any identifier replacement updates the current slot, identifier history, and
  audit event in one transaction. No workflow may write a new current value
  without reserving that value globally first.

Local/dev/test reseed rules:

- Seed at least goat and sheep. Species is derived from governed species/breed
  rules, e.g. Anantapur Sheep is sheep; goat breeds stay under goat; ambiguous
  labels go to seed review and must not silently default to goat.
- Every seeded animal must have `animal_identifier_1` and
  `animal_identifier_2`. These are two parallel animal IDs for every goat,
  sheep, and future species, not old/new IDs. Local/dev/test may generate
  deterministic fixture identifiers only with seed/test provenance; production
  rows block until both real identifiers are known. Identifier fixture values
  must still be globally unique and must never be reused across any seeded
  current or historical animal. Raw legacy source column names stay in import
  provenance only.
- Every seeded animal must have `sex in ('female','male')`. If source sex is
  missing in a dev/test fixture row, choose a deterministic fixture value from
  a documented seed rule and mark seed/test provenance. Never store `unknown`,
  never infer from F2 labels for production truth, and never create vaccination
  obligations for unresolved production rows.
- Every seeded animal must resolve to one current park, shed, and governed
  shed/tag whose allowed-species and stage-sex policy accepts that animal.
  Invalid species/tag or sex/tag combinations are rejected during seed.
- Vaccination history belongs to `animal_id` for both goats and sheep. Dev/test
  fixture history may be generated only from verified source evidence or
  explicit synthetic scenarios derived from the active matrix; production
  completions are never fabricated.

| Migration | Contents |
|---|---|
| `species_catalog` migration | Add governed species reference data and seed at least `goat` and `sheep`; no static goat/sheep enum in code. |
| `breed_species` migration | Ensure every breed belongs to one species; seed Anantapur Sheep as sheep and goat breeds under goat; add alias/review path for dirty source labels. |
| `herd_animals_identity` migration | Create canonical `herd_animals` with `animal_id`, required `animal_identifier_1`, required `animal_identifier_2`; wipe/reseed local/dev/test herd data from verified sources through the clean-slate importer; replace `goats_species_check`; add DOB confidence, origin/entry, lifecycle/exit, current location/shed/tag fields, and merge target `merged_into_animal_id`. Do not copy dirty goat-only state forward as runtime truth. Identifier values remain reserved forever after animal exit. |
| `animal_identifiers_history` migration | Rename/create `animal_identifiers`, `animal_location_history`, and `animal_identity_events`; migrate FK references from goat names to animal names; map legacy source identifier columns into `animal_identifier_1`/`animal_identifier_2` or provenance aliases through a reviewed importer. Add global lifetime uniqueness over identifier value across active and historical rows; support replacement states for broken/fallen tags without making the animal operationally invalid. Canonical names must not be old/new identity. Tagging state stays derived, not stored. |
| `old_dashboard_schema_removal` migration | Remove old dashboard/BQ-port schema and contracts from the final V1 database shape: no `legacy_import_*`, `legacy_sync_*`, BQ snapshot mirrors, sync freshness tables, goat identity counters, `identity_state`, `source_confidence`, old tag/sheet/external identifier types, or legacy BQ location source contexts. |
| `shed_tag_age_policy` migration | Extend/seed `animal_stage_lookup` with source age range label, source day numbers, normalized age days, allowed species, max residence days, purpose, and status. |
| `vaccination_animal_targets` migration | Change vaccination/obligation/completion FKs and OpenAPI contracts from `goat_id`/`target_type='goat'` to `animal_id`/`target_type='herd_animal'`; add `animal_protocol_facts`. |
| `protocol_rule_dimensions` migration | Compile matrix selectors such as species, breed, stage/tag, age days, sex, health, reproductive/lactation, and procurement path for indexed impact/generation. |
| `vaccination_module` migration | Keep `vaccination_completions`, inventory, SOP, and protocol engine concepts, but bind completions to `animal_id` and matrix rows to mixed-species animals. |

**Reuse exemplars:** `000001` (RANGE partition + outbox + idempotency), `000030` (projection contract + `COALESCE` unique precedent), `000050` (RBAC + capabilities), `000060` (SOP engine, task state machine).

**Spec the 7 state machines before coding** — see [state-machines.md](../protocol-engine/state-machines.md). **Preventive Care (PC) / Vaccination uses SM-1/2/3/4/5/7**; **Feed adds SM-6**: SM-1 schedule generation, SM-2 shift recompute, SM-3 death/sale cancel, SM-4 batch lifecycle, SM-5 stock reserve/consume/release, SM-6 feed generation, SM-7 booster generation. Agree these before SQL.

---

## 8. Keep / enhance / ditch
- **KEEP (reuse-as-is):** locations + operational_attributes + capacity_records, workforce_*, user_scope_grants, audit_log, outbox_messages, idempotency_keys, sop_*.
- **REPLACE / RENAME:** `goats`, `goat_identifiers`, `goat_location_history`, `goat_identity_events`, `goat_id`, `target_type='goat'`, `goat.created`, and frontend/domain `Goat*` types become `herd_animals`, `animal_identifiers`, `animal_location_history`, `animal_identity_events`, `animal_id`, `target_type='herd_animal'`, `animal.created`, and Animal/HerdAnimal language.
- **ENHANCE:** locations (+profiles), `animal_stage_lookup` age/tag policy, feature_coverage_registry (+vaccination).
- **DITCH completely from the final V1 shape:** old dashboard/BQ-port schema,
  `legacy_import_*`, `legacy_sync_*`, BQ snapshot mirrors, sync freshness/status
  tables, goat identity counters/projection views, `identity_state`
  review/dispute states, `source_confidence`, old tag/sheet/external-system
  identifier types, and dashboard-parity-with-BigQuery thinking. Control Tower
  reads GoatOS Postgres/projection state only.

## 9. Source-derived local/dev baseline and later inputs
- Active V1 matrix baseline: use [source-nuances-rules.md](./source-nuances-rules.md)
  for the Config preset and generation contract: ET+TT at 28 and 49 days with
  2 ml, PPR at 112 days, FMD/HS at 84 days, Goat Pox at 140 days to honor
  live-live spacing, plus adult revaccination intervals. The older
  ET/K1/day-21/0.5 ml proof fixture is historical local proof context only.
- Local/dev shed-tag baseline: seed/read Goats and Parks source age ranges from
  `animal_stage_lookup`/tag policy: K0 1-2 source days, K1 3-9, K2 10-77,
  K3 78-84, fattening kid tags 120-240, and adult tags 300+. Store normalized
  zero-based age days for query predicates and keep max-stay notes separate.
- Local/dev herd baseline: reseed animals through `herd_animals`, not the old
  goat-only table shape. Unknown source values in test data must be resolved to
  rule-valid fixture values or blocked from the seed. Sex is always female/male;
  species is always resolved through the governed catalog; every animal has
  `animal_identifier_1` and `animal_identifier_2`; vaccination history is linked
  to `animal_id` and seeded only from source evidence or explicit synthetic
  matrix scenarios.
- Later production expansion changes the active matrix by publishing/activating
  a new scoped version. It must not create one top-level protocol per vaccine.
