# Preventive Care Vaccination Rule Matrix Authoring Handoff

**Date:** 2026-07-02
**Audience:** UI mock builder, backend/schema implementer, PR reviewer
**Scope:** Correct the Config rule authoring model before building the final
page. Vaccination is the first concrete category; the same pattern must support
Feed Direction and later protocol categories.

---

## 1. Senior architect correction

The rule JSON is **not** the place to list herd-animal required fields. Rule JSON
is authored policy. Herd-animal/shed/procurement/vaccination-history properties
are canonical database facts and derived read-model facts.

Wrong direction:

```json
{
  "embedded_herd_animal_field_checklist": [
    "species",
    "breed",
    "sex",
    "dob",
    "current_shed_id",
    "vaccination_history"
  ]
}
```

That mixes policy storage with target data. It also cannot scale because every
rule publish, animal edit, or shed change would tempt the engine to re-interpret
raw JSON against live tables in an unbounded way.

Correct direction:

1. Store the **scoped rule matrix policy** in `protocol_versions.rule_dsl`.
2. Keep one logical vaccination ruleset family, for example
   `vaccination.matrix`; do not create one top-level protocol per vaccine.
3. Expand/compile the matrix into `protocol_rules` and optional predicate rows.
4. Resolve the active version by category + scope before generation.
5. Maintain indexed target facts from canonical data, for example
   `animal_protocol_facts` for animal-targeted protocols.
6. Evaluate only affected animals/cohorts/sheds when a fact changes.
7. Group human execution through `obligation_batches`, never one task per animal.

## 2. Existing schema anchors

The current repo has important foundations, but the target model is Path B:
canonical `herd_animals` / `animal_id`, not goat-only physical storage.

| Need | Current home |
|---|---|
| Herd animal species, breed, sex, DOB/age, lifecycle, health, reproductive state | `herd_animals` target model, migrated from current `goats` implementation |
| Current park/shed/cohort | `herd_animals.current_location_id`, `herd_animals.park_id`, `herd_animals.shed_id`, `herd_animals.cohort_id`, `locations` |
| Shed tag/stage | `shed_profiles.animal_stage_id`, `animal_stage_lookup.stage_code` from `000071` |
| Protocol version and authored rule JSON | `protocol_versions.rule_dsl` from `000073` |
| Expanded dose/rule rows | `protocol_rules` from `000073` |
| Per-animal due state | `obligation_instances` from `000074` |
| Shed drive / grouped work | `obligation_batches` from `000074` |
| Accepted vaccination history | `vaccination_completions` from `000075` |
| Procurement accepted intake and warm-up/history evidence | `procurement_pc_handoffs`, `procurement_hf_vaccination_evidence` |

## 3. Required DB direction

### 3.1 Keep source-of-truth normalized

Do not add display-only "category" strings as the only join between rules and
herd facts. The stable joins are:

| Join element | Use |
|---|---|
| `tenant_id` | mandatory tenant boundary |
| `protocol_version_id`, `rule_id` | rule identity and completion/history anchor |
| `animal_id` | animal-targeted protocols such as vaccination |
| `location_id` / `shed_id` / `cohort_id` | shed/cohort-targeted protocols such as feed direction |
| `breed_id` plus alias display | breed dimension |
| `animal_stage_id` plus `stage_code` | shed tag/stage dimension |
| `species_code`, `sex`, lifecycle, health, reproductive state | rule predicate dimensions |
| `entry_date`, `origin_type`, procurement handoff/evidence | procurement and warm-up dimensions |

### 3.2 Add target-facts read models for scale

For vaccination, add or maintain a derived table/view like:

```text
animal_protocol_facts
  tenant_id
  animal_id
  species_id
  species_code
  breed_id
  breed_key
  sex
  dob
  dob_confidence
  age_days_as_of
  lifecycle_status
  health_status
  reproductive_status
  pregnancy_month
  lactation_state
  origin_type
  procurement_path
  herd_entry_date
  warmup_until
  park_id
  shed_id
  cohort_id
  animal_stage_id
  stage_code
  facts_hash
  computed_at
```

This is a read model, not canonical truth. It is invalidated by animal CRUD,
current-shed/stage change, health/reproductive change, procurement accepted
intake, warm-up expiry, and accepted vaccination completion. Index it for the
dimensions used by active protocol categories:

```text
(tenant_id, species_code, stage_code, lifecycle_status, health_status)
(tenant_id, shed_id, stage_code, lifecycle_status)
(tenant_id, breed_id, sex, lifecycle_status)
(tenant_id, warmup_until)
(tenant_id, animal_id)
```

Feed Direction should use the same idea with a shed/cohort target-facts read
model, not vaccination-specific columns.

### 3.3 Compile rule predicates when needed

`rule_dsl` is the authored payload. For impact preview and million-animal
generation, compile matrix rows into SQL-selectable predicates when JSON
evaluation becomes too slow:

```text
protocol_rule_dimension_values
  tenant_id
  protocol_version_id
  rule_id
  dimension_key
  include_exclude
  value_kind
  uuid_value
  text_value
  int_min
  int_max
  sort_order
```

Example dimension keys: `species_code`, `breed_id`, `stage_code`, `animal_stage_id`,
`sex`, `lifecycle_status`, `health_status`, `reproductive_status`,
`pregnancy_month`, `lactation_state`, `procurement_path`, `age_days`.

This table is derived from the rule JSON and regenerated on version publish. It
is not another hand-edited source of truth.

### 3.4 Add scoped active-version resolution

Vaccination must be configured as one active ruleset per scope, not many active
vaccine rows. The generic engine should support multiple policies, but
vaccination uses:

```text
category = vaccination
protocol_code = vaccination.matrix
scope_resolution_mode = tenant_default_with_park_overrides
active_cardinality = single_active_ruleset_per_scope
override_semantics = park_replaces_tenant_for_that_park
```

Recommended additions or contract surfaces:

```text
protocol_category_scope_policies
  tenant_id
  category
  protocol_code
  target_type
  allowed_scope_levels
  scope_resolution_mode
  active_cardinality
  override_semantics
  activation_cutover_policy
  status

protocol_scope_resolutions
  tenant_id
  category
  protocol_id
  target_scope_type
  target_scope_id
  active_protocol_version_id
  source_scope_type
  source_scope_id
  overridden_protocol_version_id
  resolution_reason
  computed_at
```

For vaccination:

- only one company-wide active version can exist;
- only one active park version can exist for a given park;
- an active park version excludes only that park from the company version;
- the company version continues to apply to every park without an active park
  override;
- draft/inactive/retired versions stay in history and never generate new work.

