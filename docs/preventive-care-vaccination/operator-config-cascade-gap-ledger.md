# Operator-Config Cascade — Gap Ledger (auto adversarial review, 2026-07-23)

Raw findings: 65 · kept after judging: 42

# Operator-Config Cascade — Prioritized Gap Ledger

**Design under review:** `docs/preventive-care-vaccination/vaccination-drive-operator-scheduling.PROPOSAL.md`
**Scope:** configurable active-operators/day (N), single common cap, date-range leave (+ never-two-on-same-date), recurring week-off, CEO default-operator + fallback chain — and the requirement that every such change correctly clears/reschedules obligations + `vaccination_drive_assignments` with no lost/duplicated obligations, no clinical regression, no scale regression.

**Headline finding:** The kernel is **generation-and-forward-batching only**. `sweepVersion` (`backend/internal/obligation/app/sweeper.go:731`) operates exclusively on **UNBATCHED** due obligations. Once an obligation is batched (`obligation_batches` + `vaccination_drive_assignments`), *no code path* releases or re-evaluates it on any capacity/operator input change. There is **no `capacity.changed`/`roster.changed`/`leave.changed`/`N.changed` domain event or subscriber**. The entire proposal §6 cascade is unbuilt. Severities are the judged/corrected values; because most of these are gaps against a not-yet-ratified design, the repo P0 bar (live data-loss/clinical-safety) is generally not met — the dominant class is P1 "blocking build-gate for the feature."

Legend: **[BUILD]** = build-new · **[FIX]** = fix-existing

---

## (A) Cascade correctness — obligations/drives not cleared or rescheduled

| id | sev | title | file |
|---|---|---|---|
| OPS-CASCADE-1 | P1 | Cap/leave/week-off/default changes never rescope existing scheduled drives | `sweeper.go` + `roster_service.go` + `domain-event-registry.json` |
| CAP-01 | P1 | Common-cap change never re-evaluates already-committed assignments | `sweeper.go:731`; `protocol/app/publish.go:150` |
| N-02 | P1 | No re-plan when N changes — parallel↔single orphans/strands assignments | `sweeper.go:731` |
| DEFOP-3 | P1 | Sweeper only plans UNBATCHED; committed drives keep stale operator forever | `sweeper.go:273` |
| LEAVE-01 | P1 | No trigger on leave-added — batched drive keeps on-leave operator | `roster_service.go:389,439` |
| LEAVE-02 | P1 | `obligation_batches.conducted_by` never revisited after leave | `sweeper.go:273`; `processintegrity/repository.go:560` |
| LEAVE-RM-02 | P1 | Leave add **or** remove never re-batches committed assignments | `sweeper.go:731`; `visit_shot_lock.go:488` |
| WKOFF-1 | P1 | No trigger re-plans existing drives when week-off added/changed | `roster_service.go:160/235`; `sweeper.go:274` |
| DEFOP-4 | P1 | Operator-grain upsert makes any re-point insert a duplicate, not update | `visit_shot_lock.go:81/167`; mig `000022` |
| OPS-CASCADE-2 | P1 | Effective cap = `MAX(vaccination_daily_animal_cap)` across positions — exceeds the single common cap | `visit_shot_lock.go:505`; `sweeper.go:351` |
| N-03 | P2 | Naive re-plan → duplicate/orphan operator rows (shrink leaks) | mig `000022`; `visit_shot_lock.go:81` |
| DEFOP-2 | P2 | No config-change event; nothing reacts to a default swap | `shift.go` (only goat-shift subscriber) |
| OPS-CASCADE-3 | P2 | No BE enforcement of "never two operators on leave same date" | `roster_service.go:389`; `roster_repository.go:970` |
| OPS-CASCADE-4 | P2 | Availability `valid_from <= date + 1 day` — operator schedulable one business day early | `visit_shot_lock.go:515/528` (vs leave predicate `:540`) |
| WKOFF-3 | P2 | Drive-date scoring ignores operator week-off — drive lands on a day the sole operator is off | `drive_planner.go` (score) + `sweeper.go:277` |
| WKOFF-4 | P2 | Empty candidate set silently batches unassigned + falls back to base (un-scaled) cap | `sweeper.go:281/310/329/345` |

