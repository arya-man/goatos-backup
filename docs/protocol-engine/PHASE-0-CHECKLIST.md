# Phase 0 — Execution Checklist (pre-migration gate)

**Date:** 2026-06-23 · Build foundation deltas + the empty engine. **No rule values, no production `protocol_rules`, no state-machine handlers** (those are Phase 1). Phase 0 = schema + repos + outbox Pub/Sub adapter + capability/structural seeds.

---

## 0. Guardrails (every commit)
- **Repo:** `vgoats/goatos` only. Push via `zsh -ic 'git mesha-push main'` (MESHA_GITHUB_PAT). **Never `gh`** (authed as wrong account). Branch off `main`, no direct commits to `main`; no PRs (review-and-counter flow).
- **GCP (only when wiring Pub/Sub):** verify active account = `ravi@mesha.sg`, org `vgoats.com`, project `goatos-dev`. **Never** Heva / Slice / `hevaplatform` / `goatos-sheets`. State account+project before any gcloud.
- **No production `protocol_rules`.** Seeds allowed: `animal_stage_lookup` bands, location profiles from **real `locations`**, capability rows, a vaccination SOP `form_dsl`/`proof_policy` skeleton. Any rule row stays `status='draft'` (with a **`not source-backed`** warning) only, never `published`. **No fabricated PPR/FMD/ET schedule values.**

## 1. Migrations `000070–074` (each: up + down, sqlc-gen, plan-checked)

| Migration | Tables / changes | Constraints + indexes |
|---|---|---|
| **`000070_goats_provenance_lifecycle`** | ALTER `goats`: + `dob date`, `dob_estimated bool NOT NULL DEFAULT true`, `origin_type text`, `entry_date date`, `exited_at timestamptz`, `exit_reason text`. Tagging = **view** `vw_goat_tagging` over `goat_identifiers` (no column). | CHECKs: `origin_type IN(birth,procured,imported,unknown)`, `exit_reason IN(sold,died,culled,transferred,lost)`, tighten `lifecycle_status`/`health_status`/`management_stage` to CHECK enums; CHECK `exited_at NOT NULL ⇒ lifecycle_status in exited-set`. (reuse existing `goats_*` indexes) |
| **`000071_location_profiles`** | `farm_profiles`, `park_profiles`, `shed_profiles` (1:1, `location_id` PK→`locations`); `animal_stage_lookup`; `shed_lifecycle_status_lookup`. | FKs → `locations(location_id)`; **location_type guard trigger** per profile (mirror `validate_user_scope_grant`); UNIQUE `(tenant_id, stage_code)` / `(tenant_id, status_code)`; idx `shed_profiles(animal_stage_id)`. |
| **`000072_inventory_foundation`** | `inventory_items` (`category` CHECK, `base_unit`), `vaccines` (FK item), `inventory_stock` (numeric balances + `quantity_unit`), **`inventory_stock_movements`** (append-only ledger). | `CHECK(quantity_in_stock>=0)`, `CHECK(quantity_reserved>=0)`; movements UNIQUE `(tenant_id, idempotency_key)`; **FEFO idx** `(tenant_id, location_id, item_id, expiry_date) WHERE quantity_in_stock>0`; movements idx `(tenant_id, lot_id, occurred_at)`. |
| **`000073_protocol_engine`** | `protocol_definitions`, `protocol_versions` (scope_type/scope_id), `protocol_rules`, `protocol_triggers`. Seed **capabilities** `protocol.draft.vaccination`/`.feed_direction` + `protocol.publish.*` (explicit, no wildcard) → grant COO/CEO. | UNIQUE `(tenant_id, code)`; **`EXCLUDE` (GiST)** non-overlap on `daterange(effective_from,effective_to)` per `(tenant_id, protocol_id, scope_type, COALESCE(scope_id,sentinel))` WHERE published (needs `btree_gist`); CHECK effective dates. |
| **`000074_obligation_engine`** | `obligation_instances`, `obligation_batches`, `obligation_status_events` (**RANGE-partition by `recorded_at`**, monthly + default, like `goat_identity_events`), `obligation_escalations`. | obligation UNIQUE `(tenant_id, idempotency_key)`; **dup-guard** UNIQUE `(tenant_id, protocol_version_id, rule_id, target_type, target_id, due_at)` **`NULLS NOT DISTINCT`**; status CHECK; scope-validate trigger. Indexes: due-window `(tenant_id,status,due_at,obligation_id)`; per-target `(tenant_id,target_type,target_id,status)`; per-scope `(tenant_id,scope_type,scope_id,status,due_at)`; per-batch `(tenant_id,batch_id,status)`. |

## 2. Idempotency keys (assume at-least-once everywhere)
- **Obligation:** deterministic `idempotency_key = hash(tenant_id·protocol_version_id·rule_id·target_type·target_id·due_at·sequence)` — generation is a no-op on replay.
- **Stock movement:** every reserve/consume/release carries `idempotency_key`, UNIQUE per tenant.
- **Reuse committed** `idempotency_keys` table for consumer handlers (Phase 1) and `outbox_messages.event_id` UNIQUE for egress.

## 3. Query-plan validation (million-goat — mandatory in CI)
Extend `make validate-sqlc-plans` with EXPLAIN assertions:
- Obligation **due-window scan** uses `(tenant_id,status,due_at)` index + partition pruning — **no seq scan** on `obligation_instances`.
- Per-target & per-scope lookups index-only/index-scan.
- **FEFO lot pick** uses the partial expiry index.
- Movements ledger insert + balance update bounded.
- **Reject any plan that full-scans** `goats`/`obligation_instances`/`inventory_stock_movements`. Add to CI; fail the build on a bad plan.

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
Migrations `000070–074` applied to `goatos-dev`; `internal/protocol`,`internal/obligation`,`internal/inventory` domains scaffolded (domain/app/ports/adapters, like `sop`/`outbox`) with repos + tests; outbox publishes to Pub/Sub; capability + structural + draft test seeds in (`not source-backed`, no fake source-backed/approved rules); CI plan-checks green; **engine stands up empty** — ready for Phase 1 (vaccination domain + state-machine handlers) once rule values arrive.
