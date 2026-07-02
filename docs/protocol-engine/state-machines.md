# Goat OS — Obligation Engine State Machines (spec before SQL)

**Status:** Draft v1 · **Date:** 2026-06-23
**Companion:** [obligation-engine.md](./obligation-engine.md) · [Preventive Care (PC) TRD](../preventive-care-vaccination/TRD.md) · [Feed TRD](../feed-direction/TRD.md)
**Purpose:** the behavioural contract that must be agreed **before** writing migrations/code. Each machine lists trigger, guards, transitions, invariants, idempotency, and failure handling. Tables/columns are defined in the engine doc; this doc defines *behaviour*.

**Path B target correction (2026-07-03):** schedule/cancel/shift machines target
mixed-species `herd_animals` / `animal_id`, not goat-only storage. Replace
`goat.created`, `goat.shifted`, `goat.exited`, `goat.stage_changed`, and
`target_type='goat'` with `animal.created`, `animal.shifted`, `animal.exited`,
`animal.stage_changed`, and `target_type='herd_animal'` in the base migration.
Existing goat names below describe the pre-correction implementation only.

---

## 0. Conventions (apply to every machine)

- **Source of truth = Postgres.** Cloud Tasks/Pub-Sub carry near-term work only; lost messages are re-derived by re-scanning `obligation_instances`.
- **At-least-once delivery → idempotent handlers.** Every handler keys on a deterministic id and is a no-op on replay.
- **Cascade = app code via transactional outbox**, never DB triggers for cross-aggregate effects. One DB txn writes the domain row(s) + `outbox_messages` + (where relevant) `obligation_status_events` + stock movement.
- **Statuses** — obligation: `scheduled → due → in_progress → completed | missed | waived | canceled | superseded`. batch: `planned → in_progress → completed | superseded | canceled`. stock movement: `reserve | release | consume | adjust | transfer | expire`.
- **Every transition** appends `obligation_status_events` and (on cross-aggregate effect) an `outbox_messages` row.

---

## SM-1 · Schedule generation
**Trigger:** `animal.created` (birth/procurement) or `animal.stage_changed` (age/weight reclass) — consumed from Pub/Sub.