**A — root cause / failure / fix (representative)**

- **OPS-CASCADE-1 / CAP-01 / N-02 / LEAVE-RM-02 / WKOFF-1** (all the same structural hole): roster/protocol writers mutate `workforce_absences` / `workforce_positions` / `vaccination_capacity_config` but emit **no domain event**; the sweeper is unbatched-only and `assignVaccinationOperator` early-returns when `ConductedBy != nil`. **Failure:** CEO lowers cap 200→100 / switches N 3→1 / grants or cancels leave / sets week-off — future batched drives keep the stale plan, stale operator, and stale capacity; N=1↔3 leaves the park under- or over-planned. **Fix [BUILD]:** one scoped domain event per config mutation (`capacity.changed`/`roster.changed`/`leave.changed`/`weekoff.changed`) registered in `context/architecture/domain-event-registry.json`, whose **idempotent, advisory-locked consumer** releases only the *affected* future, still-actionable batches/assignments back to unbatched (preserving in-progress/completed history) and lets the sweeper re-plan through the existing gated path — modeled on the existing `goat.location.changed → ReScopeOpenForGoatShift` pattern (`shift.go`).
- **DEFOP-3 / LEAVE-01 / LEAVE-02 / WKOFF-1** are the read-side-of-the-same-hole; **[BUILD]** the same consumer must also null/re-derive `conducted_by` on affected batches.
- **DEFOP-4 / N-03** — mig `000022` unique key includes `COALESCE(operator_id, zero-uuid)`; the sole insert primitive `ON CONFLICT`s on the operator-inclusive key, so re-pointing to a new operator **inserts a second row** and shrinking N **leaves orphan rows**; read models `STRING_AGG(DISTINCT operator)` then show both, and `animal_count` double-counts. **Fix [BUILD]:** re-plan must be a **set-reconciling** write inside the tenant advisory lock — delete future rows for `(park,date,batch)` whose `operator_id` ∉ new plan, then upsert; guarded by `atomic-readmodel-sync-guard`.
- **OPS-CASCADE-2 [FIX]:** source operator cap solely from `vaccination_capacity_config` (or define explicit common-cap-wins precedence) instead of `MAX(per-position)`; two-position parity test. This is the one item demonstrable on seeded production data today.
- **OPS-CASCADE-3 [FIX]:** reject a leave whose range intersects another operator's approved leave (park-scoped) in `ApplyLeave`; matches FE rule.
- **OPS-CASCADE-4 [FIX]:** change `valid_from`/`effective_from` gates to `<= $3::date` (matching the leave predicate) so a seat starting tomorrow is not counted today; fixed-clock exclusion test.
- **WKOFF-3 [FIX]:** feed operator availability into candidate-date scoring (penalize/exclude zero-eligible-operator dates within safe window) using the shared `(tenant,park,date,cap)` session cache to avoid N+1.
- **WKOFF-4 [FIX/BUILD]:** on empty candidate set, **defer/flag** the drive (obligation stays unbatched with surfaced blocker) rather than creating a zero-operator batch sized at the base cap.

---

## (B) Clinical-rule regression risk (guardrails on the re-plan engine)

| id | sev | title | file |
|---|---|---|---|
| CAP-06 | P2 | No re-plan engine exists → no guarantee a cap-driven re-batch preserves shot-cap / safe-window / clinical rules | `drive_planner.go:391` |
| LEAVE-06 | P2 | Clinical rules preserved today only because nothing re-plans; the missing engine must route through them | `drive_planner.go` (shot-cap/safe-window) |
| WKOFF-5 | P3 | Re-plan clinical re-application, idempotency, scale-safety untestable until the trigger exists | `sweeper.go` / `drive_planner.go` |

