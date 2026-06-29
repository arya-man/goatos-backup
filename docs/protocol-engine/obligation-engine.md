# Goat OS — Generic Protocol & Obligation Engine

**Status:** Draft v2 correction · **Date:** 2026-06-29
**Grounded in:** the committed migrations in `backend/migrations/postgres/`
through the current tail (`000117` at this correction). The first draft was
written when the repo stopped near `000060`; current work must check the live
migration tail before adding tables or treating protocol/obligation/inventory as
absent.
**Why this doc:** PHC (vaccination, deworming, biosecurity, feed/water testing, panel cleaning, sanitization, fire-safety, SOP-video, stock checks, director reporting) **and** Feed Direction all need the same thing: admin-set rules → due obligations → tasks → proof → completion → projections. Build it **once**. Module-specific tables (e.g. `vaccination_completions`) link into this engine; they do not re-implement it.

---

## 0. Repo reality (what already exists — reuse, don't reinvent)

> Convention: **no native Postgres ENUMs** — every enum is `text` + `CHECK`. Every table is `tenant_id uuid NOT NULL REFERENCES tenants` scoped.

| Committed table | Verdict | Role for this engine |
|---|---|---|
| `goats`, `goat_identifiers`, `goat_location_history`, `goat_identity_events`(partitioned) | reuse-as-is | obligation targets + provenance |
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
admin rule (protocol_version, published)  →  trigger fires (goat born / cron / upstream completion)
  →  obligation_instances rows materialized in Postgres  [SOURCE OF TRUTH for "what is due"]
  →  Cloud Scheduler worker flips due rows, enqueues Cloud Tasks (near-term only)
  →  sop_task dispatched  →  operator submits via sop_submissions (+ proof)  →  verification
  →  module completion row (links obligation_id)  →  inventory_stock_movements reserve/consume/release (FEFO)  →  outbox event
  →  obligation_status_events ledger  →  projection refresh  →  Control Tower / Passport / Calendar
