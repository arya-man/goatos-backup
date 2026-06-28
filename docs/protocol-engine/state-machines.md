# Goat OS — Obligation Engine State Machines (spec before SQL)

**Status:** Draft v1 · **Date:** 2026-06-23
**Companion:** [obligation-engine.md](./obligation-engine.md) · [PHC TRD](../phc-vaccination/TRD.md) · [Feed TRD](../feed-direction/TRD.md)
**Purpose:** the behavioural contract that must be agreed **before** writing migrations/code. Each machine lists trigger, guards, transitions, invariants, idempotency, and failure handling. Tables/columns are defined in the engine doc; this doc defines *behaviour*.

---

## 0. Conventions (apply to every machine)

- **Source of truth = Postgres.** Cloud Tasks/Pub-Sub carry near-term work only; lost messages are re-derived by re-scanning `obligation_instances`.
- **At-least-once delivery → idempotent handlers.** Every handler keys on a deterministic id and is a no-op on replay.
- **Cascade = app code via transactional outbox**, never DB triggers for cross-aggregate effects. One DB txn writes the domain row(s) + `outbox_messages` + (where relevant) `obligation_status_events` + stock movement.
- **Statuses** — obligation: `scheduled → due → in_progress → completed | missed | waived | canceled | superseded`. batch: `planned → in_progress → completed | superseded | canceled`. stock movement: `reserve | release | consume | adjust | transfer | expire`.
- **Every transition** appends `obligation_status_events` and (on cross-aggregate effect) an `outbox_messages` row.

---

## SM-1 · Schedule generation
**Trigger:** `goat.created` (birth/procurement) or `goat.stage_changed` (age/weight reclass) — consumed from Pub/Sub.

