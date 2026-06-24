# PHC Vaccination + Feed Direction — Implementation Plan (repo execution)

**Date:** 2026-06-23 · **Grounded in** the committed repo (34 migrations, `000001–000060`; `internal/` domains) + the build-spec docs ([obligation-engine](./obligation-engine.md) · [state-machines](./state-machines.md) · [migration-and-cutover](./migration-and-cutover.md) · PHC/Feed PRD-TRD).

---

## A. Status separation (what's real vs not)

### 1. Already implemented in backend (REUSE as-is)
| Area | Tables (migration) | Go code |
|---|---|---|
| Identity | `goats`, `goat_identifiers`, `goat_location_history`, `goat_identity_events`(partitioned) (`000001-22`) | `internal/identity` |
| Locations | `locations` + `location_operational_attributes` + `location_capacity_records` (`000024-26`) | `internal/locations` |
| Workforce / RBAC | `workforce_*`, `user_scope_grants`, `auth_pending_email_grants` (`000050`, `000023`) | `internal/workforce`, `internal/permissions` |
| **SOP engine** | `sop_definitions/versions/tasks/submissions/submission_items` (`000060`) | **`internal/sop`** — `app/service.go` (`ValidateFormDSL`, `Evaluate`), http handler, postgres repo ✅ |
| **Transactional outbox** | `outbox_messages`, `idempotency_keys` (`000001`) | **`internal/outbox`** — lease-based relay (ClaimPending→publish→MarkPublished), postgres repo, **logging publisher stub** ✅ |
| Projection-contract | `feature_coverage_registry`, process-integrity projection contracts | Obligation/vaccination projections; do not mirror deleted counts/mortality runtime modules |
| Audit | `audit_log` (partitioned) (`000001`) | platform |

### 2. Docs / spec only (written, zero code)
- `obligation-engine.md` (+ §9 scale hard-rules, §2.1 config-UI contract), `state-machines.md` (SM-1…SM-7), `migration-and-cutover.md`, PHC PRD/TRD, Feed PRD/TRD.
- The `protocol_*`, `obligation_*`, `inventory_*` schema + the 7 state machines = fully designed, **not built**.

### 3. Mock only (UI prototype, static data, no backend)
- `goatos/mock/goatos-dashboard-mock.html` — Control Tower, Action Center (adherence-grouped), **Protocol Adherence**, vaccination/feed module views, role switcher, scope toggle. All static.

### 4. Needs new migration / code
- **Migrations `000070–078`** (below). `internal/vaccination` + `internal/feed` = **stubs** (build out); `internal/protocol`, `internal/obligation`, `internal/inventory` = **absent** (create).
- Handlers per state machines; outbox **Pub/Sub publisher adapter** (today only the logging stub exists).

### 5. Human-input blockers (cannot proceed without)
- **Vaccination schedule VALUES** — vaccine × age/stage × interval × dose × booster × route × storage-temp. **Absent in every source.** From PHC/vet sign-off or a reviewed extract of the live "Vaccination DB" sheet (Slack `F0AK7S3NR4K`/`F0AKC6EBD9U`).
- **Feed ration VALUES** + session clock times per park.
- **CUTOVER_DATE** + vaccination-history-trust decision (migration-and-cutover §6).
- The **4 superadmin mail IDs** + capability grants (`protocol.publish.<category>`).
- **K2 age band** 42 vs 45 (make it config).

---

## B. Migrations needed
| # | Contents |
|---|---|
| `000070_goats_provenance_lifecycle.sql` | ALTER `goats`: `dob`, `dob_estimated`, `origin_type`, `entry_date`, `exited_at`, `exit_reason`; CHECKs on lifecycle/health/stage. (tagging = derived view, no column) |
| `000071_location_profiles.sql` | `farm_profiles`, `park_profiles`, `shed_profiles`, `animal_stage_lookup`, `shed_lifecycle_status_lookup` + location_type guard triggers |
| `000072_inventory_foundation.sql` | `inventory_items`, `vaccines`, `inventory_stock` (numeric + `CHECK≥0`), `inventory_stock_movements` ledger |
| `000073_protocol_engine.sql` | `protocol_definitions/versions/rules/triggers`; GiST non-overlap on published windows; `protocol.draft.<cat>`/`protocol.publish.<cat>` capability seeds (explicit, no wildcard) |
| `000074_obligation_engine.sql` | `obligation_instances` (rule_id in guard, NULLS NOT DISTINCT, deterministic idempotency_key), `obligation_batches`, `obligation_status_events` (RANGE-partitioned), `obligation_escalations` |
| `000075_vaccination_module.sql` | `vaccination_completions` (UNIQUE, batch+inventory FK); ALTER `feature_coverage_registry` +`vaccination`; vaccination SOP definition/version seed |
| `000076_feed_config.sql` | `feed_master`, `goat_class_energy_requirement` (**key `(tenant_id, breed_id, animal_stage_id)` → `animal_stage_lookup`, NOT shed_status**), `feed_pattern(+feeds)`, `feed_session_templates(+items)` |
| `000077_feed_plan_directions.sql` | `supply_planning` (replace-not-update), `feed_directions` (v1/v2 supersede) |
| `000078_feed_execution.sql` | `feed_packing`, `feed_consumption`, `feed_transport`; +`feed_direction` in coverage registry; feed SOP seeds; feed capability seeds |