If the current schema keeps `status='published'`, expose a separate
`activation_state` in API/UI (`draft`, `scheduled`, `active`, `inactive`,
`retired`). The user-facing Config list should say active/inactive because the
business action is selecting the currently active ruleset for a scope.

## 4. Correct UI model

The Config page is a generic Admin/Data Ops screen at `/config`, filtered by
`category=vaccination` for this module. V1 visibility is CEO/COO/superadmin
only. Remove source/review authoring sections from the UI. Show normal version
audit only: version, created by, created time, activated/published by,
activated/published time, effective from/to, inactive/retired time where
applicable.

The first Config screen is the scoped ruleset list, not a vaccine-row list.
For vaccination it should show:

| Row | Meaning |
|---|---|
| Company-wide default | The active `vaccination.matrix` version for all parks without active park overrides |
| Park override rows | The active `vaccination.matrix` version for a specific park |
| History drawer | Draft, scheduled, inactive, and retired versions for the selected scope |

Top-level list rows must not be ET+TT, PPR, Goat Pox, FMD, or HS. Those are
matrix cells inside the selected active version.

### Layout

1. Header: category selector, scope mode (Company-wide / Park-wise), active
   version, effective date, activation state, audit summary.
2. Ruleset list: company default row plus active park override rows, with
   applies-to summary, excluded park count, override count, last activator, and
   history.
3. Matrix tab: rows are target cohorts; columns are vaccines/doses; cells open
   a drawer.
4. Cohort builder rail: species, breed, sex, shared shed tag/stage, lifecycle, health,
   reproductive, procurement/warm-up, age bounds.
5. Vaccine catalog rail: code, label, class, pathogen, dose amount, vial size,
   revaccination interval, inventory item binding.
6. Compatibility tab: live/killed gaps, same-day allowed combinations,
   same-vaccine minimum gaps, ET+TT kid/adult course booster minimum gap.
7. Impact preview: affected animals, deferred animals, excluded animals, due rows,
   catch-up rows, stock estimate, batch estimate, missing data blockers, open
   obligations to supersede, and in-progress batches requiring review.
8. JSON rail: shows `rule_dsl` only. It must not show animal rows or an
   embedded herd-animal field checklist.

### Matrix behavior

- A row is a cohort selector, not a vague category.
- A cell is the rule for one cohort x vaccine/dose family.
- The cell drawer sets action: `due`, `not_applicable`, `defer`, `block`, or
  `review`.
- The cell drawer sets timing: trigger, offset, earliest/ideal/latest window,
  min gap, max delay, repeat/revaccination.
- Species, breed, and shed tag must be first-class dimensions in the same row, so the UI
  can express combinations like "Jamunapari female, K2, healthy, not pregnant"
  differently from "all breeds, quarantine shed tag".
- Pregnancy, lactation, ICU, quarantine, under-treatment, warm-up, and
  procurement path are rule dimensions, not hidden text notes.

## 5. Prompt for the UI/mock builder

Use this prompt:

```text
Rebuild the vaccination Config surface as a scoped ruleset manager plus matrix
authoring tool, not a vaccine-first flat form and not one protocol per vaccine.

Context:
- The page is /config filtered by category=vaccination.
- It is visible only to CEO/COO/superadmin.
- Remove all source/review UI fields. Keep only version/audit fields:
- version, created by/time, activated by/time, effective from/to, inactive time.
- Vaccination has one logical ruleset family: vaccination.matrix.
- Company-wide has at most one active version.
- Each park has at most one active park override.
- A park override excludes only that park from the company version.
- Rule JSON stores policy only. Herd-animal fields come from DB facts and must
  not appear as an embedded required-fields list in the JSON.
- Herd animals are mixed-species. Goat and sheep can share the same shed/tag;
  species is a selector inside the matrix, not a separate product tab.
- Drive planning is park-level optimization, not one tiny drive per shed. Config
  and preview must show the combined doctor-visit target plus per-shed/tag
  counts. Compatible kid goat+sheep groups may be combined; adult goat and adult
  sheep work must remain species-specific execution groups inside the same park
  visit.

UI requirements:
- First screen is the scoped ruleset list.
- Show one Company-wide row with applies-to summary like "All parks except 2
  park overrides".
- Show active Park override rows with scope label, active version, last
  activator, effective date, and history.
- Opening a row shows the matrix.
- Rows are target cohorts built from dimensions: species, breed, sex, shed
  tag/stage, lifecycle, health, reproductive state, procurement/warm-up,
  age bounds.
- Columns are vaccines/dose families: ET+TT, PPR, Goat Pox, Sheep Pox, Blue
  Tongue, FMD, HS, and future
  rows from the catalog.
- Each cell opens a drawer to set action, trigger, offset, earliest/ideal/latest
  window, min gap, max delay, repeat/revaccination, proof/SOP, and defer/block
  states.
- Add a separate Compatibility tab for live/killed gaps and same-day allowance.
- Add Impact Preview that counts affected animals, deferred animals, excluded animals,
  due rows, catch-up rows, stock estimate, batch estimate, and missing-data
  blockers. When activating a park override, include company-version open
  obligations to supersede and in-progress batches requiring explicit choice.
- Add JSON rail showing rule_dsl only.
- Do not make "Who qualifies" a single category picker or a goat-only tab. It
  must allow all dimensions together so species x breed x shed tag x vaccine x
  reproductive/health state permutations can be represented.
- Do not create a category, selector, tab, seed row, API field, import prompt,
  fallback schedule, or UI option based on mother vaccination status. The V1
  business policy assumes mothers are kept vaccinated; missing, unknown, or
  not-vaccinated mother evidence is ignored for scheduling and the approved
  standard kid schedule is always used.
- Treat `mother`/`lactating` as biological state that can apply to goat and
  sheep mothers for vaccination. Treat `Mother Milking Waiting`,
  `Milking Warmup`, and `Milking` as commercial goat-milk workflow tags; do not
  show them for sheep unless a future approved sheep dairy policy adds them.
- Treat `BUCK` as the shared adult-male breeder tag for vaccination eligibility
  across goat and sheep unless operations later creates species-specific
  male-breeder tags.
- Stage/tag options must come from the governed Goats and Parks tag catalog plus
  stage-species policy. When species is sheep, the UI must not show commercial
  goat-milking tags as selectable category/stage values. If an existing legacy
  row violates the species/tag policy, show it as a Data Ops blocker, not as a
  selectable normal state.

Design:
- Dense operational UI, not marketing.
- No oversized cards, no explanatory in-app text, no source/review sections.
- Use tables, segmented controls, select menus, checkboxes/toggles, chips, and
  drawers.
```

