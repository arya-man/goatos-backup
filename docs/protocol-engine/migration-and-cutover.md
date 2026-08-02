# Goat OS — Legacy → Canonical Migration & Preventive Care (PC) Cutover

**Status:** Draft v1 · **Date:** 2026-06-23
**Companion:** [obligation-engine.md](./obligation-engine.md) · [state-machines.md](./state-machines.md) · [Preventive Care (PC) TRD](../preventive-care-vaccination/TRD.md) · [Operational-kernel 5k–50k scale-envelope ADR](../decisions/operational-kernel-5k-50k-scale-envelope.md)
**Context:** local/dev/test may use verified source files or prior snapshots as
seed evidence, but the V1 base is clean slate. This doc says how to seed clean
canonical state for Preventive Care (PC) / Feed without carrying old dashboard,
BQ, Sheets, import-review, or sync-runtime tables forward.

> **Prime rule:** Legacy data **seeds** canonical state; it does **not** drive the runtime. Once canonical herd animals are clean enough, vaccination/feed rules run from Goat OS Postgres only.

**Path B target correction (2026-07-03):** canonical state is mixed-species
`herd_animals` / `animal_id`, not goat-only storage. Any references below to
`goats`, `goat_id`, `goat_identifiers`, or `goat.*` events are legacy/current
implementation names that must be migrated to the animal model before the base
is considered clean.

**Runtime-topology note (2026-07-14):** the runtime this cutover seeds into is
the 5k-to-50k operational-kernel envelope accepted in
[operational-kernel-5k-50k-scale-envelope.md](../decisions/operational-kernel-5k-50k-scale-envelope.md).
Under that envelope the vaccination execution/operations/shed and
Calendar/process-integrity screens are served from canonical indexed SQL rather
than separate projection tables, one modular kernel worker replaces the fleet of
independently scheduled Cloud Run Jobs, and the three event/history parents
(`goat_identity_events`, `audit_log`, `obligation_status_events`) are ordinary
indexed tables. The dropped projection schemas, projectors, and monthly
partitioning stay recoverable from the `kernel-split-workers-v1` tag if a
measured hotspot later needs them. This changes only the deployment shape and
how a healthy read is verified — a completed cutover means the canonical-read
APIs (for example `/vaccination/sheds`, `/vaccination/execution`,
`/vaccination/operations`) return 200 from canonical indexed SQL, not that a set
of projectors finished. Small indexed summaries that are **not** on the ADR's
removal list (for example `vaccination_eligibility_rollups` and Counts
summaries) may still be recomputed at closeout. The vaccination date/anchor
business semantics below — trusted completed history as the base anchor, future
recurrence calculated strictly after the backend business date, blank/NA/Pending
never backfilling synthetic late work, HRMS roster/manager/backup ownership, the
constraint model, and idempotency — are orthogonal to projections and are
unchanged.

---

## 1. Treat source files as evidence, not runtime truth

Keep source files outside the runtime schema:
- Local/dev/test cutover wipes and reseeds GoatOS data through the target model.
  It does not repair old rows in place.
- Raw BQ/Sheets/dashboard/procurement rows are evidence for the seed/importer
  only. Do not create `legacy_import_*`, `legacy_sync_*`, old dashboard sync,
  import-review, conflict-resolution, or freshness tables as part of the V1 base.
- **Runtime (api/consumer/sweeper/Control Tower) must not read legacy/BQ/Sheets
  or any import ledger.** Enforced by the engine's read rules +
  `feature_coverage_registry`.
- BigQuery/Sheets parity thinking, dashboard-specific derived tables, and the
  old import-review UI are **scrapped from runtime**.

## 2. Build canonical Goat OS tables fresh (only what Preventive Care (PC) / Feed need for v1)

From the frozen source, materialize clean canonical rows in the **target** tables:
- `species_catalog` and species-owned breed rows/aliases. Seed at least goat and sheep; Anantapur Sheep is sheep.
- `herd_animals`: `sex`, `dob`/`dob_confidence` plus migrated `approx_dob`, `origin_type`, `entry_date`, `lifecycle_status` (active/dead/sold/missing), `park_id`/`shed_id`/`cohort_id`, and current shed tag/stage.
- `animal_identifiers` for `animal_identifier_1` and `animal_identifier_2`.
  Both current values are required, different on the same animal, and globally
  single-use for life across current plus historical rows. Source labels are
  provenance/proof only unless the clean importer explicitly maps them into an
  Animal ID slot.
- current location/shed (`current_location_id`, `shed_id`) and `animal_location_history` **if trustworthy**.
- inventory **opening balances** as `inventory_stock` lots + an initial `inventory_stock_movements` `adjust` row, **if trustworthy**.
- operator/user mapping into `workforce_*` / `user_scope_grants` **if needed**.

Anything not needed for Preventive Care (PC) / Feed v1 (genetics depth, commerce, etc.) is **ignored for v1** — not migrated yet.

## 3. Migration quality gate (CLI/report/admin — NOT product UI)

No Import-Review product screen, **no review workflow, no review queue.** This is a one-time **migration audit report only** — a CLI/admin artifact (counts + a downloadable row list), not a product surface and not an interactive triage queue. The migration **must classify** every legacy row so Preventive Care (PC) knows what it can act on:

| Class | Meaning | Preventive Care (PC) effect |
|---|---|---|
| `accepted` | enough data to create a canonical herd animal | full obligation generation |
| `accepted_with_estimate` | usable, but DOB/stage/etc. estimated (`dob_estimated=true`) | generate with estimated anchors; flag |
| `blocked_for_pc` | animal exists but a key field (species/sex/stage/shed/DOB) is missing → vaccination can't trigger | no obligations until resolved; listed in report |
| `discarded_legacy_noise` | BQ/dashboard-only junk, not a real animal | dropped (archived, not canonical) |