Reuse exemplars: `000001` (partition+outbox+idempotency), `000030` (projection + COALESCE-unique), `000050` (RBAC+caps), `000060` (SOP engine).

---

## C. Backend services / handlers (Cloud Run, by runtime role)
- **`api`** — config CRUD (protocol draft/publish + impact-preview), completion submit, SOP submission. New Go domains: `protocol/`, `obligation/`, `inventory/`, build out `vaccination/`, `feed/` (hexagonal: domain/app/ports/adapters, like `sop`/`outbox`).
- **`consumer`** (Pub/Sub push) — **SM-1** schedule-gen (`goat.created`), **SM-2** shift-recompute (`goat.shifted`), **SM-3** death/sale cancel (`goat.exited`), **SM-7** booster-gen (`vaccination.completed`). Idempotent.
- **`sweeper`** (Cloud Run Job ← Cloud Scheduler) — **SM-4** batch create + due-flip; **SM-6** feed generation (midnight v1 + 2 PM v2). Chunked by park/date.
- **`outbox-relay`** — exists; swap logging publisher → **Pub/Sub adapter** (only net-new piece here).
- Execution reuses committed `internal/sop`.

## D. How the tables connect (the chain)
```
protocol_definitions → protocol_versions (PUBLISHED, effective-dated) → protocol_rules + protocol_triggers
   └─SM-1/SM-7→ obligation_instances (per goat/shed · rule_id · due_at)        [Postgres = due truth]
        └─SM-4 sweeper→ obligation_batches (drive) → spawns ONE sop_task (from protocol_version.sop_version_id)
             └─ operator → sop_submission + sop_submission_items (+ GCS proof)
                  └─ verification → vaccination_completions (links obligation_id + batch_id + sop_submission_item_id + inventory lot)
                       └─SM-5→ inventory_stock_movements (reserve/consume/release) · obligation_status_events · outbox event
                            └─ projections (vaccination_coverage) → Control Tower · Protocol Adherence · Action Center
```
Adherence = **computed**: expected (rule) vs actual (completion + proof + timing). `sop_versions` = the *how*; obligations = the *what's due*; protocol = the *what should happen*.

## E. Frontend: current slice vs later
- **Build now:** Admin Config / Protocol Rules (`/config`), PHC Vaccination Operations, Parks vaccination execution context, contextual Goat Passport vaccination history, and the work-state data needed by those screens.
- **Design now, full UI later:** standalone Action Center and Control Tower. Their status model must exist underneath PHC/Parks, but their full command-room surfaces should summarize real gaps only after the operating workflows are wired.
- **Later verticals:** feed direction, procurement, breeding, HR, analytics, and the other non-vaccination modules remain valid Goat OS scope, but must not pull this slice back into the old generic dashboard/admin phase ladder.

## F. Seed / draft test data
- **Allowed:** structural seeds — `animal_stage_lookup` bands, park/shed profiles from **real `locations`**, vaccination SOP `form_dsl`/`proof_policy` (from wiki §6 + PHC handbook), capability seeds, and **draft test `protocol_rules` (`status='draft'`, `source_system='manual_admin'`/`review_status='extracted'`)** — these carry a **`not source-backed`** warning, are never `published`, and generate no obligations.
- **Source-backed real config (dev):** a rule whose `source_system` is a real source (Vaccinations DB / PHC / vet) and `review_status='approved'` is **real config in `goatos-dev`, publishable there** — the **dev-real path** — and may generate real dev obligations. Unsourced / `manual_admin` / `extracted` rows stay `status='draft'` and carry the **`not source-backed`** warning; they cannot be published.
- **NOT allowed:** **hand-invented** vaccine/feed schedule values as production logic. Unsourced draft rules (warning: `not source-backed`) never generate production obligations and cannot be published.

## G. Order of implementation
**Phase 0 — foundation deltas (no rule values needed):** `000070`–`000074` + outbox→Pub/Sub adapter + create `protocol`/`obligation`/`inventory` Go domains (schema + repos, no rules yet). Engine stands up empty.
**Phase 1 — PHC Vaccination slice:** `000075`; build `vaccination` domain; SM-1/3/4/5/7 handlers; SOP form/proof seed; config-screen API + impact-preview; Control Tower + Action Center + Protocol Adherence reads + coverage projection. **Draft test rules** (`not source-backed`, UI/engine validation only, never published, no obligations) are used **only when no source values exist yet**; the moment Vaccinations DB / PHC / vet **approved** values arrive, `goatos-dev` runs **source-backed real config** that publishes and generates obligations — that is the main path. *(Real obligations gated on blocker §A5.)*
**Phase 2 — Feed Direction:** `000076`–`000078`; build `feed` domain; SM-6 (gen + 2 PM recompute + packing reserve); feed screens.
**Cutover (one-time, after Phase 1 engine ready):** freeze legacy → canonical → backfill generator per migration-and-cutover.md; `CUTOVER_DATE` policy (no historical-overdue flood).

**Critical-path gate:** Phase 0 + the *engine* of Phase 1/2 can be built **now** (no values needed). The system only generates **real** work once the **rule values** (§A5) arrive — until then it runs on **unpublished draft test rules (`not source-backed`, UI/engine validation only)**, replaced by source-backed real config as soon as approved values land.