**Inputs:** the goat; the set of **published** `protocol_versions` whose `[effective_from, effective_to)` covers `now`, matching tenant + (scope: tenant-default or the goat's park) + category.

**Steps:**
1. Resolve **multi-factor eligibility** per the rule's `eligibility_json` = `age + animal_stage(shed/cohort) + sex + breed + lifecycle + health + reproductive(exclude pregnant/lactating)`. Phase 0 does not expose per-goat individual override generation; PHC-approved catch-up uses the manual campaign path. If a `defer_states` condition holds (ICU/quarantine/sick), **create/update a *visible* deferred obligation (or emit a defer event) and re-evaluate on recovery — never silently skip** (and don't mark missed): the obligation lands in a `deferred` state with a reason, so **Control Tower / Protocol Adherence can explain why the dose did not fire**. Skip rules the goat is ineligible for (true ineligibility ≠ defer — ineligible generates nothing; defer generates a tracked, explained obligation).
2. **Iterate the `schedule[]` dose rows** (a rule is multi-dose, not one trigger). For each dose, compute `due_at` by `trigger_type`:
   - `birth_age`: `approx_dob + offset_days`
   - `post_arrival`: `entry_date + offset_days`
   - `calendar`: fixed/cron date
   - `after_previous_completion`: **deferred to SM-7** (generated from the prior dose's actual `administered_at` + `offset_days`, respecting `min_gap_days`).
   - `manual_campaign`: generated only when a campaign trigger fires.
   - apply `repeat` (`yearly`/`every_n_days`; age-window repeats are rejected until generator support lands) to spawn the lifecycle-phase recurrences.
3. **History-based next-due (catch-up):** if the goat has prior accepted history (`vaccination_completions` / imported), compute next due from the **last accepted completion**, not blindly from DOB. For an already-passed due with no completion, apply `missed_dose_policy`: `immediate` (catch-up now) · `next_cycle` (skip to next) · `phc_approval` (hold for sign-off) · `defer` (defer + reason) — see migration-and-cutover §6 for the cutover variant (no historical-overdue flood).
4. **Upsert** `obligation_instances`, deterministic `idempotency_key = hash(tenant·protocol_version_id·rule_id·target_type·target_id·due_at·sequence)`; status `scheduled`.

**Invariants:** never two active obligations for `(tenant, protocol_version, rule_id, target, due_at)` (DB guard, `NULLS NOT DISTINCT`). Idempotent — re-run produces zero new rows.
**Failure/retry:** consumer failure → Pub/Sub redelivery → upsert no-ops. Poison → DLQ.
**Edge:** procured adult unknown DOB → `dob_estimated=true`, `post_arrival` off `entry_date`. Backfilled import → history-based next-due (step 3), idempotent path.

---

## SM-2 · Shift recompute
**Trigger:** `goat.shifted` (shed change), emitted when `goats.shed_id`/`current_location_id` changes (and the committed `goat_location_history` row is written).

**Steps (per pending obligation of the goat, status in `scheduled`/`due`):**
1. Determine the goat's **new** `animal_stage` (from destination `shed_profiles.animal_stage_id` / cohort).
2. Re-evaluate eligibility under the new stage for the **same vaccine/protocol**:
   - **Still eligible, dest batch open (same protocol_version, not completed):** re-point `obligation_instances.batch_id`/`scope_id` to the destination shed's batch.
   - **Still eligible, dest batch already completed:** hold for PHC-approved catch-up/manual campaign review; Phase 0 does not auto-spawn standalone per-goat tasks.
   - **No longer eligible** (stage no longer matches the rule): `cancel` the obligation (status `canceled`, reason `ineligible_after_shift`); generate any newly-eligible obligations for the new stage (SM-1 path).
3. **Never** blind-repoint across vaccines — re-point only within the same protocol/vaccine.

**Invariants:** a goat's obligation can never reference a batch for a different protocol than its rule. Decrement old batch `estimated_targets`, increment new.
**Idempotency:** keyed on `(goat, shift_event_id)`; re-delivery recomputes to the same end state.
**Edge:** rapid double-shift → process in `occurred_at` order; final state reflects the latest shed.

---

## SM-3 · Death / sale cancel
**Trigger:** `goat.exited` — emitted in the **same txn** that sets `goats.lifecycle_status ∈ {dead, sold, missing}` + `exited_at` + `exit_reason`.

**Steps (one txn):**
1. `UPDATE obligation_instances SET status='canceled', reason=exit_reason WHERE tenant_id=? AND target_type='goat' AND target_id=<goat_id> AND status IN ('scheduled','due','in_progress')`. (Obligations key on `target_type/target_id`, not a `goat_id` column.)
2. For each affected open batch: decrement `estimated_targets` (and `planned_quantity` if the goat's dose was counted); if the goat's dose had reserved stock, emit a `release` movement.
3. Append `obligation_status_events('canceled')` per obligation; emit `outbox` (`goat.obligations_canceled`).

**Invariants:** a dead/sold goat has **zero** active obligations → never appears overdue, never inflates coverage or stock estimates.
**Idempotency:** re-delivery finds no `scheduled/due/in_progress` rows → no-op.
**Edge:** death **during** an in-progress drive → the goat's in_progress obligation is canceled; the batch's reserved stock is reconciled at close (SM-4/SM-5), not double-released.

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

**Supersede:** if regenerated (e.g. feed 2 PM, SM-6) → old batch `→ superseded`, its still-`due` obligations move to the new batch.
**Invariants:** exactly one open batch per `(scope, protocol_version, window)`; stock reserve/consume happen at batch boundaries, **never per goat** in a group drive.
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

**Mode split (no contradiction):** group drive = `reserve` at SM-4 create, `consume`+`release` at SM-4 close. PHC-approved catch-up creates canonical obligations/batches before execution; standalone per-goat individual override stock mode is not exposed in Phase 0.
**Idempotency:** every movement carries `idempotency_key` (UNIQUE per tenant); replay no-ops.
**Invariants:** balance is always `= Σ movements`; never negative; expired lots never consumed.
**Edge:** stock-out at reserve → batch flagged shortfall (anomaly surfaced), partial reserve allowed only if policy permits; never a negative balance.

---

## SM-6 · Feed generation (direction publish → 2 PM recompute → packing)
**Feed has three distinct phases; do NOT reserve stock at generation.** Generation only computes directions; stock is reserved/consumed at the **packing** phase (SM-4 execute).

**Phase 1 — direction generation (Cloud Scheduler; day-before or ~morning publish):**
1. Recompute `supply_planning` from published `feed_pattern × feed_pattern_feeds × live headcounts` per shed (headcount from `goats` by `shed_id`/stage). `supply_planning` is **replaced, not updated** (`generated_at` is truth).
2. Write `feed_directions` **version 1** (status `active`), pushed through `feed_session_templates`.
3. Per shed×session → an `obligation_batches` row (status `planned`). **No stock reservation yet.**

**Phase 2 — 2 PM recompute (Cloud Scheduler 14:00 OR `goat.shifted` location_event):**
1. Regenerate `supply_planning` from current headcounts (post-shiftings).
2. Write `feed_directions` **version 2** (`active`); set affected v1 directions `→ superseded`.
3. **Reconcile batches (SM-4 supersede):** affected sheds' v1 batches `→ superseded`; new v2 batches for changed sheds. (Reservations only exist if packing already started — release/re-reserve the delta then.) Unchanged sheds keep v1 (no churn).

**Phase 3 — packing (the SOP/packing task starts, ~3 PM placement):**
1. Batch `planned→in_progress`; **reserve** feed stock for the batch's `planned_quantity` (SM-5) — first lock here.
2. On packing accepted/verified: **consume** packed qty + **release** remainder (SM-5); record `feed_packing`.
3. Consumption recording (feeding sessions) → `feed_consumption`, variance = consumed vs packed.

**Invariants:** exactly one `active` direction per `(date, shed, session, breed, age, feed)`; v2 reflects current headcount; **stock is only locked at packing, never at generation**; reservations match the active direction.
**Idempotency:** generation keyed on `(planning_date, shed, session, feed, direction_version)`.
**Edge:** birth/death intra-day → folded into 2 PM (or on-demand regenerate); packing already done on a superseded direction → counts toward consumption, variance flagged.

---

## SM-7 · Booster generation
**Trigger:** `vaccination.completed` (a primary dose's completion) — matched to a `protocol_triggers` row with `trigger_type='upstream_completion'` on the same protocol version.

**Steps:**
1. Read the booster `protocol_rules` (config_type=booster) for the completed primary's `(vaccine, animal_type, sex, stage)`.
2. Compute booster `due_at` = **actual `administered_at` + `booster_interval_days`** (from the *real* administration time, not the planned date).
3. Upsert a new `obligation_instances` (status `scheduled`) for the same goat, `sequence = primary.sequence + 1`, `prev` linkage via the deterministic idempotency_key chain.

**Invariants:** a booster exists only after its primary is *completed* (never pre-generated in SM-1). One booster per primary completion (idempotent on `(tenant, protocol_version, rule_id, target, due_at, sequence)`).
**Idempotency:** keyed on the completion event id; replay no-ops.
**Edge:** primary **missed/canceled** → no `vaccination.completed` → no booster (correct). Interrupt policy (e.g. primary re-done late) → booster recomputed from the new `administered_at`.

---

## Build gate
Migrations `000070–075` (PHC) and `000076–078` (Feed) implement the *tables*; **these seven machines are the behaviour those tables must support.** Agree this doc first; then SQL, then handlers. Each machine maps to one Cloud Run path: SM-1/2/3/7 = `consumer` (Pub/Sub push — `goat.created`/`shifted`/`exited`/`vaccination.completed`); SM-4 create + SM-6 generation = `sweeper`/feed-gen Jobs (Cloud Scheduler); SM-4 close + SM-5 + SM-6 packing = `api` (SOP submission/verification).