The report is the cutover artifact Preventive Care (PC) reviews — counts per class, blocked-field breakdown, per-park.

## 4. Vaccination triggers from canonical state — NOT legacy import events

Do **not** replay old rows as `animal.created`. Run a **one-time Preventive Care (PC) backfill generator** (a bounded, resumable one-shot backfill command — one of the non-projection one-shot commands the kernel worker retains for backfill/repair under the 5k-to-50k envelope — chunked by park/date, same scale rules as the operational sweep stage):

```
for each canonical ACTIVE herd animal (paged, chunked by park):
   read published vaccination protocol_versions (effective today, tenant/park scope)
   evaluate eligibility and constraints:
      age/DOB or entry date, species/breed, sex, stage/shed, lifecycle,
      health/defer states (sick, ICU, quarantine), pregnancy/lactation,
      procurement warm-up, park/shed scope, rule gaps, min gaps/cross-vaccine
      spacing, inventory, operator ownership, and capacity/session caps
   subtract already-known trusted vaccination history (§5)
   create the MISSING obligation_instances (idempotent on the deterministic key)
   group into shed drives (SM-4 batches)
```

Idempotent: re-running the backfill creates zero duplicates (same `idempotency_key` guard as SM-1). After backfill, **all** new/future obligations come from the engine (SM-1/SM-7), never from legacy.

The old source date is allowed to anchor history; it is not allowed to bypass the
current engine. A trusted past date can mark that dose as done and establish the
last accepted completion date, but any next/future row still passes through the
published vaccination matrix, eligibility, defer rules, pregnancy/lactation
rules, warm-up holds, cross-vaccine gaps, inventory checks, ownership checks,
capacity/session caps, and the cutover policy below.

## 5. Historical vaccination data

This contract applies to local/dev/stg/prod real-data seeds and later migration
jobs.

**If reliable animal-level old vaccination records exist:**
- Treat each trusted source date as the **actual last-administered/done date**,
  not as a due date and not as the seed/import run date.
- Preserve the old date as visible vaccination history even before a matching
  active rule exists. Passport/Vaccination/Action Center must be able to show
  "source says last given on <date>" or a config/review blocker; the date must
  not vanish just because `vaccination.matrix` is not published yet.
- If a matching active rule exists, reconcile the evidence into the canonical
  completion path (`vaccination_completions` or the equivalent reviewed
  import-to-completion bridge), mark/suppress the corresponding obligation as
  completed, and do **not** re-vaccinate that dose.
- If no matching active rule exists yet, keep the evidence in the import/history
  ledger with source refs and review status. Do **not** fabricate a rule,
  obligation, or completion. Surface a configuration gap/review item instead.
  When a rule is later published for that vaccine/dose/scope, the next
  generation/reconciliation run must use the preserved past date as the last
  accepted completion and schedule future work from it.
- Schedule boosters/repeats from the **actual administered date** (SM-7), not the
  import date.
- Apply all current constraints before creating open work: active matrix scope,
  species/breed/sex/stage, lifecycle/death/sold state, current park/shed,
  health/defer states (`sick`, `ICU`, `quarantine`), pregnancy/lactation holds,
  procurement warm-up, min-gap and cross-vaccine spacing, latest safe date,
  inventory/FEFO availability, assigned owner/backup, daily capacity, max buffer
  days, and session split policy.

**If history is missing or untrusted:**
- **do NOT invent completions.**
- automatically create **baseline / catch-up obligations** in the normal adult
  drive for that vaccine, grouped by whole physical shed. No separate manual
  campaign or Preventive Care approval is required for routine adult coverage;
  the animal still receives an explicit initial/catch-up dose instruction, not
  fabricated history.

## 6. Cutover policy — no years of overdue noise

The backfill must not manufacture historical breaches:

```
for an animal's computed past-due dose:
  if due_date < CUTOVER_DATE and no reliable completion:
      → create ONE catch-up obligation / fold into a baseline drive   (status reflects "catch-up", not "overdue since 2023")
  else (due on/after cutover):
      → normal scheduled obligation
NEVER: emit one historical 'overdue' obligation per missed past dose per animal
```

Otherwise Control Tower explodes with fake historical overdue — the single worst cutover failure mode. `CUTOVER_DATE` is a config knob Preventive Care (PC) sets.

## 7. Keep / scrap

**KEEP as source evidence only:** animal identity, species/breed, active/dead/sold
state, park/shed mapping, shift/location history *if trustworthy*, vaccination
history *if trustworthy*, inventory opening balances *if trustworthy*, and
operator/user mapping *if needed*. The clean importer maps any source identifier
labels into Animal ID 1/2 or rejects the row; source identifier names do not
become runtime API/DB names.

**SCRAP from runtime:** BQ dashboard-parity thinking, legacy review UI, old Sheets/BQ row model as runtime schema, dashboard-specific derived tables, old import-review workflows.

## 8. The migration story (one line)

```
verified source evidence
  → clean seed/import validation
  → canonical Goat OS herd_animals/locations/operators
  → migration quality report (accepted / accepted_with_estimate / blocked_for_pc / discarded_noise)
  → one-time Preventive Care (PC) backfill generator (canonical → missing obligations → shed drives)
  → cutover policy (catch-up/baseline, NOT historical overdue)
  → current/future vaccination + feed obligations run from Goat OS only
```

**Build gate:** this cutover plan + the [7 state machines](./state-machines.md) + the rule values are the prerequisites for the Path B herd-animal base migrations and the backfill Job. Agree all three, then SQL.