**Root cause:** the clinical guards — `selectIDsWithinVisitShotCap` (`drive_planner.go:391`), `driveCandidateFeasibleOnPlannerDate` (`:318`), live/killed spacing, ET+TT dose-2, kid/adult, per-animal shot cap, clinical-defer set — run **only** in the forward-batching path. Because no re-plan exists, they are trivially non-regressed today, but a naive cap/N/leave/week-off re-batch that hand-reassigns dates/operators (raw DELETE+INSERT) would bypass them.
**Failure:** re-batch onto a date violating the safe window / per-animal daily shot cap; lost clinical-defer state; broken combo/ET+TT dose-2 pairing (`AlignComboDrivesAsOf` maintains it only for still-planned batches).
**Fix [BUILD]:** the re-plan engine (A) must **only unbatch** (return obligations to unbatched) and let the existing sweeper re-run `drive_planner` + shot-cap + safe-window — **never** hand-reassign dates/operators; exclude in-progress/completed; preserve per-obligation clinical-defer + combo pairing; ship with adversarial tests (mid-transition, duplicate-batch, shot-cap, safe-window, zero clinical regression) before merge. This is the proposal §6 non-negotiable and §7 red-test-first gate.

---

## (C) Scale / latency / render

| id | sev | title | file |
|---|---|---|---|
| SCALE-CTAC-3 | P1 | Over-cap uses single tenant `max_per_day` + hardcoded `available_operators=1` → N-parallel drives flood CT with false `over_cap_required` | `processintegrity/repository.go:531-533,1208-1216`; `service.go:253,316` |
| SCALE-3 | P2 | Cascade has no scoped invalidation → only mechanism is a full future re-sweep across every park | `sweeper.go:731`; `park_consolidation.go:488` |
| SCALE-1 (park) | P2 | Per-candidate-date availability SQL fan-out not shared across dates; each probe runs `COUNT(DISTINCT)` load agg | `park_consolidation.go:498`; `visit_shot_lock.go:488` |
| SCALE-1 (release) | P2 | Only batch-release primitive is a per-batch `tx.Exec` loop (N+1 write) with 4 sub-selects each | `repository.go:4134` |
| SCALE-4 | P2 | Cascade unbatch re-runs the 6-LEFT-JOIN unbatched god-CTE over the whole re-opened park | `repository.go:2169` |
| SCALE-5 | P2 | Reads reflect operator config only via persisted assignments → correctness forces full assignment-table rewrite per config edit | `processintegrity/repository.go:1216` |
| SCALE-CAL-01 | P2 | `drive_ops` CTE scans **every** tenant `vaccination_drive_assignments` row per shed-summary page (unfiltered) | `vaccinationexecution/repository.go:2577` |
| SCALE-2 | P3 | Availability cache key includes `capPerOperator` → fragments cache, re-runs identical god-CTE (cap-independent under normal config) | `sweep_session.go:91`; `visit_shot_lock.go:501` |
| SCALE-CTAC-2 | P3 | Per-obligation correlated LATERAL into `vaccination_drive_assignments` scans N× more rows under N-parallel | `processintegrity/repository.go:618` |
| SCALE-CAL-02 | P3 | Correlated LATERAL per qualifying obligation in shed god-CTE; no seek index for `(tenant,batch,shed)` | `vaccinationexecution/repository.go:2465` |
| MOB-SCALE-1 | P3 | Full serial multi-page roster walk + stop-the-world Room delete+reinsert on every refresh | `ExecutionRepository.kt:312` |
| MOB-SCALE-2 | P3 | Per-emission Room I/O inside 20-flow scan combine grows O(session scans) | `ScanViewModel.kt:181` |
| MOB-SCALE-3 | P3 | Calendar combine parses/sorts ~200 rows on Main dispatcher (no `flowOn`) | `CalendarViewModel.kt:145` |

**C — key items**

