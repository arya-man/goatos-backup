# Weighing device E2E — 2026-08-03 bug ledger

Standing record of the first-ever weighing run on real hardware. Read this before
reopening anything below. A row marked **CLOSED** is closed: do not re-litigate it
without new evidence, and if you do have new evidence, add a row rather than
editing history.

**Baseline:** `origin/main` = `bf4f875be` at branch time (main advanced to
`67c15e18e` mid-session — one unrelated Firebase-distribution commit).
**Branch:** `weighing-e2e-batch`.

## What the run actually was

Six weighing buckets, two parks, three operators, both categories, all completed:

| Park | Shed | Category | Operator | Captured |
|---|---|---|---|---|
| CBE | Godel 1 | individual | Pramod | 5 |
| CBE | Yashoda 1 | individual | Pramod | 5 |
| CBE | Gandhi 1 | individual | Dinakar | 2 |
| CBE | Gandhi 2 | individual | Dinakar | 3 |
| CPT | Castro 1 | lump-sum | Amit | 130 kg / 13 |
| CPT | Mandela 2 | lump-sum | Dinakar | 120 kg / 22 |

17 proof artifacts, all `completed`. No proof reuse, no blanks, no observation
without proof.

---

## CLOSED — fixed and verified

| # | Bug | Root cause | Evidence |
|---|---|---|---|
| W-01 | Growth Director could record a weighing but not upload its mandatory video | Proof routes gated on `TaskExecute`; the role holds `WeighingExecute` and deliberately not `TaskExecute` | Live 403 in server log; fixed on the ROUTE (`AnyPermissions{TaskExecute, WeighingExecute}`), never by widening the role. Field-confirmed: Dinakar captured with proof. Guard added. |
| W-02 | Every individual capture raised TWO verification rounds | Two callers of `attachIndividualProof` (ViewModel + reconciler) race; the second mints a `:proof:` key and re-enqueues identical content, which the server reads as an edit | Field-confirmed after fix: 5 captures → 5 items, **0** withdrawals |
| W-03 | Verification withdrawal was silent | `WithdrawItemsBySource` was a bare `UPDATE` with no event, while `item.pending` had already published | Retraction now emitted in the SHARED verification layer so vaccination inherits it |
| W-04 | 12 outbox events permanently undeliverable | `trace_id` set to the idempotency key; schema caps `trace_id` at 200 but allows keys to 320 | Proven by running the relay against a copy: `at '/trace_id': maxLength: got 226, want 200`. Fixed with a bounded digest. **See W-04b for the misdiagnosis.** |
| W-05 | Second task in a park-week refused with a bare 409 | Campaign-grain unique index on `(tenant, park, period_type, cadence, period_start)`; `weighing_category` lives on the shed, so it could not discriminate. Predicate also excluded only `canceled`, so a **closed** campaign held the slot forever | Index dropped (`000081`); shed-grain `uq_weighing_open_shed_per_park_date` is the real invariant and holds across campaigns |
| W-06 | Client rendered `HTTP 409 Conflict` | 16 sites rendered `throwable.message`, which on a Retrofit `HttpException` IS the status line | Client now renders server `code`/`message`/`field_errors`; a raw throwable message can no longer reach the screen |
| W-07 | Submit greyed out with no reason | No disabled-with-reason anywhere | `submitBlockedReason` names one next action, most-blocking first, in four locales |
| W-08 | `expected_animal_count` hardcoded 1, `mismatch_status` hardcoded `extra_scan` | Free-flow has no roster, so both are meaningless by construction | Derivations removed; `mismatch_status` dropped (`000082`). Also uncovered and fixed: `RemainingCount` made every weighed animal a NEGATIVE remainder |
| W-09 | Campaign edit was broken on `main` | `UPDATE weighing_campaign_sheds` qualified a predicate with `weighing_campaigns.tenant_id` | `SQLSTATE 42P01`, reproduced on stock main, fixed |
| W-10 | 4xx logged with no error code; 403 logged only `roles:""`; app emitted nothing to logcat | `WriteError` deliberately did not log 4xx (with a test enforcing the silence) while every call site already carried a machine `code`. `TelemetryInterceptor` was disabled entirely on dev. No API-failure event existed | Fixed at the seams, not call sites |
| W-11 | Alerts entry point was a top-right bell that did nothing | Weighing had no `alerts` item in the nav registry, so an icon was hand-placed. `onClick = {}` | Alerts is now a bottom-bar destination on all three surfaces, composed from the backend registry. Bell removed |
| W-12 | Every CEO count was invented | `captured_count` did not exist, so each surface derived its own; `park_name` was on the wire but undeclared | One backend-owned count; tiles and rows read it. Dead text removed ("Amit now sees the work" regardless of assignee, invented "Day 1/2/3") |
| W-13 | WorkManager vetoed the outbox drain on a loopback base | `NetworkType.CONNECTED` requires a VALIDATED network; loopback is not a `Network`. Constraint blocks before the loopback-aware gate is consulted | Keyed on the base URL, never build type. Also `KEEP`→`UPDATE` so an upgraded install is not pinned to its first-ever constraint |

## CLOSED — process/judgement errors, recorded so they are not repeated

| # | What | Correction |
|---|---|---|
| W-04b | Diagnosed the 12 dead events as a missing `actual_location_id` and reported it as fact | Wrong. The validator accepts that payload. The `:proof:` post was both the edit-path write (blank location) AND the long one — length was causal, the field was a confounder. **Always run the validator, never diff payloads and infer.** |
| W-14 | Merge kept a dead property alive so its tests would pass | The two properties had disjoint string sets and different behaviour; 6 tests proved nothing about the screen. Retargeted at the rendered property, dead property and strings deleted. **Never preserve a symbol to keep a test green.** |
| W-15 | Claimed the phone shows a green tick for unsent weights | It shows "Scanned"; `"Completed"` requires `syncedToBackend`. The row label is honest. **See W-16 for the real gap.** |