## 6. Prompt for backend/schema implementation

Use this prompt:

```text
Implement the protocol rule-matrix contract without mixing rule JSON and herd
facts.

Required:
- Treat protocol_definitions as stable ruleset families. For vaccination use
  vaccination.matrix, not one protocol per vaccine/stage/breed copy.
- Persist authored policy in protocol_versions.rule_dsl.
- Expand schedule/cell rows to protocol_rules.
- Keep herd-animal/shed/procurement/completion data in canonical tables.
- Add category scope policy and active-version resolution for tenant default
  plus park overrides.
- For vaccination enforce one active company version and one active version per
  park. Park overrides replace the company version for that park only.
- Add/maintain animal_protocol_facts as an indexed read model for vaccination
  evaluation.
- Add protocol_rule_dimension_values if impact preview/generation needs
  SQL-selectable predicates at 1M-goat scale.
- Recompute only affected targets on animal CRUD, shed/stage change,
  health/reproductive change, procurement accepted-intake, warm-up expiry, and
  accepted vaccination completion.
- Activation/publish requires CEO/COO/superadmin authority, valid JSON schema,
  published SOP binding where needed, non-overlapping effective dates, active
  cardinality checks, scope override resolution, and impact preview.
- Add a governed stage/species policy store for `animal_stage_lookup`, exposed to
  APIs as allowed species per tag/stage. Enforce it on animal creation/import,
  shed/stage movement, rule authoring, rule publish/activation, and impact
  preview. Invalid species/tag pairs fail with a stable validation error such as
  invalid_species_for_stage; legacy violations are Data Ops blockers.
- Rule matching must validate each row's species x stage cross-product against
  stage_species_policy before vaccine cells run. Do not allow sheep rows to carry
  `MOTHER_MILKING_WAITING`, `MILKING_WARMUP`, `MILKING`, or any future goat-only
  commercial milking tag. `BUCK` is not goat-only for vaccination; it is a shared
  adult-male breeder tag unless replaced by explicit species-specific tags.
- Add tests for API/import rejection, UI option filtering, publish-time matrix
  validation, generation blocking when a sheep record references a goat-only
  commercial milking stage, and successful sheep + `BUCK` matching.
- Activating a park override must supersede/recompute open company-version work
  for that park only; completed history keeps its original protocol_version_id;
  in-progress batches require explicit operator choice.
- Remove source/review UI and runtime publish gates from config authoring.
  Source docs stay engineering reference only.
- Generation must page through indexed facts and write idempotent
  obligation_instances. Human execution remains grouped through
  obligation_batches.
```

## 7. Rule storage sample

### 7.1 `protocol_versions` row wrapper

This audit/version data is table metadata, not rule-policy JSON:

```json
{
  "protocol_version_id": "pv_vaccination_2026_07_02_v1",
  "protocol_code": "vaccination.matrix",
  "protocol_name": "Vaccination Rule Matrix",
  "category": "vaccination",
  "scope_type": "tenant",
  "scope_id": null,
  "scope_label": "Company-wide",
  "version": 1,
  "activation_state": "active",
  "effective_from": "2026-07-02",
  "effective_to": null,
  "created_by": "ceo_user_id",
  "created_at": "2026-07-02T09:30:00Z",
  "activated_by": "ceo_user_id",
  "activated_at": "2026-07-02T10:00:00Z",
  "applies_to_summary": "All parks except active park overrides",
  "excluded_scope_count": 0
}
```

### 7.2 `rule_dsl` policy JSON

This is the JSON policy stored in `protocol_versions.rule_dsl`. It has no animal
snapshots and no embedded herd-animal field checklist.

Hard validation rules for this sample and the production schema:

- `sex` is only `female` or `male`; `unknown`, blank, or inferred sex is rejected
  before canonical animal creation/import and must never be offered in UI rule
  authoring or used during vaccination matching.
- `stage_species_policy` and `stage_sex_policy` are enforced before row matching.
  They are not display hints. A row/cell that would create an impossible
  species/tag or sex/tag combination fails validation and cannot publish.