- **SCALE-CTAC-3 [FIX] (the one P1 in this group):** `capacity_cfg` reads a single tenant `max_per_day`; `all_rows` flags `over_cap_required` when `expected_animals > max_per_day`, sets `drive_operator_cap=max_per_day`, and **hardcodes** `drive_available_operators = 1-if-assigned`. Under the proposal's N×cap model (3×200=600), a legitimately N-sized drive (550) is misclassified `over_cap_required` on every CT/AC/Adherence read and renders "over 200 operator slots". **Fix:** compare `expected_animals` against real N×per-operator cap for the drive's `planned_date`; persist the true N and cap in the projection updated on re-plan, not a hardcoded 1; adversarial tests at N=1/2/3 + boundary. (Note: the finding's *secondary* claim of summary-count inflation is wrong — `CountByWorkState` groups on a separate dimension; only the per-row classification/rendering is the defect.)
- **SCALE-3 / SCALE-1(release) / SCALE-4 / SCALE-5 [BUILD]:** these are the scale contract on the (A) engine — the cascade must be **event-scoped incremental** (derive exact affected `(park,date-range,operator)` from the delta, keyset-chunked, idempotent), use a **set-based release** (`WHERE batch_id = ANY($1)` / `UNNEST`) not a per-batch loop, avoid re-opening a whole park into the god-CTE per edit, and re-materialize only invalidated assignments so hot reads self-correct — never delete-all-future + full re-sweep. Query-plan proof (no Seq Scan at ~500k rows) + hot-read p95 gate per proposal §7.
- **SCALE-CAL-01 [FIX]:** push request scope into `drive_ops` (`AND ($6=''OR park_id=$6) AND ($7=''OR shed_id=$7)` + `planned_date` window) and add covering index `(tenant_id, park_id, physical_shed) INCLUDE(operator_id)`.
- **SCALE-1(park)/SCALE-2/SCALE-CTAC-2/SCALE-CAL-02 [FIX]:** efficiency hygiene — batch the availability probe across candidate dates / precompute per `(tenant,park)`; add composite seek index for the LATERALs; keep the cache-key cap grain (do **not** drop it — it is load-bearing in the no-config fallback).
- **MOB-SCALE-1/2/3 [FIX]:** incremental delta upsert + TTL/version short-circuit for roster refresh; derive local-done overlay in-memory + conflate scan emissions; add `.flowOn(Dispatchers.Default)` before `stateIn` on the calendar combine.

---

## (D) Missing code that must be built (the config substrate — precondition for everything in A/B)

| id | sev | title | file |
|---|---|---|---|
| N-01 | P1 | `active_operators_per_day` (N) does not exist anywhere — schema, rule_dsl, publish, sweeper | `visit_shot_lock.go:488`; `publish.go` |
| DEFOP-1 | P1 | CEO default operator + fallback chain does not exist in code | `visit_shot_lock.go:499` |
| LEAVE-RM-01 | P1 | No leave-removed/cancel write path at all; `LeaveStatusCanceled` declared but never written | `roster_handler.go:41-45`; `roster_types.go:310` |
| LEAVE-RM-03 | P1 | No loss/duplication-safe unbatch primitive for committed assignments | `repository.go` (rescope only for unbatched singles) |
| OPS-CASCADE-5 | P2 | N, default operator, fallback chain, auto-revert, manual-swap-sticks all unimplemented (capacity always Σ all operators) | `sweeper.go:281/351`; `visit_shot_lock.go:573` |
| LEAVE-08 | P3 | Single common cap / N / default+fallback are proposal-only; leave→fallback has no substrate | `visit_shot_lock.go:505`; `sweeper.go:351` |

**D — build spec**

- **N-01 [BUILD]:** add `active_operators_per_day` to the published capacity/`rule_dsl` contract with validate-or-reject (1|2|3), a lock-safe migration + read-model column, threaded into `AvailableVaccinationOperatorsForDrive`/`operatorCapacityPlanner` so `totalVaccinationOperatorCap = min(available, N) × cap` and assignment fan-out is capped at N. Today N is implicit = "all available operators".
- **DEFOP-1 / OPS-CASCADE-5 / LEAVE-08 [BUILD]:** model CEO default-operator + **ordered** fallback chain as tenant/park config; make it the primary `ORDER BY` key in the availability query (default → fallback rank → load tiebreak) instead of pure load-order; implement single/pair/parallel modes + auto-revert + manual-swap-sticks. Currently `PickVaccinationOperatorForDrive` just returns `operators[0]` by least-load.
- **LEAVE-RM-01 [BUILD]:** add `CancelLeave`/`WithdrawLeave` service + repo + route transitioning `workforce_absences → 'canceled'` with idempotency key + `row_version` + audit. Precondition for any "leave removed → reschedule" cascade to have a trigger.
- **LEAVE-RM-03 [BUILD]:** build the unbatch+replan as a **single advisory-locked transaction** reusing `drive_planner` feasibility/shot-cap/safe-window (never raw DELETE+INSERT), set-based membership move, adversarial tests. (Same engine as A/B — this is its integrity contract.)

---

## Phased implementation order (each phase independently testable + landable)

**Phase 0 — Immediate fix-existing correctness (no new config; lands today, standalone tests)**
- OPS-CASCADE-2 (cap precedence, not `MAX`) · OPS-CASCADE-4 (`valid_from` off-by-one) · OPS-CASCADE-3 (same-date-leave BE guard) · WKOFF-4 (defer instead of zero-operator batch) · WKOFF-3 (week-off in date scoring).
*Gate:* unit + fixed-clock tests; no cascade dependency.

**Phase 1 — Config substrate (schema + contract, no behavior cascade yet)**
- N-01 (`active_operators_per_day`) · DEFOP-1/OPS-CASCADE-5/LEAVE-08 (single common cap + N + default/fallback modeled) · LEAVE-RM-01 (CancelLeave write path).
*Gate:* validate-or-reject config tests; sweeper honors N & default+fallback for **new unbatched** work only (parallel/pair/single actually differ; capacity = N×cap); leave cancel round-trips. Read models still stale for committed drives — accepted, documented.

**Phase 2 — Reactive cascade engine (the hard part; the shared consumer)**
- Domain events (`capacity.changed`/`roster.changed`/`leave.changed`/`weekoff.changed`/`default.changed`) registered in `domain-event-registry.json` (OPS-CASCADE-1, DEFOP-2) → **idempotent advisory-locked consumer** that scoped-releases affected future still-actionable batches/assignments to unbatched (CAP-01, N-02, DEFOP-3, LEAVE-01/02, LEAVE-RM-02, WKOFF-1).
- Integrity primitives: set-reconciling delete + upsert (DEFOP-4, N-03) and the loss-safe unbatch+replan-through-planner (LEAVE-RM-03, CAP-06, LEAVE-06, WKOFF-5).
*Gate:* producer→consumer→re-batch E2E asserting **no lost/duplicated obligations**, preserved clinical outputs (shot-cap, safe-window, defer, combo/ET+TT), exclusion of in-progress/completed, idempotent on replay.

**Phase 3 — Scale-correctness of the cascade + read models**
- Event-scoped incremental invalidation & set-based release (SCALE-3, SCALE-1-release, SCALE-4, SCALE-5) · SCALE-CTAC-3 (real N×cap over-cap classification, P1) · SCALE-CAL-01 (scope `drive_ops`).
*Gate:* query-plan proof (no Seq Scan at ~500k rows), hot-read p95 ≤ 500ms, one config edit = O(affected drives) not O(park); CT/AC no false over_cap.

**Phase 4 — Efficiency hygiene (independent, low-risk)**
- SCALE-1-park, SCALE-2, SCALE-CTAC-2, SCALE-CAL-02 (backend index/cache/batching) · MOB-SCALE-1/2/3 (mobile delta upsert, in-memory overlay, `flowOn`).
*Gate:* micro-benchmarks / plan tests; no correctness change.

**Sequencing rationale:** Phase 0 is pure existing-bug cleanup. Phase 1 gives the knobs something to persist (without it the HRMS N/default/cap selectors are no-ops). Phase 2 is the single engine that satisfies every A + B finding — it must not ship before Phase 1 (nothing to react to) and must carry the clinical + integrity gates. Phase 3 makes that engine and its read models survive the 5k-50k envelope. Phase 4 is deferrable polish.