```

**Boundary:** GCP does plumbing (retry, DLQ, timers, delivery, storage). Goat OS owns domain (rules, eligibility, grouping, proof, escalation, stock). **Postgres stays the queryable truth; Control Tower never reads Cloud Tasks/Pub-Sub state.**

---

## 2. Authority model (corrected)

PHC/Feed Director **drafts**; COO/CEO **publishes**. Authority is **capability-based**, not role-name-based, so "any admin/tech user" cannot publish rules.

**Category-specific capabilities (required — scope alone cannot separate verticals).** `workforce_member_capabilities` scopes only by tenant/park/shed/cohort — it has **no category dimension**. A generic `protocol.draft` would therefore let a Feed Director draft PHC rules and vice-versa. So the capability code itself carries the category:
- `protocol.draft.vaccination`, `protocol.draft.feed_direction`, `protocol.draft.deworming`, … (and `protocol.publish.<category>`).
- These fit the committed `capability_code` regex (`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*$`). The PHC Director holds `protocol.draft.vaccination` (+ other PHC categories); the Feed Director holds `protocol.draft.feed_direction`.
- The API resolves the target `protocol_definitions.category` and requires the matching `protocol.{draft|publish}.{category}` capability at the relevant tenant/park scope.
- *(Acceptable fallback if you must keep generic codes: a generic `protocol.draft`/`protocol.publish` **plus** an explicit API authorization check that the actor's allowed categories include the target category. Per-category codes are preferred — the boundary is then enforceable from the capability model alone.)*

| Action | Required capability (category-specific, scoped tenant or park) | Effect |
|---|---|---|
| Draft / edit a `protocol_version` (status=`draft`) | `protocol.draft.<category>` (e.g. `…vaccination` for PHC Director) | mutable |
| **Publish** (`draft→published`, set `effective_from`) | **`protocol.publish.<category>`** (COO/CEO) | immutable; new effective window opens |
| Retire | `protocol.publish.<category>` | `effective_to` set (closes the window) |

Capability is checked via `workforce_member_capabilities` (same mechanism as `vaccination.execute`); `user_scope_grants.role` still gates the surface. Every transition writes `audit_log`. A rule change = a **new version**, never an in-place edit of a published one.

**Seeding rule (no wildcard):** there is no `protocol.publish.*` wildcard capability. The seed migration must grant COO/CEO an **explicit publish capability for every v1 category** — `protocol.publish.vaccination` AND `protocol.publish.feed_direction`. **Every new `protocol_definitions.category` added later MUST ship a matching `protocol.draft.<category>` + `protocol.publish.<category>` capability seed**, or no one can author/publish it. (Revisit a wildcard only if category count grows unwieldy.)

**Versioning is effective-window-based, not "one published ever":** multiple published versions coexist across time; their `[effective_from, effective_to)` windows **must not overlap** per `(tenant_id, protocol_id, scope)`. The active version for a moment is `effective_from <= now < effective_to`. Schedules already generated keep the version they were generated under.

### 2.1 Config UI contract (CEO/COO authoring surface)

`protocol_rules` are **real business/medical/operations config — not public, not user-editable.** Only approved superadmins (CEO/COO/admin-style grants) create and **publish** real rules; Directors may **draft/propose** only if explicitly granted `protocol.draft.<category>`. Field/verifier/park users **never see or edit raw config** — they see generated obligations, SOP tasks, proof requirements, and their Action Center work.

**Config screen (CEO/COO):** create/edit a *draft* rule → link an `sop_version_id` → define proof policy → define escalation policy → **impact preview** → publish version.

**Rule fields:** module/`category` (vaccination, feed_direction, deworming, sanitation, …) · multi-factor eligibility (age, animal_stage, sex, breed, lifecycle, health_status, shed/cohort/park, reproductive[exclude pregnant/lactating], defer_states[ICU/quarantine/sick]) · **`schedule[]` — the Schedule Builder: an array of dose/phase rows** (dose_code · trigger_type[birth_age/post_arrival/calendar/after_previous_completion/manual_campaign] · offset_days · due_window_days · min_gap_days · repeat[none/every_n_days/yearly; age-window repeats rejected until generator support lands] · repeat_until_after_age · catch_up · per-dose sop_label[display only — executable SOP binds at version sop_version_id] + proof_policy) — NOT a single trigger-day + booster flag · `missed_dose_policy` (immediate/next_cycle/phc_approval/defer) · park override (scope_type/scope_id) · `effective_from`/`effective_to`. Next due is derived by trigger/repeat/catch-up logic plus trusted accepted completion evidence; there is no separate next-due-basis DSL field. **Source/review metadata — nested `source` object on `rule_dsl`** (canonical shape; matches Config screen JSON): `source:{ source_system` (vaccinations_db/phc/vet/manual_admin) `· source_ref · imported_at · reviewed_by · review_status` (extracted→reviewed→approved) `· approved_by · approved_at }`.

**Publish behavior:** production obligations generate **only** from `status='published'` rules; drafts generate **no** live work; published versions are **immutable** (a change = a new version); old versions stay auditable; **only CEO/COO/superadmin publish** (`protocol.publish.<category>`).

**Impact preview (required before publish — computed from `schedule[]`):** affected goats/sheds/cohorts (from eligibility); **obligations per cycle** (= affected × non-recurring doses); **annual-repeat count** (rows with `repeat:yearly`); **catch-up count** and **existing-history count** (goats with prior accepted completions → next-due from last completion, not DOB); expected stock required; expected SOP tasks/batches; and **risks** — missing stock, no assigned operator, missing executable `sop_version_id` (a free-text `sop_label` does not count), conflicting rule, effective-date overlap. Publish is gated on the operator reviewing this.

**Config visibility matrix:**
| Role | Config access |
|---|---|
| CEO/COO/superadmin | full config + **publish** |
| Director | draft/propose/view — only with `protocol.draft.<category>` |
| Park Head / Manager | view *effective instructions/tasks*, not raw config (unless explicitly granted) |
| Field worker | **no config** — Action Center + SOP execution only |
| Verifier | **no config** — proof queue only |

**Publish gate + dev policy (source-backed):** a version publishes **only when `review_status='approved'`** and source-backed. Values that come from a **real source (Vaccinations DB / PHC / vet-approved / committed PHC PRD source finding)** and are marked `approved` are **real config in `goatos-dev` and publishable there** — the **dev-real path**. **Unsourced / `manual_admin` / `extracted`** rows stay `status='draft'` and carry a **`not source-backed`** warning — they cannot be published and generate no production work. **Never hand-invent** PPR/FMD/ET schedule values; the local/dev ET row is source-derived from `docs/phc-vaccination/PRD.md:60` and future values arrive via this config UI or a reviewed source extract.

---

## 3. Schema — protocol layer (the ruleset, admin-authored)

### `protocol_definitions` (mirror of `sop_definitions`)
`protocol_id PK · tenant_id · code text CHECK (~ '^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*$') · name · category text CHECK · status text CHECK(draft/active/retired) · created_by · created_at · updated_at · row_version` — UNIQUE `(tenant_id, code)`.
`category CHECK IN ('vaccination','deworming','biosecurity','feed_water_testing','panel_cleaning','sanitization','fire_safety','sop_video','stock_check','director_reporting','feed_direction')`.

### `protocol_versions` (immutable, effective-dated — mirror of `sop_versions`)
`protocol_version_id PK · tenant_id · protocol_id→protocol_definitions · scope_type text CHECK(tenant/park) DEFAULT 'tenant' · scope_id uuid NULL (park for a per-park calendar; NULL ⇔ scope_type='tenant' = tenant-wide default) · version int(>0) · version_label · status text CHECK(draft/published/retired) · effective_from date NOT NULL · effective_to date NULL · rule_dsl jsonb · proof_policy jsonb · sop_version_id uuid NULL→sop_versions · drafted_by · published_by NULL · published_at NULL · retired_at · row_version`. CHECK: `scope_type='park' ⇒ scope_id NOT NULL` and `scope_type='tenant' ⇒ scope_id IS NULL`.
- **Non-overlap, not one-published-ever:** an **`EXCLUDE` constraint (GiST)** over `daterange(effective_from, effective_to)` keyed on `(tenant_id, protocol_id, scope_type, COALESCE(scope_id, '00000000-…-0'::uuid))` where `status='published'` — published windows cannot overlap **within the same scope** (a park calendar and the tenant default coexist), but past/present/future published versions all coexist. (`btree_gist` for the equality columns; the COALESCE sentinel collapses the nullable scope_id.) App-layer validation if GiST is undesirable.
- CHECK `effective_to IS NULL OR effective_to > effective_from`. Open-ended (`effective_to NULL`) = the current version; publishing the next one closes the prior's window.
- `sop_version_id` = the execution form to instantiate on dispatch.

### `protocol_rules` (cadence / eligibility expansion — **one row per dose/phase**)
`rule_id PK · tenant_id · protocol_version_id→protocol_versions · dose_code text (primary/booster_1/booster_2/annual/catch_up/…) · sequence int · trigger_type text CHECK(birth_age/post_arrival/calendar/after_previous_completion/manual_campaign) · offset_days int · due_window_days int · min_gap_days int · repeat text CHECK(none/every_n_days/yearly) · repeat_until_after_age text · catch_up text CHECK(immediate/next_cycle/phc_approval/defer) · eligibility_json jsonb · sop_version_id uuid NULL (per-dose override) · proof_policy jsonb · withdrawal_days int NULL · sort_order int`.

### `rule_dsl` shape (the authored ruleset — **multi-dose / multi-phase**, source of truth on `protocol_versions`)
A rule is **not** a single trigger+booster. The editor authors and stores in `protocol_versions.rule_dsl` (jsonb, JSON-Schema-validated), then the engine **expands each `schedule[]` entry into one `protocol_rules` row**:
```
{ category, scope:{type,id},
  eligibility:{ age_band, animal_stage, sex, breed, lifecycle_status, health_status,
                park/shed/cohort, reproductive(any|exclude_pregnant|exclude_lactating|pregnant_only),
                defer_states:[ICU,quarantine,sick] },
  missed_dose_policy: immediate | next_cycle | phc_approval | defer,
  schedule: [                                 // ARRAY — multi-dose / lifecycle phases
    { dose_code, sequence, trigger_type, offset_days, due_window_days, min_gap_days,
      repeat, repeat_until_after_age, catch_up, sop_label, proof_policy:[…] }, … ],
  escalation,
  source:{ source_system, source_ref, imported_at, reviewed_by,        // provenance + approval gate
           review_status, approved_by, approved_at } }
