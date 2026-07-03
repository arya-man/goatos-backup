# Goat OS — Generic Protocol & Obligation Engine

**Status:** Draft v2 correction · **Date:** 2026-06-29
**Grounded in:** the committed migrations in `backend/migrations/postgres/`
through the current tail (`000117` at this correction). The first draft was
written when the repo stopped near `000060`; current work must check the live
migration tail before adding tables or treating protocol/obligation/inventory as
absent.
**Why this doc:** Preventive Care (PC) (vaccination, deworming, biosecurity, feed/water testing, panel cleaning, sanitization, fire-safety, SOP-video, stock checks, director reporting) **and** Feed Direction all need the same thing: admin-set rules → due obligations → tasks → proof → completion → projections. Build it **once**. Module-specific tables (e.g. `vaccination_completions`) link into this engine; they do not re-implement it.

**Path B target correction (2026-07-03):** any current-state references to
`goats`, `goat_id`, `target_type='goat'`, or `goat.*` events are legacy
implementation names only. The target contract is mixed-species
`herd_animals` / `animal_id`, `target_type='herd_animal'`, and `animal.*`
events. Species is catalog-driven (`species_catalog`) and goats/sheep can share
the same shed/tag.

---

## 0. Repo reality (what already exists — reuse, don't reinvent)

> Convention: **no native Postgres ENUMs** — every enum is `text` + `CHECK`. Every table is `tenant_id uuid NOT NULL REFERENCES tenants` scoped.

| Committed table | Verdict | Role for this engine |
|---|---|---|
| `goats`, `goat_identifiers`, `goat_location_history`, `goat_identity_events`(partitioned) | legacy current implementation; replace/rename under Path B | target becomes `herd_animals`, `animal_identifiers`, `animal_location_history`, `animal_identity_events` |
| `locations` (self-FK tree: farm/park/shed/cohort/pen) + `location_operational_attributes` (`usable_for_vaccination/feed/sop`, `is_holding/is_quarantine/is_icu`) + `location_capacity_records` (temporal) | reuse-enhance | scope tree; **no `parks`/`sheds` tables exist** |
| `workforce_*` (members, capabilities `vaccination.execute`/`feed.report`/`proof.verify`, member_capabilities scoped) | reuse-as-is | who executes |
| `user_scope_grants` (role `admin/park_head/operator/verifier/ceo_internal`, scope validated against `locations`) | reuse-as-is | **authority** (draft vs publish) |
| `sop_definitions/versions/tasks/submissions/submission_items` (form_dsl + proof_policy jsonb; one-published-per-sop; task state machine; idempotent submissions) | reuse-as-is | **execution + proof** |
| `outbox_messages`, `idempotency_keys`, `audit_log`(partitioned) | reuse-as-is | egress, retry-safety, audit |
| projection-contract (`feature_coverage_registry`, counts/mortality `*_projection_rows`/`*_projection_state`) | reuse-enhance | dashboard read-models |
| `movement_commands` (CHECK `'shifting.apply'` only) | reference-only | the *only* hardcoded side-effect dispatch — do not overload |
| inventory (`inventory_items`/`inventory_stock`/`inventory_stock_movements`/`vaccines`) | reuse-enhance | generic stock/FEFO ledger; feed items use `inventory_items.category='feed'` |
| `protocol_*`, `obligation_*` | reuse-enhance | generic rule/version/trigger and obligation/batch/status kernel already exists |
| `parks`, `sheds`, `vaccine_stock` | **ABSENT** | do not invent parallel tables; use `locations`, generic inventory, and module completion rows |

---

## 1. The engine in one line

```
admin rule (protocol_version, published)  →  trigger fires (animal born / cron / upstream completion)
  →  obligation_instances rows materialized in Postgres  [SOURCE OF TRUTH for "what is due"]
  →  Cloud Scheduler worker flips due rows, enqueues Cloud Tasks (near-term only)
  →  sop_task dispatched  →  operator submits via sop_submissions (+ proof)  →  verification
  →  module completion row (links obligation_id)  →  inventory_stock_movements reserve/consume/release (FEFO)  →  outbox event
  →  obligation_status_events ledger  →  projection refresh  →  Control Tower / Passport / Calendar
```