---

## OPEN — carried forward

| # | Issue | Severity | Note |
|---|---|---|---|
| W-16 | Submit gate trusts a LOCAL draft for the weight but demands server confirmation for the proof | **P1** | The asymmetry is what lets a shed close over weights the server never received. W-02's fix made it rare; the gate is what allows it at all |
| W-17 | Event spine has never run | **P1** | All 92 outbox rows were pending. `work_state` has exactly ONE writer (the sweeper), so the kernel's view and the observation tables disagree by design. Kernel worker also starts in SHADOW mode by default |
| W-18 | Verifier flow unproven end to end | **P1** | Queue renders and approval flips the item, but with no relay the verdict handler never runs. It drains to empty and nothing changes — **reads as "works" and is the opposite**. Approval also never closes a bucket; that is a separate `CloseScope` |
| W-19 | 12 pre-existing failures in `weighing/adapters/postgres` | **P1** | Broken `seedWeighingObservationFixture`. Gates the multi-task, rework-rescan and reopen tests — i.e. it is masking coverage of exactly the paths the run did not exercise. Package is permanently red, so it cannot distinguish a new regression |
| W-21 | No "shed is empty" outcome | **P1** | Zero animals is rejected; the only escape is `AbandonScope`, which needs `WeighingMonitor` — leadership only. Day one, an operator at an empty shed must phone a park head |
| W-22 | ExoPlayer bypasses the telemetry interceptor | **P1** | media3 uses its own HTTP stack. It is the verifier's playback engine, and the next act is video review — a dead player would produce no signal |
| W-23 | Outbox/upload path has zero logging | **P1** | A stuck upload prints nothing. This is why the original blocker needed DB forensics |
| W-24 | admin-web weighing is unreachable | **P2** | `/weighing` hard-redirects to `/vaccination`, nav filters it with client literals, backend has no page contract. Un-hiding is a product call |
| W-25 | Proof delete/overwrite has no actor check | **P2** | Tenant-scoped only. Pre-existing; widened by one role. Mitigated by unguessable UUIDs and FKs on attached proofs |
| W-26 | Both new guards are trivially evadeable | **P2** | Proof-auth: `parseRoutes` needs `OperationID` FIRST — reorder and it goes green. Only matches `*Execute`. Nav: baseline key is file+rule, so unlimited NEW violations pass in a baselined file |
| W-27 | admin-web 409 test is a source-text grep | **P2** | Asserts on strings the same commit wrote. `plannerFromCatalog` never executes |
| W-28 | Task header shows one operator | **P2** | CBE has two |
| W-29 | "Lumpsum" vs "Lump-sum" | **P3** | Mock says `Lumpsum`, mobile says `Lump-sum`. Mock is UI truth — needs a maintainer call, not a silent edit |
| W-30 | Only the newest task in a park-week is reachable | **P2** | `DISTINCT ON (park_id)` picks the latest. Lands the day leadership splits a park-week — which is what W-05 enabled |
| W-31 | Edit sheds after publish | **P3** | Recommended shape: allow ADD, never REMOVE (a bucket may hold captures/proof/verification). W-05 gives a workaround: plan leftovers as a second task |

---

## CLOSED — NOT A BUG (maintainer ruling 2026-08-03)

**W-20 — verifier queue is per-animal.** Raised by a judge as a scale/operating-model
defect; I repeated it. **Both wrong.** The queue grain follows the EVIDENCE, and it is
already consistent across every module:

| Path | Evidence produced | Verification items |
|---|---|---|
| weighing individual | one video PER ANIMAL | one per animal — each video is separate evidence |
| weighing lump-sum | one video per SHED | one per shed |
| vaccination | one proof per SUBMISSION | one per submission |

Same rule everywhere: one review per piece of evidence. My "grain inconsistency between
modules" argument was wrong — the modules differ in evidence shape, not in rule.

What remains is arithmetic, not a defect: 5,000 individually-weighed kids means 5,000
videos, because 5,000 videos were recorded. Reducing that means changing what evidence is
captured (a product decision), never the queue grain.

**Do not reopen as a bug.** If review volume becomes an operational problem, the question
to ask is whether per-animal video is still required — not whether the queue should batch
evidence the operator captured separately.

## BANNED — do not reopen

- **Do not narrow the Growth Director's permissions.** He executes weighing like an
  operator AND oversees others. Holding both `WeighingExecute` and `WeighingMonitor`
  is deliberate (maintainer ruling). W-01 was fixed on the route for this reason.
- **Do not add an expected-animal denominator to weighing.** Free-flow has no roster.
  A progress percentage against an invented denominator IS the defect (W-08).
- **Do not re-introduce a herd/goat join anywhere in weighing.** Isolation is total.
- **Do not put a feature entry point in the top-right app bar** (W-11). Bottom bar,
  or drawer per the 2+ modules rule. Guard: `make nav-entry-point-placement-guard`.
- **Do not "fix" W-17 by having the submit path write `work_state`.** The sweeper is
  its single writer by design; eventual consistency there is intended.

## The next test worth running

Run the relay and a kernel sweep against a COPY of the phone-QA data, then assert
the outbox drains to zero pending, `work_state` reaches `completed` for all six
buckets, and notifications route to the right park verifier and park head. Then
drive one verifier approval and re-check. That single action converts W-17 and
W-18 from unproven to proven-or-broken, using data that is already real.

Never run it against `:15544` — that DB holds the only copy of this run.