```
**Lifecycle phases** (e.g. 0–12mo primary+booster, >12mo `repeat:yearly` on a separately eligible annual row) are expressed as multiple `schedule` rows, not one booster flag. The whole `rule_dsl` is one immutable published `protocol_version`; a schedule change = a new version.

**Executable SOP binding vs. display label (publish gate truth).** The **executable** SOP for a version is `protocol_versions.sop_version_id` — a **real published SOP-version UUID** the Config UI selects from the SOP Library. The publish execution-contract gate (`protocol/app/publish.go` `ValidateExecutionContract`) requires that UUID plus an object-shaped `proof_policy`. The per-dose `schedule[].sop_label` is a **display label only** — the current Config UI emits it as `sop_label` (never as an executable `sop_version`), so a free-text label like `"vacc-sop v2"` can never satisfy the gate. A **genuine per-dose executable override** is still supported by the schema (`protocol_rules.sop_version_id`, a real UUID): use that column when a real per-dose SOP version exists; do not resurrect a text label as an executable field.

### `protocol_triggers` (what spawns obligations)
`trigger_id PK · tenant_id · protocol_version_id→protocol_versions · trigger_type text CHECK(schedule/goat_lifecycle/location_event/manual/upstream_completion) · trigger_config jsonb · is_active boolean`.
e.g. booster = `upstream_completion` of the prior obligation; feed direction = `schedule` (nightly).

---

## 4. Schema — obligation layer (the due state — **SOURCE OF TRUTH**)

### `obligation_instances` — far-future due rows live HERE, queryable
`obligation_id PK · tenant_id · protocol_version_id→protocol_versions · rule_id NOT NULL→protocol_rules · batch_id uuid NULL→obligation_batches · target_type text CHECK(goat/cohort/shed/park/tenant) · target_id uuid (interpreted per target_type: goat→goats, cohort/shed/park→locations, tenant→tenant_id) · scope_type/scope_id (org unit owning execution; vocabulary = sop_tasks: tenant/custodian_party/farm/park/shed/cohort) · due_at timestamptz NOT NULL · window_start NULL · window_end NULL · status text CHECK(scheduled/due/in_progress/completed/missed/waived/canceled/superseded) · sop_task_id uuid NULL→sop_tasks · idempotency_key text NOT NULL · generated_by_trigger_id NULL→protocol_triggers · sequence int DEFAULT 1 · completed_at · row_version`.
- **Deterministic idempotency_key** = `hash(tenant_id · protocol_version_id · rule_id · target_type · target_id · due_at · sequence)`. UNIQUE `(tenant_id, idempotency_key)`.
- **Duplicate-spawn guard:** UNIQUE active per `(tenant_id, protocol_version_id, rule_id, target_type, target_id, due_at)` — **includes `rule_id`** so the same goat can carry two different rules due the same day (e.g. two vaccines), and **`NULLS NOT DISTINCT`** (or `COALESCE(scope_id, sentinel)`), see §7.
- `batch_id` groups per-target obligations into a work unit (shed drive / feed session) — see `obligation_batches` below.
- Indexes: due-window scan `(tenant_id, status, due_at, obligation_id)`; per-target `(tenant_id, target_type, target_id, status)`; per-scope `(tenant_id, scope_type, scope_id, status, due_at)`; per-batch `(tenant_id, batch_id, status)`.

### `obligation_status_events` (append-only ledger — mirror `goat_identity_events`)
`obligation_event_id · tenant_id · obligation_id→obligation_instances · event_type text(scheduled/became_due/dispatched/completed/missed/waived/escalated) · occurred_at · recorded_at · actor_id · payload jsonb · idempotency_key · PK(obligation_event_id, recorded_at)` — **PARTITION BY RANGE(recorded_at)** monthly + `_default`, identical to committed `goat_identity_events`/`audit_log`.

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

The unit of execution is the **batch**, not the individual obligation: **many due obligations → grouped into one `obligation_batch` → one `sop_task`.** Phase 0 catch-up uses PHC-approved manual campaign obligations/batches; standalone per-goat individual override generation is not exposed.

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

Assignment uses `workforce_member_capabilities` (e.g. `vaccination.execute` scoped to the shed's park). Verification uses `proof.verify`. PHC-approved catch-up uses the same canonical obligation/batch path as other campaign work. No new task engine.

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
- **Group drive (batch):** `reserve` N doses against the FEFO lot at **batch start** (1 movement); `consume` the actual used + `release` the unused at **drive close / verification** (1–2 movements). **Never decrement per goat row** during a large shed drive.
- **Manual campaign / catch-up:** create canonical obligations/batches before execution; ad hoc per-goat individual override generation is not exposed in Phase 0.
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
| **PHC** | vaccination · deworming · biosecurity · feed/water testing · panel cleaning · sanitization · fire/safety · SOP-video verification · stock anti-misuse · director reporting |
| **Feed** (vertical) | feed-direction (config/ration -> next-day full direction -> cutoff Diff -> packing/staging -> execution -> wastage -> stock); later modules: feed-stock/loads, wastage/variance, ration-library |

Module-specific tables (`vaccination_completions`, `feed_direction_completions`, and optional generation/bridge/projection rows where the generic kernel has no natural home) link via `obligation_id` or run/proof identifiers. New modules add a category + protocol rules + an SOP form + (optionally) a completion/projection table — **no new engine.**

See: [PHC Vaccination TRD](../phc-vaccination/TRD.md) · [Feed Direction TRD](../feed-direction/TRD.md) · [State machines](./state-machines.md) · [Migration & cutover](./migration-and-cutover.md).

---

## 9. Million-goat scale — acceptance checklist (hard rules, enforce before SQL ships)

Non-negotiable invariants. A PR that violates any of these is rejected, not merged.

1. **No per-goat `sop_task` for group work.** Group execution = ONE `sop_task` per `obligation_batch` (shed drive / feed session). Catch-up is still canonical obligation/batch work in Phase 0; standalone per-goat tasks wait for an explicit PHC catch-up action contract. 1M goats must never become ~1M human tasks.
2. **No far-future work in Cloud Tasks.** Future due state lives only in `obligation_instances` (Postgres). Cloud Scheduler + sweeper enqueue Cloud Tasks for the near-term window only; a lost task is re-derived from Postgres. Cloud Tasks/Pub-Sub are never the source of truth.
3. **Due scans use the `(tenant_id, status, due_at)` index** and touch only the current partition/window — never a full-table or full-herd scan. Sweeper pages through results, bounded batch size, resumable.
4. **Archive/partition policy for growth.** `obligation_instances`, `obligation_batches`, `vaccination_completions`, and `obligation_status_events` are RANGE-partitioned by date (there is **no** `vaccination_schedule` table — per-goat due state lives in `obligation_instances`); `completed`/`canceled`/`superseded` rows roll off hot partitions to cold/archive on a retention policy so the hot set stays bounded (~current + near-future). Control Tower never aggregates over cold history live.
5. **Control Tower / dashboards read Postgres projections, never raw fact scans and never queue state.** Coverage/overdue come from `*_projection` tables (committed projection-contract pattern), refreshed by workers.
6. **Stock is ledger-based, never direct decrement.** All quantity changes are `inventory_stock_movements` rows; balances are a same-txn projection. Reserve at batch start (vaccination) / packing (feed); consume+release at close.
7. **Query-plan validation is mandatory** for the sweeper's due-scan, the backfill generator, and every Control Tower/projection query (extend `make validate-sqlc-plans`). A query that can table-scan goats/events/obligations at scale fails CI.
8. **Idempotent everywhere.** Generation, consumers, stock movements, batch creation all key on deterministic ids and no-op on replay (at-least-once delivery is assumed).

**Drift guardrails (the three things that kill 1M scale):** do NOT let implementation drift into (a) per-goat tasks, (b) direct stock decrement, (c) queue-as-source-of-truth.