**Boundary:** GCP does plumbing (retry, DLQ, timers, delivery, storage). Goat OS owns domain (rules, eligibility, grouping, proof, escalation, stock). **Postgres stays the queryable truth; Control Tower never reads Cloud Tasks/Pub-Sub state.**

---

## 2. Authority model (CEO/COO-only Config in V1)

Protocol rules are business-critical operational policy. In V1, the raw Config
screen is visible only to CEO/COO/superadmin users. Directors, park users,
field workers, and verifiers see generated instructions, obligations, tasks,
proof requirements, escalations, and dashboards; they do not see or edit raw
rule JSON.

**Category-specific capabilities (required — scope alone cannot separate verticals).** `workforce_member_capabilities` scopes only by tenant/park/shed/cohort — it has **no category dimension**. A generic protocol capability would therefore let one vertical's admin change another vertical's rules. So the capability code itself carries the category:
- `protocol.publish.vaccination`, `protocol.publish.feed_direction`, `protocol.publish.deworming`, ...
- Future proposer workflows can add explicit `protocol.propose.<category>` or
  `protocol.draft.<category>` capabilities, but V1 vaccination does not expose
  them in the UI.
- These fit the committed `capability_code` regex (`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*$`).
- The API resolves the target `protocol_definitions.category` and requires the matching `protocol.publish.<category>` capability at the relevant tenant/park scope for V1 config changes.
- Until an author/publish split is deliberately reopened, vaccination edit,
  preview, publish, and retire all require the CEO/COO/superadmin publish
  capability and top-level Config route access.