**Inputs:** the herd animal; the set of **published** `protocol_versions` whose `[effective_from, effective_to)` covers `now`, matching tenant + (scope: tenant-default or the animal's park) + category.

**Steps:**
1. Resolve **multi-factor eligibility** per the rule's `eligibility_json` = `species + breed + age + animal_stage(shed/cohort) + sex + lifecycle + health + reproductive(exclude pregnant/lactating)`. Phase 0 does not expose per-animal individual override generation; Preventive Care approved catch-up uses the manual campaign path. If a `defer_states` condition holds (ICU/quarantine/sick), **create/update a *visible* deferred obligation (or emit a defer event) and re-evaluate on recovery — never silently skip** (and don't mark missed): the obligation lands in a `deferred` state with a reason, so **Control Tower / Protocol Adherence can explain why the dose did not fire**. Skip rules the animal is ineligible for (true ineligibility ≠ defer — ineligible generates nothing; defer generates a tracked, explained obligation).
2. **Iterate the `schedule[]` dose rows** (a rule is multi-dose, not one trigger). For each dose, compute `due_at` by `trigger_type`:
   - `birth_age`: `approx_dob + offset_days`
   - `post_arrival`: `entry_date + offset_days`
   - `calendar`: fixed/cron date
   - `after_previous_completion`: **deferred to SM-7** (generated from the prior dose's actual `administered_at` + `offset_days`, respecting `min_gap_days`).
   - `manual_campaign`: generated only when a campaign trigger fires.
   - apply `repeat` (`yearly`/`every_n_days`; age-window repeats are rejected until generator support lands) to spawn the lifecycle-phase recurrences.
3. **History-based next-due (catch-up):** if the animal has prior accepted history (`vaccination_completions` / imported), compute next due from the **last accepted completion**, not blindly from DOB. For an already-passed due with no completion, apply `missed_dose_policy`: `immediate` (catch-up now) · `next_cycle` (skip to next) · `phc_approval` (hold for sign-off) · `defer` (defer + reason) — see migration-and-cutover §6 for the cutover variant (no historical-overdue flood).
4. **Upsert** `obligation_instances`, deterministic `idempotency_key = hash(tenant·protocol_version_id·rule_id·target_type·target_id·due_at·sequence)`; status `scheduled`.

**Invariants:** never two active obligations for `(tenant, protocol_version, rule_id, target, due_at)` (DB guard, `NULLS NOT DISTINCT`). Idempotent — re-run produces zero new rows.
**Failure/retry:** consumer failure → Pub/Sub redelivery → upsert no-ops. Poison → DLQ.
**Edge:** procured adult unknown DOB → `dob_estimated=true`, `post_arrival` off `entry_date`. Backfilled import → history-based next-due (step 3), idempotent path.

**Published version-change policy:** a rule/SOP change publishes a new immutable
`protocol_version`. It never mutates accepted completions or the audit/proof
history tied to an older version. Future generation uses the effective version
for the animal/scope/as-of date. Already materialized open work follows this
policy:

- `scheduled`, `due`, and `deferred` rows may be canceled, superseded, or
  regenerated only by an explicit repair command that writes
  `obligation_status_events` and preserves the old row's version pointer.
- `in_progress` rows stay tied to their execution batch and SOP task; proof
  acceptance, rejection/rework, missed-window handling, or death/sale cancel is
  the only way they leave that state.
- `completed`, `waived`, `canceled`, `superseded`, and `missed` rows are
  immutable history for coverage/as-of reads. A later policy correction creates
  new work or a rework/correction record; it does not rewrite the closed row.

---

## SM-2 · Shift recompute
**Trigger:** `animal.shifted` (shed change), emitted when `herd_animals.shed_id`/`current_location_id` changes (and the `animal_location_history` row is written).

**Steps (per pending obligation of the animal, status in `scheduled`/`due`/`deferred`):**
1. Determine the animal's **new** `animal_stage` (from destination `shed_profiles.animal_stage_id` / cohort).
2. Re-evaluate eligibility under the new stage for the **same vaccine/protocol**:
   - **Still eligible, dest batch open (same protocol_version, not completed):** re-point `obligation_instances.batch_id`/`scope_id` to the destination shed's batch.
   - **Still eligible, dest batch already completed:** hold for Preventive Care approved catch-up/manual campaign review; Phase 0 does not auto-spawn standalone per-animal tasks.
   - **No longer eligible** (stage no longer matches the rule): `cancel` the obligation (status `canceled`, reason `ineligible_after_shift`); generate any newly-eligible obligations for the new stage (SM-1 path).
3. **Never** blind-repoint across vaccines — re-point only within the same protocol/vaccine.

**In-progress/completed shift edge:** SM-2 is not a post-hoc history rewrite.
Open, unbatched rows and planned-batch rows can move because no field execution
has begun. `in_progress` rows remain on the original execution batch/SOP task;
if the dose is not accepted in time, the missed/rework path keeps it visible
instead of silently moving it to the destination shed. `completed` rows remain
accepted historical evidence at the original scope. Planned-batch repairs may
decrement old batch `estimated_targets` and flag `shift_repair` stock release
work; in-progress/completed batches are closed by proof, rework, missed, or
explicit cancel/reversal policy only.

**Invariants:** an animal's obligation can never reference a batch for a different protocol than its rule. Decrement old planned batch `estimated_targets`, increment new.
**Idempotency:** keyed on `(animal, shift_event_id)`; re-delivery recomputes to the same end state.
**Edge:** rapid double-shift → process in `occurred_at` order; final state reflects the latest shed.

---

## SM-3 · Death / sale cancel
**Trigger:** `animal.exited` — emitted in the **same txn** that sets `herd_animals.lifecycle_status` to an exited state + `exited_at` + `exit_reason`.

**Steps (one txn):**
1. `UPDATE obligation_instances SET status='canceled', reason=exit_reason WHERE tenant_id=? AND target_type='herd_animal' AND target_id=<animal_id> AND status IN ('scheduled','due','in_progress')`.
2. For each affected open batch: decrement `estimated_targets` (and `planned_quantity` if the animal's dose was counted); if the animal's dose had reserved stock, emit a `release` movement.
3. Append `obligation_status_events('canceled')` per obligation; emit `outbox` (`animal.obligations_canceled`).

**Invariants:** a dead/sold animal has **zero** active obligations → never appears overdue, never inflates coverage or stock estimates.
**Idempotency:** re-delivery finds no `scheduled/due/in_progress` rows → no-op.
**Edge:** death **during** an in-progress drive → the animal's in_progress obligation is canceled; the batch's reserved stock is reconciled at close (SM-4/SM-5), not double-released.

---

## SM-4 · Batch lifecycle (the drive / work unit)
**Create (sweeper, on Cloud Scheduler tick):**
1. Scan due window: `obligation_instances WHERE status IN ('scheduled','due') AND due_at <= now()+lead`, grouped by `(scope, protocol_version, window)`.
2. Flip matched obligations `scheduled→due`.
3. Create/attach an `obligation_batches` row (status `planned`); set each obligation's `batch_id`.
4. Spawn **one** `sop_task` from the protocol version's `sop_version_id`; set `batch.sop_task_id`. Compute `planned_quantity`/`quantity_unit` and `estimated_targets`.
5. **Reserve stock — module-policy timing** (SM-5 reserve): **Vaccination** reserves `planned_quantity` against the FEFO lot at batch create (drive is imminent), setting `primary_inventory_lot_id`. **Feed** does NOT reserve at create — direction generation ≠ packing; feed reserves only when the **packing task** starts (SM-6), so stock isn't locked hours early.

**Execute:** `batch planned→in_progress` when the worker opens the task; obligations `due→in_progress`. (Feed: this is the packing task → reserve happens now.)

**Close (on SOP submission accepted / verification):**
1. Per accepted `sop_submission_item` → write module completion row (links `obligation_id`, `batch_id`), obligation `in_progress→completed`.
2. Items not done in-window → `missed`; blocked (ICU/quarantine/stock-out) → `waived` + reason.
3. **Reconcile stock** (SM-5): `consume` actual used + `release` remainder of the reservation.
4. `batch in_progress→completed`; roll up `used_quantity`, counts.

**Supersede:** if regenerated or corrected (e.g. Feed Direction cutoff Diff, SM-6) → old batch `→ superseded`, its still-`due` obligations move to the new batch.
**Invariants:** exactly one open batch per `(scope, protocol_version, window)`; stock reserve/consume happen at batch boundaries, **never per animal** in a group drive.
**Idempotency:** batch creation keyed on `(scope, protocol_version, planned_date, session)`; SOP submission idempotent via committed `sop_submissions UNIQUE(tenant, idempotency_key)`.

---

## SM-5 · Stock reserve / consume / release (ledger)
All writes go to `inventory_stock_movements` (append-only); `inventory_stock` balances update in the **same txn**. Quantities are `numeric` + `quantity_unit`.

| Event | Movement | Balance effect | Guard |
|---|---|---|---|
| Batch reserve | `reserve(qty)` | `reserved += qty` | FEFO lot, `expiry_date >= window_end`; `in_stock − reserved >= qty` |
| Catch-up / drive consume | `consume(qty)` | `in_stock −= qty`, `consumed += qty`, `reserved −= reserved_portion` | `CHECK(in_stock>=0)`; lot not expired |
| Drive close remainder | `release(qty)` | `reserved −= qty` | qty ≤ outstanding reservation |
| Manual correction | `adjust(±qty)` | `in_stock += qty` | actor has stock-adjust capability; reason required |
| Lot expiry | `expire(qty)` | `in_stock −= qty` | `now > expiry_date` |

**Mode split (no contradiction):** vaccination group drives `reserve` at SM-4 batch create and `consume`+`release` at SM-4 close. Feed direction generation does not reserve stock; feed reserves only when the packing batch/task starts (SM-6 Phase 3), then consumes/releases on accepted packing proof. Preventive Care approved catch-up creates canonical obligations/batches before execution; standalone per-animal individual override stock mode is not exposed in Phase 0.
**Idempotency:** every movement carries `idempotency_key` (UNIQUE per tenant); replay no-ops.
**Invariants:** balance is always `= Σ movements`; never negative; expired lots never consumed.
**Edge:** stock-out at reserve → batch flagged shortfall (anomaly surfaced), partial reserve allowed only if policy permits; never a negative balance.

---

## SM-6 · Feed generation (full direction → Diff → packing)
**Feed has distinct generation and execution stages; do NOT reserve stock at
generation.** Generation only computes next-day direction work from the
published feed protocol and the horizon-aware Counts/Shifting input contract.
The current repo does not yet have the aggregate base-count + horizon-aware
shifting projection Feed requires, so Feed generation is blocked until
Counts/Shifting provides it or an approved per-animal derivation path exists.
Stock is reserved/consumed at the **packing** phase (SM-4 execute).

**Phase 1 — full direction generation (Cloud Scheduler; recommended default Day N 09:00):**
1. Read the published `protocol_versions.rule_dsl` for `category='feed_direction'`.
2. Consume the Counts/Shifting projection for tomorrow's shed + breed +
   stage/tag counts. Projection horizon is exactly one day. For this forward
   projection, include authorized/directed future-effective shiftings effective
   on Day N+1; physical proof gates tomorrow's realization/reconciliation, not
   initial projection. Shifting events must carry structured cohort/stage impact
   and must be applied idempotently by event id; unresolved impact fails closed
   into process-exception work instead of changing counts.
3. Apply source-backed feed eligibility exclusions and ration transforms.
4. Write a generation run + source/planning snapshot rows, then create
   shed/session/feed obligations for Day N+1. **No stock reservation yet.**

<a id="sm-6-feed-diff-cancel-helper"></a>

**Phase 2 — Diff generation (cutoff window; recommended default Day N 13:30-13:45):**
1. Collect eligible target-date shiftings raised after the full direction and
   before or at the cutoff under the horizon-aware projection policy:
   authorized/directed future-effective moves for projection, applied/completed
   moves for realized corrections.
2. Generate canonical restatement rows for affected shed/session/feed targets
   only. Source-facing output may present the net correction, but runtime
   authority is the affected-row restatement plus supersession state.
3. **Reconcile open work explicitly:** affected stale obligations/batches are canceled or superseded before replacement/additional obligations are created. Idempotency-key dedupe is not sufficient by itself; model the feed-specific helper after `CancelOpenVaccinationObligationsForGoatExceptVersions` so stale open work cannot survive beside the Diff.
4. High-priority post-cutoff additions use the manual 2x-ration bridge protocol and are logged as bridge events; do not implement the superseded 07:30 next-morning Diff design.

<a id="sm-6-feed-inventory-app-anchors"></a>

**Phase 3 — packing (the SOP/packing task starts, recommended default Day N 15:00 staging):**
1. Batch `planned→in_progress`; **reserve** feed stock for the batch's planned quantity via `ReserveForBatch` (SM-5) — first lock here.
2. On packing accepted/verified: **consume** packed qty via `ConsumeForBatch` + **release** remainder (SM-5); record the packing stage completion and any projection rows.
3. Packing shortfall or rejected packing proof reopens/reissues packing work and does not consume inventory.
4. Transport, consumption, and wastage are separate proof-gated execution stages or typed stage records; they do not collapse into one Boolean processed flag.
5. Transport uses a reviewed direction-shed to transport-shed consolidation map when source operations stage grouped transport work.
6. Consumption recording (feeding sessions) records consumed quantity, wasted quantity, variance, and proof through the chosen stage model; source-backed discrepancy/wastage thresholds create exception or rework state.
7. Refresh Feed Direction read-model buckets for generation blocked, packing
   due/shortfall, proof missing, transport pending/rejected, consumption
   incomplete, wastage exception, bridge exception, stock-out, and rework so
   top-level command lenses can answer who owns the next action without scanning
   raw rows.

**Invariants:** exactly one open active instruction per `(target_date, shed, session, breed, shed_tag_or_stage, feed)` after full/Diff reconciliation; **stock is only locked at packing, never at generation**; reservations match the active instruction; unresolved cohort/stage impact never changes feed counts.
**Idempotency:** generation is keyed by tenant, target date, shed, session, feed, run kind, and source/version hash; shifting ledger application is keyed by shifting event id/logical event key.
**Edge:** birth/death or shifting changes after the cutoff are absorbed by the next full direction unless the high-priority addition bridge applies. Packing already done on a superseded/canceled instruction counts toward consumption and is surfaced as variance.

---

## SM-7 · Booster generation
**Trigger:** `vaccination.completed` (a primary dose's completion) — matched to a `protocol_triggers` row with `trigger_type='upstream_completion'` on the same protocol version.

**Steps:**
1. Read the booster `protocol_rules` (config_type=booster) for the completed primary's `(vaccine, animal_type, sex, stage)`.
2. Compute booster `due_at` = **actual `administered_at` + `booster_interval_days`** (from the *real* administration time, not the planned date).
3. Upsert a new `obligation_instances` (status `scheduled`) for the same animal, `sequence = primary.sequence + 1`, `prev` linkage via the deterministic idempotency_key chain.

**Invariants:** a booster exists only after its primary is *completed* (never pre-generated in SM-1). One booster per primary completion (idempotent on `(tenant, protocol_version, rule_id, target, due_at, sequence)`).
**Idempotency:** keyed on the completion event id; replay no-ops.
**Edge:** primary **missed/canceled** → no `vaccination.completed` → no booster (correct). Interrupt policy (e.g. primary re-done late) → booster recomputed from the new `administered_at`.

---

## Build gate
Migrations `000070+` implemented the generic protocol/obligation/inventory kernel, and `000079` intentionally made Feed Direction reuse that kernel instead of adding a parallel typed feed execution stack. **These machines are the behavior the committed generic tables and any later module-specific run/projection tables must support.** Agree this doc first; then SQL, then handlers. Each machine maps to one Cloud Run path: SM-1/2/3/7 = `consumer` (Pub/Sub push — `animal.created`/`shifted`/`exited`/`vaccination.completed`); SM-4 create + SM-6 generation = `sweeper`/feed-gen Jobs (Cloud Scheduler); SM-4 close + SM-5 + SM-6 packing = `api` (SOP submission/verification).