```json
{
  "schema_version": "protocol.rule_matrix.v1",
  "category": "vaccination",
  "target_type": "herd_animal",
  "scope": {
    "type": "tenant",
    "id": null
  },
  "dimensions": {
    "species_codes": ["goat", "sheep"],
    "breed_ids": ["*"],
    "sex": ["female", "male"],
    "stage_codes": [
      "K0",
      "K1",
      "K2",
      "K3",
      "FATTENING_MALE",
      "FATTENING_FEMALE",
      "NON_PREGNANT",
      "PREGNANT_EARLY",
      "PREGNANT_LATE",
      "MOTHER",
      "MOTHER_MILKING_WAITING",
      "MILKING_WARMUP",
      "BREEDING",
      "MILKING",
      "BUCK"
    ],
    "stage_species_policy": {
      "K0": ["goat", "sheep"],
      "K1": ["goat", "sheep"],
      "K2": ["goat", "sheep"],
      "K3": ["goat", "sheep"],
      "FATTENING_MALE": ["goat", "sheep"],
      "FATTENING_FEMALE": ["goat", "sheep"],
      "NON_PREGNANT": ["goat", "sheep"],
      "PREGNANT_EARLY": ["goat", "sheep"],
      "PREGNANT_LATE": ["goat", "sheep"],
      "BREEDING": ["goat", "sheep"],
      "MOTHER": ["goat", "sheep"],
      "MOTHER_MILKING_WAITING": ["goat"],
      "MILKING_WARMUP": ["goat"],
      "MILKING": ["goat"],
      "BUCK": ["goat", "sheep"]
    },
    "stage_sex_policy": {
      "K0": ["female", "male"],
      "K1": ["female", "male"],
      "K2": ["female", "male"],
      "K3": ["female", "male"],
      "FATTENING_MALE": ["male"],
      "FATTENING_FEMALE": ["female"],
      "NON_PREGNANT": ["female"],
      "PREGNANT_EARLY": ["female"],
      "PREGNANT_LATE": ["female"],
      "BREEDING": ["female"],
      "MOTHER": ["female"],
      "MOTHER_MILKING_WAITING": ["female"],
      "MILKING_WARMUP": ["female"],
      "MILKING": ["female"],
      "BUCK": ["male"]
    },
    "health_states": ["healthy", "recovering", "sick", "under_treatment", "quarantine", "icu"],
    "reproductive_states": ["non_pregnant", "pregnant", "lactating", "mother", "buck"],
    "procurement_paths": ["farm_born", "procured", "imported"]
  },
  "vaccine_catalog": [
    {
      "vaccine_code": "ET_TT",
      "label": "ET+TT",
      "class": "killed",
      "pathogen_class": "bacterial",
      "inventory_item_code": "vaccine.et_tt",
      "dose_amount": 2,
      "dose_unit": "ml",
      "vial_doses": 100,
      "revaccination_days": 182,
      "priority": 1
    },
    {
      "vaccine_code": "PPR",
      "label": "PPR",
      "class": "live",
      "pathogen_class": "viral",
      "inventory_item_code": "vaccine.ppr",
      "dose_amount": 1,
      "dose_unit": "ml",
      "vial_doses": 100,
      "revaccination_days": 1095,
      "priority": 2
    },
    {
      "vaccine_code": "GOAT_POX",
      "label": "Goat Pox",
      "class": "live",
      "pathogen_class": "viral",
      "inventory_item_code": "vaccine.goat_pox",
      "dose_amount": 1,
      "dose_unit": "ml",
      "vial_doses": 25,
      "revaccination_days": 365,
      "priority": 3
    },
    {
      "vaccine_code": "SHEEP_POX",
      "label": "Sheep Pox",
      "class": "live",
      "pathogen_class": "viral",
      "inventory_item_code": "vaccine.sheep_pox",
      "dose_amount": 1,
      "dose_unit": "ml",
      "vial_doses": 100,
      "revaccination_days": 365,
      "priority": 3
    },
    {
      "vaccine_code": "BLUE_TONGUE",
      "label": "Blue Tongue",
      "class": "killed",
      "pathogen_class": "viral",
      "inventory_item_code": "vaccine.blue_tongue",
      "dose_amount": 2,
      "dose_unit": "ml",
      "vial_doses": 100,
      "revaccination_days": 365,
      "priority": 4
    },
    {
      "vaccine_code": "FMD",
      "label": "FMD",
      "class": "killed",
      "pathogen_class": "viral",
      "inventory_item_code": "vaccine.fmd",
      "dose_amount": 1,
      "dose_unit": "ml",
      "vial_doses": 30,
      "revaccination_days": 274,
      "priority": 5
    },
    {
      "vaccine_code": "HS",
      "label": "HS",
      "class": "killed",
      "pathogen_class": "bacterial",
      "inventory_item_code": "vaccine.hs",
      "dose_amount": 2,
      "dose_unit": "ml",
      "vial_doses": 100,
      "revaccination_days": 365,
      "priority": 6
    }
  ],
  "matrix_rows": [
    {
      "row_key": "kid_shared_course_all_species",
      "label": "Goat and sheep kids, shared course, all breeds",
      "criteria": {
        "species_codes": ["goat", "sheep"],
        "breed_ids": ["*"],
        "sex": ["female", "male"],
        "stage_codes": ["K1", "K2", "K3"],
        "age_days": {
          "min": 0,
          "max": 140
        },
        "lifecycle_status": ["alive"],
        "health_status": ["healthy", "recovering"],
        "reproductive_status": ["non_pregnant"],
        "procurement_path": ["farm_born", "procured", "imported"]
      }
    },
    {
      "row_key": "goat_kid_pox_course_all_breeds",
      "label": "Goat kids, Goat Pox course, all breeds",
      "criteria": {
        "species_codes": ["goat"],
        "breed_ids": ["*"],
        "sex": ["female", "male"],
        "stage_codes": ["K1", "K2", "K3"],
        "age_days": {
          "min": 0,
          "max": 140
        },
        "lifecycle_status": ["alive"],
        "health_status": ["healthy", "recovering"],
        "reproductive_status": ["non_pregnant"],
        "procurement_path": ["farm_born", "procured", "imported"]
      }
    },
    {
      "row_key": "sheep_kid_pox_blue_tongue_all_breeds",
      "label": "Sheep kids, Sheep Pox and Blue Tongue course, all breeds",
      "criteria": {
        "species_codes": ["sheep"],
        "breed_ids": ["*"],
        "sex": ["female", "male"],
        "stage_codes": ["K1", "K2", "K3"],
        "age_days": {
          "min": 0,
          "max": 140
        },
        "lifecycle_status": ["alive"],
        "health_status": ["healthy", "recovering"],
        "reproductive_status": ["non_pregnant"],
        "procurement_path": ["farm_born", "procured", "imported"]
      }
    },
    {
      "row_key": "adult_goat_female_revaccination",
      "label": "Adult goat females, shared non-commercial adult tags, all breeds",
      "criteria": {
        "species_codes": ["goat"],
        "breed_ids": ["*"],
        "sex": ["female"],
        "stage_codes": ["FATTENING_FEMALE", "NON_PREGNANT", "PREGNANT_EARLY", "PREGNANT_LATE", "BREEDING", "MOTHER"],
        "age_days": {
          "min": 141,
          "max": null
        },
        "lifecycle_status": ["alive"],
        "health_status": ["healthy", "recovering"],
        "reproductive_status": ["non_pregnant", "pregnant", "lactating", "mother"],
        "procurement_path": ["farm_born", "procured", "imported"]
      }
    },
    {
      "row_key": "adult_goat_male_breeder_revaccination",
      "label": "Adult goat males, buck and fattening-male tags, all breeds",
      "criteria": {
        "species_codes": ["goat"],
        "breed_ids": ["*"],
        "sex": ["male"],
        "stage_codes": ["FATTENING_MALE", "BUCK"],
        "age_days": {
          "min": 141,
          "max": null
        },
        "lifecycle_status": ["alive"],
        "health_status": ["healthy", "recovering"],
        "reproductive_status": ["buck"],
        "procurement_path": ["farm_born", "procured", "imported"]
      }
    },
    {
      "row_key": "adult_sheep_female_revaccination",
      "label": "Adult sheep females, shared non-commercial adult tags, all breeds",
      "criteria": {
        "species_codes": ["sheep"],
        "breed_ids": ["*"],
        "sex": ["female"],
        "stage_codes": ["FATTENING_FEMALE", "NON_PREGNANT", "PREGNANT_EARLY", "PREGNANT_LATE", "BREEDING", "MOTHER"],
        "age_days": {
          "min": 141,
          "max": null
        },
        "lifecycle_status": ["alive"],
        "health_status": ["healthy", "recovering"],
        "reproductive_status": ["non_pregnant", "pregnant", "lactating", "mother"],
        "procurement_path": ["farm_born", "procured", "imported"]
      }
    },
    {
      "row_key": "adult_sheep_male_breeder_revaccination",
      "label": "Adult sheep males, buck and fattening-male tags, all breeds",
      "criteria": {
        "species_codes": ["sheep"],
        "breed_ids": ["*"],
        "sex": ["male"],
        "stage_codes": ["FATTENING_MALE", "BUCK"],
        "age_days": {
          "min": 141,
          "max": null
        },
        "lifecycle_status": ["alive"],
        "health_status": ["healthy", "recovering"],
        "reproductive_status": ["buck"],
        "procurement_path": ["farm_born", "procured", "imported"]
      }
    },
    {
      "row_key": "adult_goat_commercial_milking_revaccination",
      "label": "Adult goats, commercial milking tags",
      "criteria": {
        "species_codes": ["goat"],
        "breed_ids": ["*"],
        "sex": ["female"],
        "stage_codes": ["MOTHER_MILKING_WAITING", "MILKING_WARMUP", "MILKING"],
        "age_days": {
          "min": 141,
          "max": null
        },
        "lifecycle_status": ["alive"],
        "health_status": ["healthy", "recovering"],
        "reproductive_status": ["lactating", "mother"],
        "procurement_path": ["farm_born", "procured", "imported"]
      }
    },
    {
      "row_key": "clinical_hold_shared_kid_stages",
      "label": "Clinical hold animals, shared kid stages",
      "criteria": {
        "species_codes": ["goat", "sheep"],
        "breed_ids": ["*"],
        "sex": ["female", "male"],
        "stage_codes": ["K0", "K1", "K2", "K3"],
        "health_status": ["sick", "under_treatment", "quarantine", "icu"],
        "lifecycle_status": ["alive"]
      }
    },
    {
      "row_key": "clinical_hold_shared_female_adult_stages",
      "label": "Clinical hold animals, shared female adult stages",
      "criteria": {
        "species_codes": ["goat", "sheep"],
        "breed_ids": ["*"],
        "sex": ["female"],
        "stage_codes": ["FATTENING_FEMALE", "NON_PREGNANT", "PREGNANT_EARLY", "PREGNANT_LATE", "BREEDING", "MOTHER"],
        "health_status": ["sick", "under_treatment", "quarantine", "icu"],
        "lifecycle_status": ["alive"]
      }
    },
    {
      "row_key": "clinical_hold_shared_male_adult_stages",
      "label": "Clinical hold animals, shared male adult stages",
      "criteria": {
        "species_codes": ["goat", "sheep"],
        "breed_ids": ["*"],
        "sex": ["male"],
        "stage_codes": ["FATTENING_MALE", "BUCK"],
        "health_status": ["sick", "under_treatment", "quarantine", "icu"],
        "lifecycle_status": ["alive"]
      }
    },
    {
      "row_key": "clinical_hold_goat_commercial_milking_stages",
      "label": "Clinical hold goats, commercial milking stages",
      "criteria": {
        "species_codes": ["goat"],
        "breed_ids": ["*"],
        "sex": ["female"],
        "stage_codes": ["MOTHER_MILKING_WAITING", "MILKING_WARMUP", "MILKING"],
        "health_status": ["sick", "under_treatment", "quarantine", "icu"],
        "lifecycle_status": ["alive"]
      }
    }
  ],
  "cells": [
    {
      "row_key": "kid_shared_course_all_species",
      "vaccine_code": "ET_TT",
      "action": "due",
      "dose_schedule": [
        {
          "dose_code": "dose_1",
          "sequence": 1,
          "trigger_type": "birth_age",
          "offset_days": 28,
          "earliest_offset_days": 28,
          "latest_offset_days": 35,
          "min_gap_days": 0,
          "max_delay_days": 7,
          "repeat": "none",
          "catch_up": "immediate"
        },
        {
          "dose_code": "booster_1",
          "sequence": 2,
          "trigger_type": "after_previous_completion",
          "offset_days": 21,
          "earliest_offset_days": 21,
          "latest_offset_days": 28,
          "min_gap_days": 21,
          "max_delay_days": 7,
          "repeat": "none",
          "catch_up": "immediate"
        }
      ],
      "proof_policy_key": "vaccination_drive_standard"
    },
    {
      "row_key": "kid_shared_course_all_species",
      "vaccine_code": "FMD",
      "action": "due",
      "dose_schedule": [
        {
          "dose_code": "single",
          "sequence": 1,
          "trigger_type": "birth_age",
          "offset_days": 84,
          "earliest_offset_days": 84,
          "latest_offset_days": 91,
          "min_gap_days": 0,
          "max_delay_days": 7,
          "repeat": "none",
          "catch_up": "immediate"
        }
      ],
      "proof_policy_key": "vaccination_drive_standard"
    },
    {
      "row_key": "kid_shared_course_all_species",
      "vaccine_code": "HS",
      "action": "due",
      "dose_schedule": [
        {
          "dose_code": "single",
          "sequence": 1,
          "trigger_type": "birth_age",
          "offset_days": 84,
          "earliest_offset_days": 84,
          "latest_offset_days": 91,
          "min_gap_days": 0,
          "max_delay_days": 7,
          "repeat": "none",
          "catch_up": "immediate"
        }
      ],
      "proof_policy_key": "vaccination_drive_standard"
    },
    {
      "row_key": "kid_shared_course_all_species",
      "vaccine_code": "PPR",
      "action": "due",
      "dose_schedule": [
        {
          "dose_code": "single",
          "sequence": 1,
          "trigger_type": "birth_age",
          "offset_days": 112,
          "earliest_offset_days": 112,
          "latest_offset_days": 119,
          "min_gap_days": 0,
          "max_delay_days": 7,
          "repeat": "none",
          "catch_up": "immediate"
        }
      ],
      "proof_policy_key": "vaccination_drive_standard"
    },
    {
      "row_key": "goat_kid_pox_course_all_breeds",
      "vaccine_code": "GOAT_POX",
      "action": "due",
      "dose_schedule": [
        {
          "dose_code": "single",
          "sequence": 1,
          "trigger_type": "birth_age",
          "offset_days": 140,
          "earliest_offset_days": 140,
          "latest_offset_days": 147,
          "min_gap_days": 28,
          "max_delay_days": 7,
          "repeat": "none",
          "catch_up": "immediate"
        }
      ],
      "proof_policy_key": "vaccination_drive_standard"
    },
    {
      "row_key": "sheep_kid_pox_blue_tongue_all_breeds",
      "vaccine_code": "SHEEP_POX",
      "action": "due",
      "dose_schedule": [
        {
          "dose_code": "single",
          "sequence": 1,
          "trigger_type": "birth_age",
          "offset_days": 84,
          "earliest_offset_days": 84,
          "latest_offset_days": 91,
          "min_gap_days": 0,
          "max_delay_days": 7,
          "repeat": "none",
          "catch_up": "immediate"
        }
      ],
      "proof_policy_key": "vaccination_drive_standard"
    },
    {
      "row_key": "sheep_kid_pox_blue_tongue_all_breeds",
      "vaccine_code": "BLUE_TONGUE",
      "action": "due",
      "dose_schedule": [
        {
          "dose_code": "dose_1",
          "sequence": 1,
          "trigger_type": "birth_age",
          "offset_days": 112,
          "earliest_offset_days": 112,
          "latest_offset_days": 119,
          "min_gap_days": 0,
          "max_delay_days": 7,
          "repeat": "none",
          "catch_up": "immediate"
        },
        {
          "dose_code": "booster_1",
          "sequence": 2,
          "trigger_type": "after_previous_completion",
          "offset_days": 28,
          "earliest_offset_days": 28,
          "latest_offset_days": 35,
          "min_gap_days": 28,
          "max_delay_days": 7,
          "repeat": "none",
          "catch_up": "immediate"
        }
      ],
      "proof_policy_key": "vaccination_drive_standard"
    },
    {
      "row_key": "adult_goat_female_revaccination",
      "vaccine_code": "ET_TT",
      "action": "due",
      "dose_schedule": [
        { "dose_code": "revaccination", "sequence": 99, "trigger_type": "after_previous_completion", "offset_days": 182, "earliest_offset_days": 182, "latest_offset_days": 196, "min_gap_days": 182, "max_delay_days": 14, "repeat": "every_n_days", "repeat_interval_days": 182, "catch_up": "immediate" }
      ],
      "proof_policy_key": "vaccination_drive_standard"
    },
    {
      "row_key": "adult_goat_female_revaccination",
      "vaccine_code": "PPR",
      "action": "due",
      "dose_schedule": [
        { "dose_code": "revaccination", "sequence": 99, "trigger_type": "after_previous_completion", "offset_days": 1095, "earliest_offset_days": 1095, "latest_offset_days": 1109, "min_gap_days": 1095, "max_delay_days": 14, "repeat": "every_n_days", "repeat_interval_days": 1095, "catch_up": "immediate" }
      ],
      "proof_policy_key": "vaccination_drive_standard"
    },
    {
      "row_key": "adult_goat_female_revaccination",
      "vaccine_code": "GOAT_POX",
      "action": "due",
      "dose_schedule": [
        { "dose_code": "revaccination", "sequence": 99, "trigger_type": "after_previous_completion", "offset_days": 365, "earliest_offset_days": 365, "latest_offset_days": 379, "min_gap_days": 365, "max_delay_days": 14, "repeat": "every_n_days", "repeat_interval_days": 365, "catch_up": "immediate" }
      ],
      "proof_policy_key": "vaccination_drive_standard"
    },
    {
      "row_key": "adult_goat_female_revaccination",
      "vaccine_code": "FMD",
      "action": "due",
      "dose_schedule": [
        { "dose_code": "revaccination", "sequence": 99, "trigger_type": "after_previous_completion", "offset_days": 274, "earliest_offset_days": 274, "latest_offset_days": 288, "min_gap_days": 274, "max_delay_days": 14, "repeat": "every_n_days", "repeat_interval_days": 274, "catch_up": "immediate" }
      ],
      "proof_policy_key": "vaccination_drive_standard"
    },
    {
      "row_key": "adult_goat_female_revaccination",
      "vaccine_code": "HS",
      "action": "due",
      "dose_schedule": [
        { "dose_code": "revaccination", "sequence": 99, "trigger_type": "after_previous_completion", "offset_days": 365, "earliest_offset_days": 365, "latest_offset_days": 379, "min_gap_days": 365, "max_delay_days": 14, "repeat": "every_n_days", "repeat_interval_days": 365, "catch_up": "immediate" }
      ],
      "proof_policy_key": "vaccination_drive_standard"
    },
    {
      "row_key": "adult_goat_male_breeder_revaccination",
      "vaccine_code": "ET_TT",
      "action": "due",
      "dose_schedule": [
        { "dose_code": "revaccination", "sequence": 99, "trigger_type": "after_previous_completion", "offset_days": 182, "earliest_offset_days": 182, "latest_offset_days": 196, "min_gap_days": 182, "max_delay_days": 14, "repeat": "every_n_days", "repeat_interval_days": 182, "catch_up": "immediate" }
      ],
      "proof_policy_key": "vaccination_drive_standard"
    },
    {
      "row_key": "adult_goat_male_breeder_revaccination",
      "vaccine_code": "PPR",
      "action": "due",
      "dose_schedule": [
        { "dose_code": "revaccination", "sequence": 99, "trigger_type": "after_previous_completion", "offset_days": 1095, "earliest_offset_days": 1095, "latest_offset_days": 1109, "min_gap_days": 1095, "max_delay_days": 14, "repeat": "every_n_days", "repeat_interval_days": 1095, "catch_up": "immediate" }
      ],
      "proof_policy_key": "vaccination_drive_standard"
    },
    {
      "row_key": "adult_goat_male_breeder_revaccination",
      "vaccine_code": "GOAT_POX",
      "action": "due",
      "dose_schedule": [
        { "dose_code": "revaccination", "sequence": 99, "trigger_type": "after_previous_completion", "offset_days": 365, "earliest_offset_days": 365, "latest_offset_days": 379, "min_gap_days": 365, "max_delay_days": 14, "repeat": "every_n_days", "repeat_interval_days": 365, "catch_up": "immediate" }
      ],
      "proof_policy_key": "vaccination_drive_standard"
    },
    {
      "row_key": "adult_goat_male_breeder_revaccination",
      "vaccine_code": "FMD",
      "action": "due",
      "dose_schedule": [
        { "dose_code": "revaccination", "sequence": 99, "trigger_type": "after_previous_completion", "offset_days": 274, "earliest_offset_days": 274, "latest_offset_days": 288, "min_gap_days": 274, "max_delay_days": 14, "repeat": "every_n_days", "repeat_interval_days": 274, "catch_up": "immediate" }
      ],
      "proof_policy_key": "vaccination_drive_standard"
    },
    {
      "row_key": "adult_goat_male_breeder_revaccination",
      "vaccine_code": "HS",
      "action": "due",
      "dose_schedule": [
        { "dose_code": "revaccination", "sequence": 99, "trigger_type": "after_previous_completion", "offset_days": 365, "earliest_offset_days": 365, "latest_offset_days": 379, "min_gap_days": 365, "max_delay_days": 14, "repeat": "every_n_days", "repeat_interval_days": 365, "catch_up": "immediate" }
      ],
      "proof_policy_key": "vaccination_drive_standard"
    },
    {
      "row_key": "adult_sheep_female_revaccination",
      "vaccine_code": "ET_TT",
      "action": "due",
      "dose_schedule": [
        { "dose_code": "revaccination", "sequence": 99, "trigger_type": "after_previous_completion", "offset_days": 182, "earliest_offset_days": 182, "latest_offset_days": 196, "min_gap_days": 182, "max_delay_days": 14, "repeat": "every_n_days", "repeat_interval_days": 182, "catch_up": "immediate" }
      ],
      "proof_policy_key": "vaccination_drive_standard"
    },
    {
      "row_key": "adult_sheep_female_revaccination",
      "vaccine_code": "PPR",
      "action": "due",
      "dose_schedule": [
        { "dose_code": "revaccination", "sequence": 99, "trigger_type": "after_previous_completion", "offset_days": 1095, "earliest_offset_days": 1095, "latest_offset_days": 1109, "min_gap_days": 1095, "max_delay_days": 14, "repeat": "every_n_days", "repeat_interval_days": 1095, "catch_up": "immediate" }
      ],
      "proof_policy_key": "vaccination_drive_standard"
    },
    {
      "row_key": "adult_sheep_female_revaccination",
      "vaccine_code": "SHEEP_POX",
      "action": "due",
      "dose_schedule": [
        { "dose_code": "revaccination", "sequence": 99, "trigger_type": "after_previous_completion", "offset_days": 365, "earliest_offset_days": 365, "latest_offset_days": 379, "min_gap_days": 365, "max_delay_days": 14, "repeat": "every_n_days", "repeat_interval_days": 365, "catch_up": "immediate" }
      ],
      "proof_policy_key": "vaccination_drive_standard"
    },
    {
      "row_key": "adult_sheep_female_revaccination",
      "vaccine_code": "BLUE_TONGUE",
      "action": "due",
      "dose_schedule": [
        { "dose_code": "revaccination", "sequence": 99, "trigger_type": "after_previous_completion", "offset_days": 365, "earliest_offset_days": 365, "latest_offset_days": 379, "min_gap_days": 365, "max_delay_days": 14, "repeat": "every_n_days", "repeat_interval_days": 365, "catch_up": "immediate" }
      ],
      "proof_policy_key": "vaccination_drive_standard"
    },
    {
      "row_key": "adult_sheep_female_revaccination",
      "vaccine_code": "FMD",
      "action": "due",
      "dose_schedule": [
        { "dose_code": "revaccination", "sequence": 99, "trigger_type": "after_previous_completion", "offset_days": 274, "earliest_offset_days": 274, "latest_offset_days": 288, "min_gap_days": 274, "max_delay_days": 14, "repeat": "every_n_days", "repeat_interval_days": 274, "catch_up": "immediate" }
      ],
      "proof_policy_key": "vaccination_drive_standard"
    },
    {
      "row_key": "adult_sheep_female_revaccination",
      "vaccine_code": "HS",
      "action": "due",
      "dose_schedule": [
        { "dose_code": "revaccination", "sequence": 99, "trigger_type": "after_previous_completion", "offset_days": 365, "earliest_offset_days": 365, "latest_offset_days": 379, "min_gap_days": 365, "max_delay_days": 14, "repeat": "every_n_days", "repeat_interval_days": 365, "catch_up": "immediate" }
      ],
      "proof_policy_key": "vaccination_drive_standard"
    },
    {
      "row_key": "adult_sheep_male_breeder_revaccination",
      "vaccine_code": "ET_TT",
      "action": "due",
      "dose_schedule": [
        { "dose_code": "revaccination", "sequence": 99, "trigger_type": "after_previous_completion", "offset_days": 182, "earliest_offset_days": 182, "latest_offset_days": 196, "min_gap_days": 182, "max_delay_days": 14, "repeat": "every_n_days", "repeat_interval_days": 182, "catch_up": "immediate" }
      ],
      "proof_policy_key": "vaccination_drive_standard"
    },
    {
      "row_key": "adult_sheep_male_breeder_revaccination",
      "vaccine_code": "PPR",
      "action": "due",
      "dose_schedule": [
        { "dose_code": "revaccination", "sequence": 99, "trigger_type": "after_previous_completion", "offset_days": 1095, "earliest_offset_days": 1095, "latest_offset_days": 1109, "min_gap_days": 1095, "max_delay_days": 14, "repeat": "every_n_days", "repeat_interval_days": 1095, "catch_up": "immediate" }
      ],
      "proof_policy_key": "vaccination_drive_standard"
    },
    {
      "row_key": "adult_sheep_male_breeder_revaccination",
      "vaccine_code": "SHEEP_POX",
      "action": "due",
      "dose_schedule": [
        { "dose_code": "revaccination", "sequence": 99, "trigger_type": "after_previous_completion", "offset_days": 365, "earliest_offset_days": 365, "latest_offset_days": 379, "min_gap_days": 365, "max_delay_days": 14, "repeat": "every_n_days", "repeat_interval_days": 365, "catch_up": "immediate" }
      ],
      "proof_policy_key": "vaccination_drive_standard"
    },
    {
      "row_key": "adult_sheep_male_breeder_revaccination",
      "vaccine_code": "BLUE_TONGUE",
      "action": "due",
      "dose_schedule": [
        { "dose_code": "revaccination", "sequence": 99, "trigger_type": "after_previous_completion", "offset_days": 365, "earliest_offset_days": 365, "latest_offset_days": 379, "min_gap_days": 365, "max_delay_days": 14, "repeat": "every_n_days", "repeat_interval_days": 365, "catch_up": "immediate" }
      ],
      "proof_policy_key": "vaccination_drive_standard"
    },
    {
      "row_key": "adult_sheep_male_breeder_revaccination",
      "vaccine_code": "FMD",
      "action": "due",
      "dose_schedule": [
        { "dose_code": "revaccination", "sequence": 99, "trigger_type": "after_previous_completion", "offset_days": 274, "earliest_offset_days": 274, "latest_offset_days": 288, "min_gap_days": 274, "max_delay_days": 14, "repeat": "every_n_days", "repeat_interval_days": 274, "catch_up": "immediate" }
      ],
      "proof_policy_key": "vaccination_drive_standard"
    },
    {
      "row_key": "adult_sheep_male_breeder_revaccination",
      "vaccine_code": "HS",
      "action": "due",
      "dose_schedule": [
        { "dose_code": "revaccination", "sequence": 99, "trigger_type": "after_previous_completion", "offset_days": 365, "earliest_offset_days": 365, "latest_offset_days": 379, "min_gap_days": 365, "max_delay_days": 14, "repeat": "every_n_days", "repeat_interval_days": 365, "catch_up": "immediate" }
      ],
      "proof_policy_key": "vaccination_drive_standard"
    },
    {
      "row_key": "adult_goat_commercial_milking_revaccination",
      "vaccine_code": "ET_TT",
      "action": "due",
      "dose_schedule": [
        { "dose_code": "revaccination", "sequence": 99, "trigger_type": "after_previous_completion", "offset_days": 182, "earliest_offset_days": 182, "latest_offset_days": 196, "min_gap_days": 182, "max_delay_days": 14, "repeat": "every_n_days", "repeat_interval_days": 182, "catch_up": "immediate" }
      ],
      "proof_policy_key": "vaccination_drive_standard"
    },
    {
      "row_key": "adult_goat_commercial_milking_revaccination",
      "vaccine_code": "PPR",
      "action": "due",
      "dose_schedule": [
        { "dose_code": "revaccination", "sequence": 99, "trigger_type": "after_previous_completion", "offset_days": 1095, "earliest_offset_days": 1095, "latest_offset_days": 1109, "min_gap_days": 1095, "max_delay_days": 14, "repeat": "every_n_days", "repeat_interval_days": 1095, "catch_up": "immediate" }
      ],
      "proof_policy_key": "vaccination_drive_standard"
    },
    {
      "row_key": "adult_goat_commercial_milking_revaccination",
      "vaccine_code": "GOAT_POX",
      "action": "due",
      "dose_schedule": [
        { "dose_code": "revaccination", "sequence": 99, "trigger_type": "after_previous_completion", "offset_days": 365, "earliest_offset_days": 365, "latest_offset_days": 379, "min_gap_days": 365, "max_delay_days": 14, "repeat": "every_n_days", "repeat_interval_days": 365, "catch_up": "immediate" }
      ],
      "proof_policy_key": "vaccination_drive_standard"
    },
    {
      "row_key": "adult_goat_commercial_milking_revaccination",
      "vaccine_code": "FMD",
      "action": "due",
      "dose_schedule": [
        { "dose_code": "revaccination", "sequence": 99, "trigger_type": "after_previous_completion", "offset_days": 274, "earliest_offset_days": 274, "latest_offset_days": 288, "min_gap_days": 274, "max_delay_days": 14, "repeat": "every_n_days", "repeat_interval_days": 274, "catch_up": "immediate" }
      ],
      "proof_policy_key": "vaccination_drive_standard"
    },
    {
      "row_key": "adult_goat_commercial_milking_revaccination",
      "vaccine_code": "HS",
      "action": "due",
      "dose_schedule": [
        { "dose_code": "revaccination", "sequence": 99, "trigger_type": "after_previous_completion", "offset_days": 365, "earliest_offset_days": 365, "latest_offset_days": 379, "min_gap_days": 365, "max_delay_days": 14, "repeat": "every_n_days", "repeat_interval_days": 365, "catch_up": "immediate" }
      ],
      "proof_policy_key": "vaccination_drive_standard"
    },
    {
      "row_key": "clinical_hold_shared_kid_stages",
      "vaccine_code": "*",
      "action": "defer",
      "defer_reason": "clinical_hold",
      "resume_when": { "health_status": ["healthy", "recovering"] }
    },
    {
      "row_key": "clinical_hold_shared_female_adult_stages",
      "vaccine_code": "*",
      "action": "defer",
      "defer_reason": "clinical_hold",
      "resume_when": { "health_status": ["healthy", "recovering"] }
    },
    {
      "row_key": "clinical_hold_shared_male_adult_stages",
      "vaccine_code": "*",
      "action": "defer",
      "defer_reason": "clinical_hold",
      "resume_when": { "health_status": ["healthy", "recovering"] }
    },
    {
      "row_key": "clinical_hold_goat_commercial_milking_stages",
      "vaccine_code": "*",
      "action": "defer",
      "defer_reason": "clinical_hold",
      "resume_when": { "health_status": ["healthy", "recovering"] }
    }
  ],
  "global_policies": {
    "procurement": {
      "warmup_hold_days": 7,
      "adult_prior_vaccination_allowed": true,
      "kids_normal_schedule_until_age_days": 112,
      "first_wave_vaccines": ["ET_TT", "PPR"],
      "et_tt_course_booster_min_gap_days": 21,
      "second_wave_after_days": 28,
      "second_wave_vaccines_by_species": {
        "goat": ["GOAT_POX"],
        "sheep": ["SHEEP_POX"]
      }
    },
    "reproductive": {
      "pregnancy_allowed_until_month": 3,
      "pregnancy_blocked_months": [4, 5],
      "post_delivery_catchup_days": 14
    },
    "clinical_defer_states": ["sick", "under_treatment", "quarantine", "icu"],
    "compatibility": [
      {
        "from_class": "live",
        "to_class": "live",
        "minimum_gap_days": 28,
        "same_day_allowed": false
      },
      {
        "from_class": "live",
        "to_class": "killed",
        "minimum_gap_days": 14,
        "same_day_allowed": true
      },
      {
        "from_class": "killed",
        "to_class": "killed",
        "minimum_gap_days": 14,
        "same_day_allowed": true
      },
      {
        "from_pathogen_class": "bacterial",
        "to_pathogen_class": "viral",
        "minimum_gap_days": 0,
        "same_day_allowed": true
      }
    ],
    "history": {
      "trusted_completion_suppresses_matching_due": true,
      "unknown_history_policy": "single_safe_catchup_review",
      "older_animal_anti_flood": true
    }
  },
  "proof_policies": {
    "vaccination_drive_standard": {
      "sop_version_binding": "protocol_versions.sop_version_id",
      "required_fields": [
        "animal_scan",
        "vaccine",
        "vial_lot",
        "dose",
        "administered_at",
        "proof_media",
        "adverse_reaction",
        "verifier_review"
      ]
    }
  }
}
```

## 8. Manohar PR review note

The current Manohar PR is mainly Calendar/drive consolidation and Herd Passport
visibility. That work can continue, but it should not hardcode the old
vaccine-first form as the final contract. Before merging UI/config changes:

- rebase onto the renamed `docs/preventive-care-vaccination` path;
- remove source/review UI dependency from Config authoring;
- replace the current one-vaccine-per-protocol Config list with a scoped
  ruleset list: company default plus park overrides;
- make Calendar/Passport detail link to `protocol_version_id`, `rule_id`,
  `animal_id`, `shed_id`, and `batch_id`;
- ensure any vaccine due row can explain which matrix row/cell matched it;
- avoid recomputing full-herd eligibility from the UI or Calendar layer.