- *(Acceptable fallback if you must keep generic codes: a generic `protocol.publish` **plus** an explicit API authorization check that the actor's allowed categories include the target category. Per-category codes are preferred — the boundary is then enforceable from the capability model alone.)*

| Action | Required capability (category-specific, scoped tenant or park) | Effect |
|---|---|---|
| Draft / edit a `protocol_version` (status=`draft`) | `protocol.publish.<category>` in V1 vaccination | mutable draft visible only inside Config |
| **Publish** (`draft→published`, set `effective_from`) | **`protocol.publish.<category>`** (COO/CEO/superadmin) | immutable; new effective window opens |
| Retire | `protocol.publish.<category>` | `effective_to` set (closes the window) |

Capability is checked via `workforce_member_capabilities` (same mechanism as `vaccination.execute`); `user_scope_grants.role` still gates the surface. Every transition writes `audit_log`. A rule change = a **new version**, never an in-place edit of a published one.

**Seeding rule (no wildcard):** there is no `protocol.publish.*` wildcard capability. The seed migration must grant COO/CEO an **explicit publish capability for every V1 category** — `protocol.publish.vaccination` and any later category such as `protocol.publish.feed_direction`. **Every new `protocol_definitions.category` added later MUST ship an explicit publish capability seed** or no one can author/publish it. A future proposer workflow may also add `protocol.propose.<category>`/`protocol.draft.<category>`, but that is not part of V1 vaccination.

**Versioning is scope-policy-based, not "one row per vaccine":** a
`protocol_definition` is a stable ruleset family such as
`vaccination.matrix`, not a copied ET/PPR/FMD row. Immutable versions coexist
across time for audit. The active version for a moment is resolved by category
scope policy plus effective dates. Schedules already generated keep the version
they were generated under.

For vaccination V1 the policy is:

```text
scope_resolution_mode = tenant_default_with_park_overrides
active_cardinality = single_active_ruleset_per_scope
override_semantics = park_replaces_tenant_for_that_park
```

That means one active company vaccination matrix and at most one active
vaccination matrix per park. A park active version excludes that park from the
company version, but it does not deactivate the company version for other
parks. Future protocol categories may choose different policies, such as
multiple additive active templates per scope or merge semantics; do not bake
vaccination's single-active rule into the generic engine.

### 2.1 Config UI contract (CEO/COO authoring surface)

`protocol_rules` are **real business/medical/operations config — not public, not user-editable.** Only approved CEO/COO/superadmin users create, edit, preview, and publish real rules in V1. Field/verifier/park users **never see or edit raw config** — they see generated obligations, SOP tasks, proof requirements, and their Action Center work.

**Config screen (CEO/COO):** choose category/family → choose company or park
scope → create/edit a *draft* ruleset version → link an `sop_version_id` →
define proof policy → define escalation policy → **impact preview** →
activate/publish version.

**Rule fields:** module/`category` (vaccination, feed_direction, deworming, sanitation, ...) · multi-factor eligibility (species, breed/breed group, age, animal_stage, sex, lifecycle, health_status, shed/cohort/park, reproductive[exclude pregnant/lactating], defer_states[ICU/quarantine/sick]) · **`schedule[]` — the Schedule Builder: an array of dose/phase rows** (dose_code · trigger_type[birth_age/post_arrival/calendar/after_previous_completion/manual_campaign] · offset_days · due_window_days · min_gap_days · repeat[none/every_n_days/yearly; age-window repeats rejected until generator support lands] · repeat_until_after_age · catch_up · per-dose sop_label[display only — executable SOP binds at version sop_version_id] + proof_policy) — NOT a single trigger-day + booster flag · `missed_dose_policy` (immediate/next_cycle/pc_approval/defer) · park override (scope_type/scope_id) · `effective_from`/`effective_to`. Next due is derived by trigger/repeat/catch-up logic plus trusted accepted completion evidence; there is no separate next-due-basis DSL field. Rule JSON must not embed animal row snapshots; it references stable dimension keys and is evaluated against canonical `herd_animals` / location / procurement / completion facts.

**Activation behavior:** production obligations generate **only** from active
versions resolved by the category scope policy. Drafts and inactive historical
versions generate **no** new work. Active versions are **immutable** (a change =
a new version); old versions stay auditable. Only CEO/COO/superadmin users can
activate/publish (`protocol.publish.<category>` in V1 wording).

**Impact preview (required before activation — computed from the scoped ruleset):** affected animals/sheds/cohorts (from eligibility); **obligations per cycle** (= affected × non-recurring doses); **annual-repeat count** (rows with `repeat:yearly`); **catch-up count** and **existing-history count** (animals with prior accepted completions → next-due from last completion, not DOB); expected stock required; expected SOP tasks/batches; and **risks** — missing stock, no assigned operator, missing executable `sop_version_id` (a free-text `sop_label` does not count), conflicting rule, effective-date overlap, open obligations to supersede, and in-progress batches that need explicit operator choice. Activation is gated on the operator reviewing this.

**Config visibility matrix:**
| Role | Config access |
|---|---|
| CEO/COO/superadmin | full config + **publish** |
| Director | no raw Config visibility in V1; sees effective instructions, exceptions, and dashboards |
| Park Head / Manager | view *effective instructions/tasks*, not raw config (unless explicitly granted) |
| Field worker | **no config** — Action Center + SOP execution only |
| Verifier | **no config** — proof queue only |

**Activation/publish gate:** a version activates only when the actor has CEO/COO/superadmin
Config authority, JSON-schema validation passes, a real published SOP version
is bound where execution needs it, effective dates do not overlap, and the
impact preview has been generated. Source documents are committed engineering
evidence for the seeded/preset values, not UI fields or runtime
`review_status` gates.

For Preventive Care (PC) Vaccination, schedule expansion must preserve the
approved matrix mode in
`docs/preventive-care-vaccination/APPROVED-SCHEDULE-MATRIX.md`: fixed kid-course
due points are authored as due schedule rows, and adult/fattening steady-state
repeats are driven from accepted completion dates. Do not turn the kid timeline
into arbitrary weekly drive slots.

---

## 3. Schema — protocol layer (the ruleset, admin-authored)

### `protocol_definitions` (mirror of `sop_definitions`)
`protocol_id PK · tenant_id · code text CHECK (~ '^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*$') · name · category text CHECK · status text CHECK(draft/active/retired) · created_by · created_at · updated_at · row_version` — UNIQUE `(tenant_id, code)`.
`category CHECK IN ('vaccination','deworming','biosecurity','feed_water_testing','panel_cleaning','sanitization','fire_safety','sop_video','stock_check','director_reporting','feed_direction')`.

For vaccination, `protocol_definitions.code` is the stable ruleset family
(`vaccination.matrix`). ET+TT, PPR, FMD, HS, and Goat Pox are matrix cells or
expanded `protocol_rules`, not separate protocol definitions.

### `protocol_versions` (immutable, effective-dated — mirror of `sop_versions`)
`protocol_version_id PK · tenant_id · protocol_id→protocol_definitions · scope_type text CHECK(tenant/park) DEFAULT 'tenant' · scope_id uuid NULL (park for a per-park calendar; NULL ⇔ scope_type='tenant' = tenant-wide default) · version int(>0) · version_label · status text CHECK(draft/published/retired) · effective_from date NOT NULL · effective_to date NULL · rule_dsl jsonb · proof_policy jsonb · sop_version_id uuid NULL→sop_versions · drafted_by · published_by NULL · published_at NULL · retired_at · row_version`. CHECK: `scope_type='park' ⇒ scope_id NOT NULL` and `scope_type='tenant' ⇒ scope_id IS NULL`.
- **Non-overlap, not one-published-ever:** an **`EXCLUDE` constraint (GiST)** over `daterange(effective_from, effective_to)` keyed on `(tenant_id, protocol_id, scope_type, COALESCE(scope_id, '00000000-…-0'::uuid))` where `status='published'` — published windows cannot overlap **within the same scope** (a park calendar and the tenant default coexist), but past/present/future published versions all coexist. (`btree_gist` for the equality columns; the COALESCE sentinel collapses the nullable scope_id.) App-layer validation if GiST is undesirable.
- CHECK `effective_to IS NULL OR effective_to > effective_from`. Open-ended (`effective_to NULL`) = the current version; publishing the next one closes the prior's window.
- `sop_version_id` = the execution form to instantiate on dispatch.

If the implementation keeps `status='published'` for immutable published
history, expose `activation_state` separately in API/UI:
`draft | scheduled | active | inactive | retired`. Vaccination's partial
unique active constraint is keyed by `(tenant_id, protocol_id, scope_type,
COALESCE(scope_id, sentinel)) WHERE activation_state='active'`.

### `protocol_category_scope_policies` (generic category behavior)
Recommended generic table:
`policy_id PK · tenant_id · category · protocol_code NULL · target_type text · allowed_scope_levels jsonb · scope_resolution_mode text · active_cardinality text · override_semantics text · activation_cutover_policy text · status · updated_at`.

Vaccination seed:
`category='vaccination' · protocol_code='vaccination.matrix' · target_type='herd_animal' · allowed_scope_levels=['tenant','park'] · scope_resolution_mode='tenant_default_with_park_overrides' · active_cardinality='single_active_ruleset_per_scope' · override_semantics='park_replaces_tenant_for_that_park'`.

Feed Direction or future modules may use `multiple_active_per_scope`,
`additive`, or `merge` semantics. The engine reads this policy; it does not
assume vaccination behavior for all modules.

### `protocol_scope_resolutions` (derived read model for fast lookup)
Recommended derived table/materialized view:
`tenant_id · category · protocol_id · target_scope_type='park' · target_scope_id · active_protocol_version_id · source_scope_type(tenant/park) · source_scope_id NULL · overridden_protocol_version_id NULL · resolution_reason · computed_at`.

For vaccination generation, resolve by the animal's park:
1. active park version for the animal's park, if present;
2. otherwise active company version;
3. otherwise no ruleset gap.

This read model also powers Config list summaries:
"Company-wide v3 applies to 12 parks; excluded by 2 park overrides."

### `protocol_rules` (cadence / eligibility expansion — **one row per dose/phase**)
`rule_id PK · tenant_id · protocol_version_id→protocol_versions · dose_code text (primary/booster_1/booster_2/annual/catch_up/…) · sequence int · trigger_type text CHECK(birth_age/post_arrival/calendar/after_previous_completion/manual_campaign) · offset_days int · due_window_days int · min_gap_days int · repeat text CHECK(none/every_n_days/yearly) · repeat_until_after_age text · catch_up text CHECK(immediate/next_cycle/pc_approval/defer) · eligibility_json jsonb · sop_version_id uuid NULL (per-dose override) · proof_policy jsonb · withdrawal_days int NULL · sort_order int`.

### `rule_dsl` shape (the authored ruleset — **multi-dose / multi-phase**, source of truth on `protocol_versions`)
A rule is **not** a single trigger+booster. The editor authors and stores in `protocol_versions.rule_dsl` (jsonb, JSON-Schema-validated), then the engine **expands each `schedule[]` entry into one `protocol_rules` row**:
```
{ category, scope:{type,id},
  eligibility:{ age_band, animal_stage, sex, breed, lifecycle_status, health_status,
                park/shed/cohort, reproductive(any|exclude_pregnant|exclude_lactating|pregnant_only),
                defer_states:[ICU,quarantine,sick] },
  missed_dose_policy: immediate | next_cycle | pc_approval | defer,
  schedule: [                                 // ARRAY — multi-dose / lifecycle phases
    { dose_code, sequence, trigger_type, offset_days, due_window_days, min_gap_days,
      repeat, repeat_until_after_age, catch_up, sop_label, proof_policy:[…] }, … ],
  escalation }
```
**Lifecycle phases** (e.g. 0–12mo primary+booster, >12mo `repeat:yearly` on a separately eligible annual row) are expressed as multiple `schedule` rows, not one booster flag. For vaccination, the whole `rule_dsl` is one immutable scoped matrix version; a schedule change = a new inactive draft/version, then activation makes it the only active version for that same company/park scope.

**Executable SOP binding vs. display label (publish gate truth).** The **executable** SOP for a version is `protocol_versions.sop_version_id` — a **real published SOP-version UUID** the Config UI selects from the SOP Library. The publish execution-contract gate (`protocol/app/publish.go` `ValidateExecutionContract`) requires that UUID plus an object-shaped `proof_policy`. The per-dose `schedule[].sop_label` is a **display label only** — the current Config UI emits it as `sop_label` (never as an executable `sop_version`), so a free-text label like `"vacc-sop v2"` can never satisfy the gate. A **genuine per-dose executable override** is still supported by the schema (`protocol_rules.sop_version_id`, a real UUID): use that column when a real per-dose SOP version exists; do not resurrect a text label as an executable field.

### `protocol_triggers` (what spawns obligations)
`trigger_id PK · tenant_id · protocol_version_id→protocol_versions · trigger_type text CHECK(schedule/goat_lifecycle/location_event/manual/upstream_completion) · trigger_config jsonb · is_active boolean`.
e.g. booster = `upstream_completion` of the prior obligation; feed direction = `schedule` (nightly).

---

## 4. Schema — obligation layer (the due state — **SOURCE OF TRUTH**)

### `obligation_instances` — far-future due rows live HERE, queryable
`obligation_id PK · tenant_id · protocol_version_id→protocol_versions · rule_id NOT NULL→protocol_rules · batch_id uuid NULL→obligation_batches · target_type text CHECK(herd_animal/cohort/shed/park/tenant) · target_id uuid (interpreted per target_type: herd_animal→herd_animals, cohort/shed/park→locations, tenant→tenant_id) · scope_type/scope_id (org unit owning execution; vocabulary = sop_tasks: tenant/custodian_party/farm/park/shed/cohort) · due_at timestamptz NOT NULL · window_start NULL · window_end NULL · status text CHECK(scheduled/due/in_progress/completed/missed/waived/canceled/superseded) · sop_task_id uuid NULL→sop_tasks · idempotency_key text NOT NULL · generated_by_trigger_id NULL→protocol_triggers · sequence int DEFAULT 1 · completed_at · row_version`.
- **Deterministic idempotency_key** = `hash(tenant_id · protocol_version_id · rule_id · target_type · target_id · due_at · sequence)`. UNIQUE `(tenant_id, idempotency_key)`.
- **Duplicate-spawn guard:** UNIQUE active per `(tenant_id, protocol_version_id, rule_id, target_type, target_id, due_at)` — **includes `rule_id`** so the same animal can carry two different rules due the same day (e.g. two vaccines), and **`NULLS NOT DISTINCT`** (or `COALESCE(scope_id, sentinel)`), see §7.
- `batch_id` groups per-target obligations into a work unit (shed drive / feed session) — see `obligation_batches` below.
- Indexes: due-window scan `(tenant_id, status, due_at, obligation_id)`; per-target `(tenant_id, target_type, target_id, status)`; per-scope `(tenant_id, scope_type, scope_id, status, due_at)`; per-batch `(tenant_id, batch_id, status)`.

### `obligation_status_events` (append-only ledger — mirror `animal_identity_events`)
`obligation_event_id · tenant_id · obligation_id→obligation_instances · event_type text(scheduled/became_due/dispatched/completed/missed/waived/escalated) · occurred_at · recorded_at · actor_id · payload jsonb · idempotency_key · PK(obligation_event_id, recorded_at)` — **PARTITION BY RANGE(recorded_at)** monthly + `_default`, identical to target `animal_identity_events`/`audit_log`.

### `obligation_escalations` (typed — the schema lacks this today)
`escalation_id PK · tenant_id · obligation_id→obligation_instances · level int CHECK(>=1) · escalated_to_user_id uuid (external subject) · escalated_to_role text CHECK(admin/park_head/operator/verifier/ceo_internal) · reason · status text CHECK(open/acknowledged/resolved/expired) · opened_at · acknowledged_at · resolved_at` — index `(tenant_id, status, level)`.

### `obligation_batches` — the DRIVE / work-unit layer (generic)
A `sop_task` alone can't hold drive-level fields. Group per-target obligations executed together (a **shed vaccination drive**, a **feed session**) into a batch:
`batch_id PK · tenant_id · protocol_version_id→protocol_versions · scope_type/scope_id (shed/cohort) · session text NULL (Morning/Afternoon/Evening for feed) · planned_date date · window_start/end · status text CHECK(planned/in_progress/completed/superseded/canceled) · estimated_targets int · planned_quantity numeric NULL · reserved_quantity numeric DEFAULT 0 · used_quantity numeric DEFAULT 0 · quantity_unit text NULL (dose/ml/kg/litre) · primary_inventory_lot_id uuid NULL→inventory_stock · sop_task_id uuid NULL→sop_tasks · conducted_by uuid · proof_ref text NULL · context jsonb (module-specific extras) · created_at · row_version`.
- `obligation_instances.batch_id` points here. Stock reservation/consumption happens at the **batch** level (§6), not per-obligation.
- **`primary_inventory_lot_id` is a convenience pointer only** (the lot most expected). The authoritative record of *which lots in what quantities* a batch consumed is **`inventory_stock_movements`** (a batch can draw from multiple items/lots — common for feed). Never treat the single primary lot as the full picture.
- Vaccination drive = a batch; feed direction per shed×session = a batch. No vaccination-specific `vaccination_drives` table needed — the batch is generic; module specifics ride `protocol_version`/completion rows.

---

## 5. Execution — reuse the committed SOP engine

The unit of execution is the **batch**, not the individual obligation: **many due obligations → grouped into one `obligation_batch` → one `sop_task`.** Phase 0 catch-up uses Preventive Care approved manual campaign obligations/batches; standalone per-animal individual override generation is not exposed.

```
sweeper: collect due obligation_instances for a (scope, protocol, window)
   → create/attach obligation_batch  (set obligation_instances.batch_id)
   → spawn ONE sop_task(task_type=<module>, scope=shed/cohort, from sop_version_id)   ← batch.sop_task_id
   → operator runs form_dsl steps, uploads proof per proof_policy (GCS)
   → sop_submissions (idempotent) → sop_submission_items (per-target result)
   → verification (proof.verify) → accept/needs_review
   → module completion rows link (obligation_id, batch_id, sop_submission_item_id)
   → per obligation: obligation_status_events(completed) + outbox_messages(event)
   → at batch close: stock consume/release (§6); batch → completed
```

Assignment uses `workforce_member_capabilities` (e.g. `vaccination.execute` scoped to the shed's park). Verification uses `proof.verify`. Preventive Care approved catch-up uses the same canonical obligation/batch path as other campaign work. No new task engine.

---

## 6. Generic inventory (committed kernel — reuse/enhance, FEFO)

Do **not** build a vaccination-only stock island or a second feed stock path.
The generic inventory kernel is already committed (starting with `000072`);
extend it through the inventory app/repository where needed. Tenant-scoped
inventory uses:
- `inventory_items` — `category text CHECK IN ('vaccine','medicine','feed','consumable','equipment', …) · base_unit text CHECK(dose/ml/kg/litre/unit)`. The unit is intrinsic to the item (vaccines = dose/ml, feed = kg/litre).
- `vaccines` / `medicines` / `feed_items` — detail tables FK → `inventory_items` (dose/withdrawal/storage-temp for vaccines; ration attrs for feed).
- `inventory_stock` — lot table (running **balances**), **`numeric` quantities, never int** (feed is kg/litres): `lot_number, expiry_date, quantity_in_stock numeric, quantity_reserved numeric, quantity_consumed numeric, quantity_unit text, reorder_threshold numeric, park scope via location_id` + `CHECK(quantity_in_stock >= 0)`, `CHECK(quantity_reserved >= 0)`.
- **`inventory_stock_movements` — append-only ledger (the source of truth for stock):** `movement_id PK · tenant_id · lot_id→inventory_stock · movement_type text CHECK(reserve/release/consume/adjust/transfer/expire) · quantity numeric · quantity_unit text · batch_id uuid NULL→obligation_batches · obligation_id uuid NULL · ref_completion_id uuid NULL · reason · actor_id · idempotency_key text · occurred_at`. UNIQUE `(tenant_id, idempotency_key)`. `inventory_stock` balances are a projection of this ledger (kept in the same txn). Gives audit, reconciliation, rollback, and anti-misuse — impossible with bare decrements.
- **FEFO at application layer**: pick earliest unexpired lot. Movements recorded for every reserve/release/consume.

### Stock consumption mode — pick by execution path (no contradiction)
- **Group drive (batch):** `reserve` N doses against the FEFO lot at **batch start** (1 movement); `consume` the actual used + `release` the unused at **drive close / verification** (1–2 movements). **Never decrement per animal row** during a large shed drive.
- **Manual campaign / catch-up:** create canonical obligations/batches before execution; ad hoc per-animal individual override generation is not exposed in Phase 0.
Either way the running `inventory_stock` balance is updated in the same transaction as the movement insert.

---

## 7. CRUD → cascade on GCP (no DB triggers for cross-aggregate effects)

Cascade is **application code via the transactional outbox**, not Postgres triggers (triggers can't publish, don't retry, are invisible to the app). Repo already ships `outbox_messages` + relay pattern + `idempotency_keys`.

| Step | Where | Mechanism |
|---|---|---|
| Write entity + emit event | `api` (Cloud Run) | one txn: domain row + `outbox_messages` |
| Publish | `outbox-relay` | poll → Pub/Sub (retry policy + **dead-letter topic**) |
| Generate obligations | `consumer` (Pub/Sub push) | idempotent on obligation `idempotency_key` |
| **Far-future due** | **Postgres `obligation_instances`** | rows now, queryable — NOT a queue |
| Flip due + dispatch near-term | `obligation-sweeper` (Cloud Run Job ← **Cloud Scheduler** cron) | scan today's window via `(tenant_id, status, due_at)` idx → batch due work, mark missed, refresh Calendar projections, sweep reminders/escalations, and enqueue notification dispatch from Postgres state |
| Timer/reminder/retry | **Cloud Tasks** | `scheduleTime` near-term; auto retry+backoff; re-derivable from Postgres if lost |
| Proof media | GCS | signed-URL upload |
| Read models | projection-contract tables | **Control Tower reads Postgres/projection, never queue state** |

### Postgres correctness rules (committed-precedent)
1. **Nullable scope in a unique index → `NULLS NOT DISTINCT` (PG15+) or `COALESCE(col, sentinel_uuid)`.** Committed precedent: `counts_projection_state` uses `UNIQUE (tenant_id, COALESCE(view_id,'__all__'))`. Default `NULLS DISTINCT` silently allows duplicate NULL rows.
2. **Scope/profile FKs target `locations(location_id)`, never `parks`/`sheds`** (they don't exist). Add a `validate_*_scope()` trigger like `validate_user_scope_grant()` (location exists AND `location_type` matches `scope_type`).
3. **Inventory = generic lots, FEFO in app** (§6).
4. **Extend, don't fork** `feature_coverage_registry.feature_module` CHECK. Naming is not identical across systems: protocol category = `feed_direction`; inventory item category = `feed`; committed feature coverage currently uses module `feed` from `000079`. If product wants feature coverage to say `feed_direction`, ship an explicit migration instead of silently mixing names.

---

## 8. Module map — everything reuses this engine

| Vertical | Modules → each = a `protocol_definitions` category |
|---|---|
| **Preventive Care (PC)** | vaccination · deworming · biosecurity · feed/water testing · panel cleaning · sanitization · fire/safety · SOP-video verification · stock anti-misuse · director reporting |
| **Feed** (vertical) | feed-direction (config/ration -> next-day full direction -> cutoff Diff -> packing/staging -> execution -> wastage -> stock); later modules: feed-stock/loads, wastage/variance, ration-library |

Module-specific tables (`vaccination_completions`, `feed_direction_completions`, and optional generation/bridge/projection rows where the generic kernel has no natural home) link via `obligation_id` or run/proof identifiers. New modules add a category + protocol rules + an SOP form + (optionally) a completion/projection table — **no new engine.**

See: [Preventive Care (PC) Vaccination TRD](../preventive-care-vaccination/TRD.md) · [Feed Direction TRD](../feed-direction/TRD.md) · [State machines](./state-machines.md) · [Migration & cutover](./migration-and-cutover.md).

---

## 9. Million-animal scale — acceptance checklist (hard rules, enforce before SQL ships)

Non-negotiable invariants. A PR that violates any of these is rejected, not merged.

1. **No per-animal `sop_task` for group work.** Group execution = ONE `sop_task` per `obligation_batch` (shed drive / feed session). Catch-up is still canonical obligation/batch work in Phase 0; standalone per-animal tasks wait for an explicit Preventive Care (PC) catch-up action contract. 1M animals must never become ~1M human tasks.
2. **No far-future work in Cloud Tasks.** Future due state lives only in `obligation_instances` (Postgres). Cloud Scheduler + sweeper enqueue Cloud Tasks for the near-term window only; a lost task is re-derived from Postgres. Cloud Tasks/Pub-Sub are never the source of truth.
3. **Due scans use the `(tenant_id, status, due_at)` index** and touch only the current partition/window — never a full-table or full-herd scan. Sweeper pages through results, bounded batch size, resumable.
4. **Archive/partition policy for growth.** `obligation_instances`, `obligation_batches`, `vaccination_completions`, and `obligation_status_events` are RANGE-partitioned by date (there is **no** `vaccination_schedule` table — per-animal due state lives in `obligation_instances`); `completed`/`canceled`/`superseded` rows roll off hot partitions to cold/archive on a retention policy so the hot set stays bounded (~current + near-future). Control Tower never aggregates over cold history live.
5. **Control Tower / dashboards read Postgres projections, never raw fact scans and never queue state.** Coverage/overdue come from `*_projection` tables (committed projection-contract pattern), refreshed by workers.
6. **Stock is ledger-based, never direct decrement.** All quantity changes are `inventory_stock_movements` rows; balances are a same-txn projection. Reserve at batch start (vaccination) / packing (feed); consume+release at close.
7. **Query-plan validation is mandatory** for the sweeper's due-scan, the backfill generator, and every Control Tower/projection query (extend `make validate-sqlc-plans`). A query that can table-scan herd animals/events/obligations at scale fails CI.
8. **Idempotent everywhere.** Generation, consumers, stock movements, batch creation all key on deterministic ids and no-op on replay (at-least-once delivery is assumed).

**Drift guardrails (the three things that kill 1M scale):** do NOT let implementation drift into (a) per-animal tasks, (b) direct stock decrement, (c) queue-as-source-of-truth.
