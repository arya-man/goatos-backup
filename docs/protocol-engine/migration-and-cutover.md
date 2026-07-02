# Goat OS — Legacy → Canonical Migration & Preventive Care (PC) Cutover

**Status:** Draft v1 · **Date:** 2026-06-23
**Companion:** [obligation-engine.md](./obligation-engine.md) · [state-machines.md](./state-machines.md) · [Preventive Care (PC) TRD](../preventive-care-vaccination/TRD.md)
**Context:** legacy BQ/Sheets data was already ported into `goatos-dev` (snapshot-based). This doc says how to turn that into clean canonical state that Preventive Care (PC) / Feed run on — **without** the new system depending on legacy forever, and **without** flooding Control Tower with fake historical breaches.

> **Prime rule:** Legacy data **seeds** canonical state; it does **not** drive the runtime. Once canonical goats are clean enough, vaccination/feed rules run from Goat OS Postgres only.

---

## 1. Freeze legacy as an archived source (never runtime truth)

Keep the raw imported data, but quarantine it from the running system:
- Archive every legacy/BQ/Sheets row with `source_system`, `source_row_id`, `imported_at`, and a `checksum`. Committed `legacy_import_*` / `legacy_sync_runs` tables are the home — **reference/archive only**.
- **Runtime (api/consumer/sweeper/Control Tower) must not read legacy/BQ/Sheets or the import ledger.** Enforced by the engine's read rules + `feature_coverage_registry`.
- BigQuery/Sheets parity thinking, dashboard-specific derived tables, and the old import-review UI are **scrapped from runtime** (kept only for audit during cutover, then frozen).

## 2. Build canonical Goat OS tables fresh (only what Preventive Care (PC) / Feed need for v1)

From the frozen source, materialize clean canonical rows in the **committed** tables:
- `goats` (+ the `000070` provenance/lifecycle columns): `sex`, `approx_dob` (+`dob_estimated`), `origin_type`, `entry_date`, `lifecycle_status` (active/dead/sold/missing), `park_id`/`shed_id`/`cohort_id`.
- `goat_identifiers` (RFID, old ear-tag, visual tag — committed identifier types + trust rules).
- current location/shed (`current_location_id`, `shed_id`) and `goat_location_history` **if trustworthy**.
- inventory **opening balances** as `inventory_stock` lots + an initial `inventory_stock_movements` `adjust` row, **if trustworthy**.
- operator/user mapping into `workforce_*` / `user_scope_grants` **if needed**.

Anything not needed for Preventive Care (PC) / Feed v1 (genetics depth, commerce, etc.) is **ignored for v1** — not migrated yet.

## 3. Migration quality gate (CLI/report/admin — NOT product UI)

No Import-Review product screen, **no review workflow, no review queue.** This is a one-time **migration audit report only** — a CLI/admin artifact (counts + a downloadable row list), not a product surface and not an interactive triage queue. The migration **must classify** every legacy row so Preventive Care (PC) knows what it can act on:

| Class | Meaning | Preventive Care (PC) effect |
|---|---|---|
| `accepted` | enough data to create a canonical goat | full obligation generation |
| `accepted_with_estimate` | usable, but DOB/stage/etc. estimated (`dob_estimated=true`) | generate with estimated anchors; flag |
| `blocked_for_phc` | goat exists but a key field (sex/stage/shed/DOB) is missing → vaccination can't trigger | no obligations until resolved; listed in report |
| `discarded_legacy_noise` | BQ/dashboard-only junk, not a real animal | dropped (archived, not canonical) |

The report is the cutover artifact Preventive Care (PC) reviews — counts per class, blocked-field breakdown, per-park.

## 4. Vaccination triggers from canonical state — NOT legacy import events

Do **not** replay old rows as `goat.created`. Run a **one-time Preventive Care (PC) backfill generator** (a bounded, resumable Cloud Run Job, chunked by park/date — same scale rules as the sweeper):

```
for each canonical ACTIVE goat (paged, chunked by park):
   read published vaccination protocol_versions (effective today, tenant/park scope)
   evaluate eligibility: age (approx_dob) + sex + animal_stage(shed/cohort) + lifecycle + health + overrides
   subtract already-known vaccination history (§5)
   create the MISSING obligation_instances (idempotent on the deterministic key)
   group into shed drives (SM-4 batches)
```

Idempotent: re-running the backfill creates zero duplicates (same `idempotency_key` guard as SM-1). After backfill, **all** new/future obligations come from the engine (SM-1/SM-7), never from legacy.

## 5. Historical vaccination data

**If reliable old vaccination records exist:**
- import into a `vaccination_history_imports` staging table → reconcile into `vaccination_completions`.
- mark the corresponding obligations `completed` (do not re-vaccinate).
- schedule boosters from the **actual administered date** (SM-7), not the import date.

**If history is missing or untrusted:**
- **do NOT invent completions.**
- after Preventive Care (PC) approval, create **baseline / catch-up drives** by shed/cohort (a deliberate "establish current coverage" pass), not fabricated history.

## 6. Cutover policy — no years of overdue noise

The backfill must not manufacture historical breaches:

```
for a goat's computed past-due dose:
  if due_date < CUTOVER_DATE and no reliable completion:
      → create ONE catch-up obligation / fold into a baseline drive   (status reflects "catch-up", not "overdue since 2023")
  else (due on/after cutover):
      → normal scheduled obligation
NEVER: emit one historical 'overdue' obligation per missed past dose per goat
```

Otherwise Control Tower explodes with fake historical overdue — the single worst cutover failure mode. `CUTOVER_DATE` is a config knob Preventive Care (PC) sets.

## 7. Keep / scrap

**KEEP (seed canonical from these):** raw source snapshots (audit), goat identity, RFID/ear-tags/old-tags, active/dead/sold state, park/shed mapping, shift/location history *if trustworthy*, vaccination history *if trustworthy*, inventory opening balances *if trustworthy*, operator/user mapping *if needed*.

**SCRAP from runtime:** BQ dashboard-parity thinking, legacy review UI, old Sheets/BQ row model as runtime schema, dashboard-specific derived tables, old import-review workflows.

## 8. The migration story (one line)

```
legacy BQ/Sheets
  → frozen source archive (legacy_import_*, checksummed)
  → canonical Goat OS goats/locations/operators (committed tables + 000070 cols)
  → migration quality report (accepted / accepted_with_estimate / blocked_for_phc / discarded_noise)
  → one-time Preventive Care (PC) backfill generator (canonical → missing obligations → shed drives)
  → cutover policy (catch-up/baseline, NOT historical overdue)
  → current/future vaccination + feed obligations run from Goat OS only
```

**Build gate:** this cutover plan + the [7 state machines](./state-machines.md) + the rule values are the prerequisites for migrations `000070–078` and the backfill Job. Agree all three, then SQL.
