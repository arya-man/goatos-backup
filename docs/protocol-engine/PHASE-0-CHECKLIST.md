# Phase 0 — Execution Checklist (pre-migration gate)

**Date:** 2026-06-23 · Build foundation deltas + the empty engine. **No rule values, no production `protocol_rules`, no state-machine handlers** (those are Phase 1). Phase 0 = schema + repos + outbox Pub/Sub adapter + capability/structural seeds.

**Path B target correction (2026-07-03):** the clean base model is
mixed-species `herd_animals` / `animal_id`, not goat-only storage. Any checklist
item below that names `goats`, `goat_id`, or `goat.*` is legacy implementation
context and must be replaced by the animal model when this base correction is
implemented.

---

## 0. Guardrails (every commit)
- **Repo:** `vgoats/goatos` only. For ordinary work use the current repository
  landing contract in `AGENTS.md`; never issue an ambient direct-main push.
  When this checklist is executed inside the approved whole-ledger/task-kernel
  program, its single integration PR and `make land-integration-pr PR=<number>`
  contract supersede this checklist's historical no-PR/direct-main workflow.
- **GCP (only when wiring Pub/Sub):** verify active account = `ravi@mesha.sg`, org `vgoats.com`, project `goatos-dev`. **Never** Heva / Slice / `hevaplatform` / `goatos-sheets`. State account+project before any gcloud.
- **No production `protocol_rules`.** Seeds allowed: `animal_stage_lookup` bands, location profiles from **real `locations`**, capability rows, and a vaccination SOP `form_dsl`/`proof_policy` skeleton. Draft rule rows never publish or generate obligations unless a later CEO/COO-authorized protocol version passes JSON-schema validation, SOP binding, impact preview, and effective-date checks. **No fabricated PPR/FMD/ET schedule values.**

## 1. Migrations `000070–074` (each: up + down, sqlc-gen, plan-checked)

| Migration | Tables / changes | Constraints + indexes |
|---|---|---|
| **`000070_herd_animals_identity`** | Create/rename `herd_animals`: + `animal_id`, species reference, breed reference, DOB confidence, origin/entry, lifecycle/exit, current location/shed/cohort, merge target. Tagging = derived from `animal_identifiers` (no denormalized tag column). | CHECKs/FKs: required `species_catalog`, no goat-only species CHECK, origin/exit enums, lifecycle/exit consistency. Add animal/shed/species indexes for matrix generation. |
| **`000071_location_profiles`** | `farm_profiles`, `park_profiles`, `shed_profiles` (1:1, `location_id` PK→`locations`); `animal_stage_lookup`; `shed_lifecycle_status_lookup`; Goats and Parks shed/tag age policy data. | FKs → `locations(location_id)`; **location_type guard trigger** per profile (mirror `validate_user_scope_grant`); UNIQUE `(tenant_id, stage_code)` / `(tenant_id, status_code)`; idx `shed_profiles(animal_stage_id)`. |
| **`000072_inventory_foundation`** | `inventory_items` (`category` CHECK, `base_unit`), `vaccines` (FK item), `inventory_stock` (numeric balances + `quantity_unit`), **`inventory_stock_movements`** (append-only ledger). | `CHECK(quantity_in_stock>=0)`, `CHECK(quantity_reserved>=0)`; movements UNIQUE `(tenant_id, idempotency_key)`; **FEFO idx** `(tenant_id, location_id, item_id, expiry_date) WHERE quantity_in_stock>0`; movements idx `(tenant_id, lot_id, occurred_at)`. |
| **`000073_protocol_engine`** | `protocol_definitions`, `protocol_versions` (scope_type/scope_id), `protocol_rules`, `protocol_triggers`. Seed explicit `protocol.publish.<category>` capabilities; V1 vaccination Config route is CEO/CXO only. | UNIQUE `(tenant_id, code)`; **`EXCLUDE` (GiST)** non-overlap on `daterange(effective_from,effective_to)` per `(tenant_id, protocol_id, scope_type, COALESCE(scope_id,sentinel))` WHERE published (needs `btree_gist`); CHECK effective dates. |
| **`000074_obligation_engine`** | `obligation_instances`, `obligation_batches`, `obligation_status_events` (**RANGE-partition by `recorded_at`**, monthly + default, like `animal_identity_events`), `obligation_escalations`. | obligation UNIQUE `(tenant_id, idempotency_key)`; **dup-guard** UNIQUE `(tenant_id, protocol_version_id, rule_id, target_type, target_id, due_at)` **`NULLS NOT DISTINCT`**; status CHECK; scope-validate trigger. Indexes: due-window `(tenant_id,status,due_at,obligation_id)`; per-target `(tenant_id,target_type,target_id,status)`; per-scope `(tenant_id,scope_type,scope_id,status,due_at)`; per-batch `(tenant_id,batch_id,status)`. |

## 2. Idempotency keys (assume at-least-once everywhere)
- **Obligation:** deterministic `idempotency_key = hash(tenant_id·protocol_version_id·rule_id·target_type·target_id·due_at·sequence)` — generation is a no-op on replay.
- **Stock movement:** every reserve/consume/release carries `idempotency_key`, UNIQUE per tenant.
- **Reuse committed** `idempotency_keys` table for consumer handlers (Phase 1) and `outbox_messages.event_id` UNIQUE for egress.

## 3. Query-plan validation (million-animal — mandatory in CI)
Extend `make validate-sqlc-plans` with EXPLAIN assertions:
- Obligation **due-window scan** uses `(tenant_id,status,due_at)` index + partition pruning — **no seq scan** on `obligation_instances`.
- Per-target & per-scope lookups index-only/index-scan.
- **FEFO lot pick** uses the partial expiry index.
- Movements ledger insert + balance update bounded.
- **Reject any plan that full-scans** `herd_animals`/`obligation_instances`/`inventory_stock_movements`. Add to CI; fail the build on a bad plan.

## 4. Outbox → Pub/Sub adapter
- Implement `internal/outbox/adapters/publisher/pubsub` satisfying the existing `ports.Publisher` (today only `publisher/logging` stub exists). Keep the committed lease-based relay (`ClaimPending→publish→MarkPublished/DeadLetter`).
- Pub/Sub **topic + dead-letter topic + retry policy** in `goatos-dev` (vgoats.com). Publish idempotent on `event_id`. Local dev uses the **Pub/Sub emulator** (`PUBSUB_EMULATOR_HOST`).
- Select adapter by env (`logging` local default vs `pubsub` deployed) — zero code change between envs.

## 5. Tests (Phase 0 scope)
- Migration up/down apply clean; sqlc regen has no drift (`make validate-sqlc-plans` / sqlc-check green).
- Postgres integration test per new table (CRUD via repo).
- **Constraint tests:** `CHECK(qty>=0)`; active-config EXCLUDE non-overlap (overlapping publish rejected); obligation dup-guard incl. `NULLS NOT DISTINCT`; scope-validate trigger; location_type guard.
- Query-plan assertions (§3).
- Outbox Pub/Sub adapter: publish success · transient retry · poison→DLQ · dedup on `event_id`.
- **No** SM-1…SM-7 handler tests (Phase 1).

## 6. Definition of done (Phase 0)
Migrations `000070–074` applied to `goatos-dev`; `internal/protocol`,`internal/obligation`,`internal/inventory` domains scaffolded (domain/app/ports/adapters, like `sop`/`outbox`) with repos + tests; outbox publishes to Pub/Sub; capability + structural + draft test seeds in (no fake published business rules); CI plan-checks green; **engine stands up empty** — ready for Phase 1 (vaccination domain + state-machine handlers) once rule values arrive.
