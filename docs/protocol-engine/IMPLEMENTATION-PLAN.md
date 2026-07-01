# PHC Vaccination + Feed Direction — Implementation Plan (repo execution)

**Date:** 2026-06-23 · **Correction:** updated 2026-06-29 for Feed Direction. This file is a historical Phase 0/1 execution plan; the repo now contains migrations through `000117` at this correction. For Feed Direction, use [../feed-direction/TRD.md](../feed-direction/TRD.md) as the current design source. The old `000076-078` typed feed table plan below is superseded by the committed `000079` generic-kernel design.

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

### 2. Docs / spec only vs current code
- `obligation-engine.md`, `state-machines.md`, `migration-and-cutover.md`,
  PHC PRD/TRD, and Feed PRD/TRD remain design references.
- The generic `protocol_*`, `obligation_*`, and `inventory_*` schema is no
  longer merely planned; it exists in later migrations. Check the live migration
  tail before assigning any new number.
- Feed Direction is partially present as the `000079` structure plus
  `backend/internal/feed` execution/verification code, but no generation
  pipeline or production entry point exists yet.

### 3. Mock only (UI prototype, static data, no backend)
- `goatos/mock/goatos-dashboard-mock.html` — Control Tower, Action Center (adherence-grouped), **Protocol Adherence**, vaccination/feed module views, role switcher, scope toggle. All static.

### 4. Needs new migration / code
- For current work, never reuse the historical `000070-078` numbers. The next
  migration must follow the live repo tail.
- Feed Direction still needs generation run/snapshot/bridge/projection storage if
  the generic kernel cannot represent those facts directly, plus handlers per
  the corrected SM-6 state machine.
- Handlers per state machines; outbox publisher/consumer wiring as required by
  the active slice.

### 5. External inputs / source-backed follow-ups
- **Vaccination schedule values** — current source-backed schedule rows are in
  `docs/phc-vaccination/APPROVED-SCHEDULE-MATRIX.md`: ET+TT, PPR, Goat Pox,
  Sheep Pox, FMD, HS, and Blue Tongue with species split, dose, vial, repeat,
  procurement, pregnancy, and gap rules. BQ and any future override still need
  timing/dose/booster/route/storage evidence from PHC/vet sign-off or a reviewed
  source extract before they can generate obligations.
- **Feed ration VALUES** + session clock times per park.
- **CUTOVER_DATE** + vaccination-history-trust decision (migration-and-cutover §6).
- The **4 superadmin mail IDs** + capability grants (`protocol.publish.<category>`).
- **K2 age band** — closed for local/dev at 42 days / six weeks; any later PHC
  override must land as source-backed `animal_stage_lookup` data/config, not a
  frontend hardcode.

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
| `000076-000078` historical feed rows | **Superseded. Do not implement as written.** Feed config is generic `protocol_versions.rule_dsl`; directions are obligations/batches; execution is `feed_direction_completions`; only add feed-specific run/snapshot/bridge/projection tables where the generic kernel has no natural home. |

Reuse exemplars: `000001` (partition+outbox+idempotency), `000030` (projection + COALESCE-unique), `000050` (RBAC+caps), `000060` (SOP engine).

---

## C. Backend services / handlers (Cloud Run, by runtime role)
- **`api`** — config CRUD (protocol draft/publish + impact-preview), completion submit, SOP submission. New Go domains: `protocol/`, `obligation/`, `inventory/`, build out `vaccination/`, `feed/` (hexagonal: domain/app/ports/adapters, like `sop`/`outbox`).
- **`consumer`** (Pub/Sub push) — **SM-1** schedule-gen (`goat.created`), **SM-2** shift-recompute (`goat.shifted`), **SM-3** death/sale cancel (`goat.exited`), **SM-7** booster-gen (`vaccination.completed`). Idempotent.
- **`sweeper`** (Cloud Run Job ← Cloud Scheduler) — **SM-4** batch create + due-flip; **SM-6** feed generation (full direction + cutoff Diff + bridge logging). Chunked by tenant/park/date.
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
- **Build now:** Admin Config / Protocol Rules (`/config`), PHC Vaccination module surface, vaccination execution context scoped by park/shed, contextual Goat Passport vaccination history, and the work-state data needed by those screens.
- **Design now, full UI later:** standalone Action Center and Control Tower. Their status model must exist underneath PHC/Parks, but their full command-room surfaces should summarize real gaps only after the operating workflows are wired.
- **Later verticals:** feed direction, procurement, breeding, HR, analytics, and the other non-vaccination modules remain valid Goat OS scope, but must not pull this slice back into the old generic dashboard/admin phase ladder.

## F. Seed / draft test data
- **Allowed:** structural seeds — `animal_stage_lookup` bands, park/shed profiles from **real `locations`**, vaccination SOP `form_dsl`/`proof_policy` (from wiki §6 + PHC handbook), capability seeds, and **draft test `protocol_rules` (`status='draft'`, `source_system='manual_admin'`/`review_status='extracted'`)** — these carry a **`not source-backed`** warning, are never `published`, and generate no obligations.
- **Source-backed real config (dev):** a rule whose `source_system` is a real source (Vaccinations DB / PHC / vet) and `review_status='approved'` is **real config in `goatos-dev`, publishable there** — the **dev-real path** — and may generate real dev obligations. Unsourced / `manual_admin` / `extracted` rows stay `status='draft'` and carry the **`not source-backed`** warning; they cannot be published.
- **NOT allowed:** **hand-invented** vaccine/feed schedule values as production logic. Unsourced draft rules (warning: `not source-backed`) never generate production obligations and cannot be published.

## G. Order of implementation
**Phase 0 — foundation deltas (no rule values needed):** `000070`–`000074` + outbox→Pub/Sub adapter + create `protocol`/`obligation`/`inventory` Go domains (schema + repos, no rules yet). Engine stands up empty.
**Phase 1 — PHC Vaccination slice:** `000075`; build `vaccination` domain; SM-1/3/4/5/7 handlers; SOP form/proof seed; config-screen API + impact-preview; Control Tower + Action Center + Protocol Adherence reads + coverage projection. **Source-derived rule values now exist** in `docs/phc-vaccination/APPROVED-SCHEDULE-MATRIX.md`; use them as source-backed dev config for ET+TT, PPR, Goat Pox, Sheep Pox, FMD, HS, and Blue Tongue. BQ and future overrides arrive later through the same source-backed versioning path when timing/dose/booster extracts exist.
**Phase 2 — Feed Direction:** use new migration numbers after the live repo tail; build the generation pipeline and wiring around the committed `000079` generic-kernel design. SM-6 is full direction + cutoff Diff + bridge logging + packing reserve. Feed operational screens remain scope-gated until explicitly reopened.
**Cutover (one-time, after Phase 1 engine ready):** freeze legacy → canonical → backfill generator per migration-and-cutover.md; `CUTOVER_DATE` policy (no historical-overdue flood).

**Critical-path gate:** Phase 0 + the *engine* of Phase 1/2 can be built **now**. For PHC vaccination local/dev, the approved schedule matrix can generate source-backed work for its listed vaccine rows; BQ and future overrides remain source-backed config/versioning work.
