# `/vaccination/command` latency and the anchor-date boundary on projections

**Status:** active · **Recorded:** 2026-08-31 · **Owner:** platform

## 1. The finding

`GET /vaccination/command` — the CEO Command Board behind
`https://dashboard.mesha.sg/vaccination?scope_mode=company` — returned **HTTP 500 after ~15.1s** in
staging. The user-visible symptom was **"Unable to load command board"**: no KPIs, no matrix, no
picker. The API log named the statement that ran out of time:

```text
vaccination command board: closed-without-dose rows: timeout: context deadline exceeded
```

The deployed API runs `GOATOS_PG_QUERY_TIMEOUT=15s`, so this is a statement exhausting the pool
deadline, not a crash.

**The dataset is small.** On the representative OCI clone of staging:

| table | rows |
|---|---|
| `obligation_instances` | 70,843 |
| `vaccination_completions` | 5,798 |
| live `goats` | 1,633 |
| `obligation_batches` | 350 |
| `locations` | 179 |

Nothing here is large. Anything taking seconds against this shape is a query or architecture defect,
not a capacity problem, and must be treated as one. Adding hardware, raising `GOATOS_PG_QUERY_TIMEOUT`,
or widening the endpoint's latency budget would each have hidden the defect rather than fixed it.

### Measured before

Per-section, measured through the OCI tunnel from a local machine (see §5 on how to read these):

| section | ms | rows |
|---|---|---|
| `shedVaccineAnimalSQL` | 2379 | **0** |
| `closedWithoutDoseSQL` | 2341 | 50 |
| `shedVaccineSQL` | 1594 | 739 |
| `driveOptionsSQL` | 693 | 51 |
| `cohortSQL` | 491 | 407 |
| `cohortExceptionSQL` | 436 | 45 |
| `shedDoseSQL` | 188 | 940 |
| `kpiSQL` | 183 | 1 |
| `cohortDaySQL` | 103 | 146 |
| `weeklySQL` | 70 | 31 |
| `verifyQueueSQL` / `cohortHeadSQL` / `vaccineCodeSQL` | 47 / 46 / 41 | — |
| **total (executed sequentially)** | **8611** | |

Fourteen statements, executed one after another in a single SSR request, against a 15s deadline.

## 2. Root causes

Five, and they compound:

1. **Fourteen sequential live reads in one request.** No concurrency, so the endpoint's wall clock
   was the *sum* of its sections.
2. **Drilldown payloads computed eagerly, tenant-wide, for every cell.** Closed-without-dose animals,
   shed-vaccine flagged animals, cohort exceptions, administered-day details and proof videos were
   ~5.3s of the ~8.6s — **62% of the endpoint spent computing evidence for cells nobody had opened.**
3. **Capped results over unbounded pre-limit work.** `shedVaccineAnimalSQL` sorted an *estimated
   57,176 rows* to return 500, and on the live tenant spent 2.4s to return **zero**. The cell it was
   explaining was applied in Go, after the query.
4. **A materialised CTE re-scanned per candidate row.** `closedWithoutDoseSQL`'s final `LATERAL`
   re-scanned the ~71k-row `scoped` CTE **323 times** to find each animal's most recent closure, and
   decorated the whole candidate set before applying `LIMIT`. **This is the reported 500.**
5. **Fourteen anonymous SQL blobs on a hot path.** Every statement was a function-local string
   literal inside `VaccinationCommandBoard`. A query-plan test and `tools/scale-guard` can only
   address SQL they can *name*, so all fourteen were **structurally exempt from both**. This is why
   the regression was invisible rather than merely missed.

A latency gate alone would not have caught this: the gate runs against a fixture far smaller than
staging, where every one of these statements is fast. The defect is in how the work **scales**, which
is a property of the plan.

## 3. The immediate, projection-free fix

Recorded because the obvious remedy — precompute the board into a read-model table — is **not
available yet** (§4).

- **Summary-first.** The board returns KPIs, both shed matrices, weekly, the verification queue and
  a small first page of drives. Every count stays whole-scope and authoritative; only evidence
  **lists** moved.
- **The cohort matrix became its own section** (`/vaccination/command/cohort-matrix`). Its three
  statements — the cell aggregate, the true herd head count and the dose-sequence exception count —
  were ~420ms of the board's ~850ms of SQL and alone held the endpoint over its budget. This is a
  real product change: the CEO's cohort grid arrives a moment after the rest of the board, behind an
  explicit loading state. The numbers are identical; only their arrival moved.
- **Drilldowns became lazy, keyset-paginated endpoints**, each *requiring* the cell it explains:
  `/vaccination/command/closed-without-dose`, `/vaccination/command/shed-vaccine-animals`,
  `/vaccination/command/cohort-exceptions`, `/vaccination/command/cohort-days`.
- **The drive picker became its own paginated endpoint**, `/vaccination/command/drives`. It was 448ms
  and **753 KB** — more than the endpoint's entire 512 KB budget — for a dropdown.
- **Closed-without-dose was reshaped**: page first, decorate second, and a set-based
  `DISTINCT ON` latest-closed join replacing the per-animal `LATERAL`.
- **Remaining summary sections run concurrently** under a bounded fan-out of 4.
- **Section-level degradation.** KPIs and the picker are required; every other section degrades to a
  named entry in `unavailableSections` instead of blanking the board. A verification queue that times
  out must not delete the KPI row from the CEO's screen.
- **All SQL is now named and package-level** (`commandboard_sql.go`, `commandboard_drilldown_sql.go`),
  so it can be plan-tested and scanned.

### Measured after

Per-statement, same measurement path as the "before" table:

| statement | before | after |
|---|---|---|
| `closedWithoutDoseSQL` | 2341 (eager, tenant-wide) | **164** (lazy, page of 50) |
| `shedVaccineAnimalSQL` | 2379, returning **0 rows** | cell-scoped, ~1ms on a live cell |
| cohort exceptions | 436 eager; **18,300** once cell-scoped | **99** list / **212** board count |
| `shedVaccineSQL` | 1594 | **127** |
| `cohortSQL` | 491 | **209** |
| `driveOptionsSQL` | 693 | **202**, and paged off the board |
| `cohortDaySQL` | 103 (eager) | **50** (lazy, per cell) |

`driveOptionsSQL`, `cohortSQL` and the cohort-exception predicate were each diffed row-for-row
against the original on live data and return **byte-identical output**.

### End-to-end, over HTTP

Measured through the running API against the OCI clone, 20 samples per endpoint via
`tools/perf/api-latency-gate.mjs`:

| endpoint | p50 | p90 | p95 | bytes | budget |
|---|---|---|---|---|---|
| `GET /vaccination/command` | 398 | **416** | 426 | 443,663 | p90 300 / 512 KB |
| `GET /vaccination/command/drives` | 182 | **185** | 188 | 44,361 | **passes** |
| `GET /vaccination/command/closed-without-dose` | 217 | **222** | 223 | 12,914 | **passes** |

**The board no longer 500s** — it returns 200 in every sample, down from 8,611ms of SQL and a
timeout. The payload is inside its 512 KB budget with 15% headroom, where it was 753 KB before.

**IT DOES NOT MEET THE 300ms p90 BUDGET, and that is stated rather than worked around.** 416ms is
not the 200-300ms class the fix was asked for. Read §7 before concluding anything about it.

## 4. THE ANCHOR-DATE BOUNDARY — no command-board projection or backfill yet

**Maintainer decision, 2026-08-31. This is the load-bearing part of this runbook.**

The standard remedy for a slow leadership aggregate in this repo is a projection/read-model table
(`docs/decisions/high-scale-dashboard-projections.md`). **It is deliberately NOT used here, and must
not be added until the condition in §4.3 is met.**

### 4.1 What "backfill" would mean here

Introducing a command-board projection means writing a table whose rows are *derived truth*,
computed once and then served. For this board that derived truth would precompute:

- each animal's current obligation state and bucket (missed / verified / awaiting / overdue /
  scheduled / closed-without-dose);
- completion and verification state per obligation;
- **source-history anchors** — the baseline vaccine dates each future dose is derived from;
- closure state and reason per animal;
- the cohort, shed×dose and shed×vaccine aggregates rolled up from all of the above.

A backfill would compute those rows for the entire existing herd and history in one pass, and
incremental maintenance would keep them current from domain events.

### 4.2 Why that is unsafe right now

**Vaccination anchor dates are not yet stable.** An anchor date is the baseline vaccine-history
semantics an animal's whole future schedule derives from (`AGENTS.md` → "Vaccination Anchor Date
Rule"). Boosters, revaccination intervals, course membership and live/killed spacing are all computed
*from* the anchor.

If anchor rules change after a backfill, the projected rows are **wrong, and wrong silently**: they
would keep serving a confidently-computed bucket for an animal whose schedule has since been
re-derived. Canonical SQL recomputes from `obligation_instances` and `vaccination_completions` on
every read, so a rule change is reflected immediately; a projection freezes yesterday's
interpretation and reports it as today's truth. On a board whose entire purpose is telling leadership
which animals are behind, that failure mode is worse than the latency it would fix.

The 500 was also **not** caused by the absence of a projection. It was caused by four specific plan
shapes, every one of which is fixable in canonical SQL — as §3 demonstrates.

### 4.3 When a projection may be reconsidered

All of the following must hold. Adding one before then requires a new recorded maintainer decision.

1. **Anchor dates are stable** — the anchor/source-history semantics are settled and no longer under
   active revision, and the schedule-path helper is not expected to re-derive existing animals.
2. **Canonical SQL still misses the target after anchors stabilise** — i.e. `/vaccination/command`
   cannot be held at p90 ≤ 300ms / p95 ≤ 500ms on representative data by query and index work alone,
   evidenced by the latency gate and `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)` output, not by
   assertion.
3. **The remaining cost is measured and attributed** to a specific section, with its plan recorded
   here.
4. The projection then follows the existing contract in
   `docs/decisions/high-scale-dashboard-projections.md`: incremental (never whole-tenant
   delete+reinsert), freshness/coverage-gated with last-known-good serving, rebuilt by
   `make seed-closeout`, and reconciled against canonical rows.

Until then the command board is served from canonical indexed SQL at the current 5k–50k envelope
(`docs/decisions/operational-kernel-5k-50k-scale-envelope.md`).

## 5. How to reproduce and measure

Use the representative OCI clone; do **not** start local Docker Postgres for this
(`CLAUDE.md` → "Hard Local Resource Rule"). Prove the clone matches staging first:

```bash
make oci-stg-db-parity
```

**Reading the numbers.** All timings in this runbook were measured through the OCI tunnel from a
local machine. That path adds real overhead — a `psql` connect round-trip alone is ~126ms — so
production, where the API sits in the same region as the database, should be faster. The before/after
comparison is still valid because **both sides were measured over the identical path**. Do not quote
these as production latency; quote them as a before/after ratio.

## 6. Guardrails, and what each one would have caught

| guard | command | catches |
|---|---|---|
| API latency gate | `make api-latency-gate` | `/vaccination/command` over p90 300 / p95 500 / p99 500 ms, or over its 512 KB budget. The policy hard-caps p90 at 300ms, so this budget **cannot be relaxed** per-endpoint. NOTE: in CI this job runs only on a `workflow_dispatch` with `run_postgres_tests` enabled (`.github/workflows/ci.yml` gates `live-api-latency` on the `postgres` scope) — it does **not** run on an ordinary PR, and that predates this change. |
| Command-board query-plan gate | `make commandboard-query-plan-guard`, and in `ci-local`'s query-plans job | The four plan shapes in §2: broad scan of a hot table where the cell should be a predicate, a materialised CTE re-scanned per outer row, a nested loop discarding tenant-scale rows, a large sort before the LIMIT. |
| `hot-path-inline-sql` (scale-guard) | `make scale-guard` | New multi-line SQL declared inside a function in a postgres adapter — root cause 5, the one that made the others invisible. |

The plan gate carries its **own mutation test**:
`TestCommandBoardPlanGuardRejectsThePreFixShapes` runs it against the pre-fix statement, preserved
verbatim, and fails if the guard passes it. A guardrail that cannot be shown to reject the bug it was
written for is decoration, and this repo has shipped that before.

`hot-path-inline-sql` found **937 pre-existing offences across 111 files** when introduced (the
figure moved as the rule's detection was tightened during review — a leading `--` or `/* */` header
and a statement split across a `+` no longer evade it, and concatenated halves now count once). They are
recorded in `tools/scale-guard/baseline.txt` as a shrink-only ratchet: new inline SQL is blocked,
existing debt is visible and burns down. Raising a baseline count is not an accepted way to land a
change.

## 7. Residual risks and what is NOT proven

**The board misses its own latency budget.** `GET /vaccination/command` measures p90 416ms against a
gate set at 300ms, so the entry added to `tools/perf/hot-paths.vaccination.json` **fails today**. The
budget was deliberately not relaxed: the policy in `tools/perf/api-latency-policy.mjs` hard-caps p90
at 300ms and refuses any per-endpoint value above it, and lowering the bar would turn the one gate
that can catch a recurrence into a rubber stamp. What is left is honest, ordinary aggregate work,
not a pathological shape:

- The floor is two ~210ms statements — the cohort matrix and the cohort-exception count — running
  concurrently over ~71k obligations. Nothing about either is a re-scan, a fan-out or a late LIMIT
  any more; they are hash aggregates over the tenant's obligations, which is what the board asks for.
- The measurement path adds real overhead: a warm pooled round-trip to the OCI clone is **18.8ms**,
  paid by every one of the board's nine statements. The transport does parallelise (six concurrent
  200ms queries complete in 258ms), so this is roughly 50-60ms on the critical path, not 170ms.
  Same-region production would recover that but would still land near 360ms.
- Raising the fan-out from 4 to 6 moved p90 by less than run-to-run noise, and `work_mem` at 64MB
  made the exception count *worse* (a plan flip, 212ms → 340ms). Neither is a lever.

Getting to 300ms from here needs a product decision, not another query rewrite: either fewer sections
on first paint, or the projection §4 currently forbids. That decision is open.

**The shed-vaccine drilldown is not browser-verified.** `vaccineCodeSQL` returns zero rows on this
clone (no published vaccination protocol version), so `shedVaccineColumns` and `shedVaccineMatrix`
are empty and that drawer cannot be opened against this data. It was empty on `origin/main` too, so
this is the dataset, not the change. The path is covered by the Postgres integration tests and by
the query-plan gate, not by a rendered screenshot.

**The plan gate's fixture is 400 animals / 1,600 obligations.** At that size Postgres will not choose
an index over a sequential scan, so the gate asserts that work is **proportional to the answer** — a
cell predicate actually reduces the row set, no CTE is re-scanned, nothing multiplies — and NOT that
a particular index was chosen. A regression that reads the tenant exactly once still passes every
summary-statement ceiling by design. Growing the fixture without revisiting the ceilings weakens it.

**`unavailableSections` is a new contract field.** A client that ignores it renders a silently
incomplete board rather than a degraded one.

**The baseline is a single ratchet wearing 116 hats.** Every `hot-path-inline-sql` entry carries the
same owner and the same 2027-02-28 expiry, so it will expire as one cliff and invite a bulk
extension. The ratchet also only blocks growth: a file whose real count later drops leaves silent
headroom.

**`driveOptionsSQL` remains the heaviest single statement** even paged, and the cohort matrix's
`animal_count` still needs a DISTINCT over the tenant's obligations. Both are the first places to
look if the board drifts.

## 8. 2026-08-31 second pass: what the rebase changed, and what is still red

The first pass was measured against the OCI clone before it was migrated forward. Re-measuring
after a rebase onto `origin/main` changed several facts, and one of them was a regression that only
the new data exposed. Recorded here because each was found by measuring rather than by reading.

### 8.1 The clone runs AHEAD of `main`, because of an unrelated workstream

Mid-session the clone moved twice: `000226 -> 000228`, then `-> 000231`. The first step is on
`main`. The second is NOT: `000229_health_diagnosis_engine`,
`000230_health_case_closure_and_verification` and `000231_health_cases_diagnosis_columns` come from
the **health** workstream's branch and had not merged. The API refuses to boot against a database
ahead of its own embedded migrations (`migration_drift_dbahead_fatal`, `internal/platform/
migrationguard`), and that guard is unconditional by design — there is no override env, and none
was added.

To measure at all, those three `.sql` files were copied into the working tree from the health
branch **and deliberately left uncommitted**. They are not part of this change and must not be
committed with it; `git status` will show them as untracked. Anyone reproducing this measurement
has to do the same, or wait for the health branch to land. Do not "fix" the boot failure by
weakening the migration guard or by fabricating placeholder migration files.

Practical consequence: the OCI clone is a shared surface that other workstreams migrate. Treat its
schema version as a moving target and re-check it before trusting a measurement.

### 8.2 A disk spill that only appeared once anchor retirement grew the table

`main`'s anchor work (`fix(vaccination): retire replaced plan anchors`, `prevent past obligation
generation`) took `obligation_instances` to 64,678 `canceled` rows. That pushed
`commandBoardCohortExceptionCTE` from ~212ms to **763ms**.

The cause was the same class of mistake twice over: its `candidate` CTE deduplicated on SEVEN
columns, two of which were DERIVED — a text-cast `COALESCE` for `park_id` and a `regexp_replace`
for `family`. Both are pure functions of columns already in the key, so they never changed which
rows survived the `DISTINCT`; they only widened ~81k rows to 114 bytes and put a regexp in the sort
key. The sort went past `work_mem` into `Sort Method: external merge  Disk: 8696kB`.

Deduplicating on the narrow base columns and projecting the derived ones in an outer `SELECT` lets
the planner choose a parallel `HashAggregate`: **763ms -> 203ms, byte-identical output**, with the
cell-scoped drawer path unchanged at ~90ms.

**Rule this leaves behind:** never carry a derived column inside a `DISTINCT`/`GROUP BY` key when it
is a function of columns already in that key. It cannot change the result and it can change the
plan.

### 8.3 `closedWithoutDose` is now legitimately zero on this clone

It reads 0 where it read 323, and that is not a regression:
`1279 verified + 241 overdue + 113 scheduled = 1633 targets`, so the residual bucket is empty. Also
worth knowing: this database contains **no `missed`-status rows at all**, so `oi.status = 'missed'`
— and therefore `any_missed` — never fires in practice.

This is a hazard for the latency gate, not for the product: the committed manifest asserts
`animals >= 1` on `vaccination_command_closed_without_dose` so the gate cannot certify a latency
measured over an empty result. On this clone that assertion cannot be satisfied, so the endpoint
was NOT latency-measured in this pass. The assertion was deliberately left in place rather than
weakened. The **plan guard** is unaffected and stays deterministic: its fixture seeds
`commandBoardPlanFixtureResidual` (every fourth animal) and asserts the tile equals it, so the
detector never depends on live OCI data.

### 8.4 Bytes were not the problem; round trips were

The board payload was 441KB, 92% of it the shed x dose grid. Interning that grid's 113 shed
identities and 21 dose labels, and emitting IST business dates instead of RFC3339 instants, cut it
to 155KB and the whole payload to 207KB — and the board's p90 moved by roughly nothing (343 ->
~340). Taking the section off first paint is what moved it, to p90 255-290.

A correction worth keeping, because the first read of the data was wrong: `operational_location_
display` is NOT redundant with `shedName`. It matches on only 36 of 1318 rows, because 1282 carry a
partition label. The conclusion came from inspecting a single row; checking the whole set reversed
it.

Per-section decomposition through the tunnel, which is how the round-trip cost was identified
(`EXPLAIN ANALYZE` discards rows, so it cannot see result transfer):

| section | exec ms | fetch ms | transfer |
|---|---|---|---|
| ShedDoseSQL | 102 | 216 | 114 |
| driveOptionsSQL | 144 | 171 | 27 |
| ShedVaccineSQL | 138 | 188 | 50 |
| KPISQL | 50 | 73 | 23 |

`commandBoardSummaryConcurrency` also moved 4 -> 6. The comment justifying FOUR was measured when
the cohort matrix still ran in the fan-out and the picker cost 448ms; with seven sections, two of
them sub-millisecond, four slots split the five real statements across two waves. Six, not seven,
so one board render cannot claim the whole ten-connection pool.

### 8.5 UNRESOLVED: `vaccination_command_cohort_matrix` is over budget

**p90 323-330 against a 300ms budget. This is an open blocker, not an accepted state.**

What is already established about it:

- The pole is `commandBoardCohortSQL` alone at ~244ms. Removing the exception count from that
  endpoint entirely changes nothing — measured 318 either way — because the fan-out already hides
  it behind the larger statement.
- Its cost is materialising **80,960 obligation rows to produce 407 cells**. The plan is already
  seq-scan + hash-aggregate throughout with no spill; there is no bad shape left to fix.
- Fusing `cell_totals` and `cell_animals` into one pass with `COUNT(DISTINCT goat_id)` is **worse:
  519ms against 244ms**. The two-aggregate split is load-bearing — do not "simplify" it back.
- The obvious filter is UNSAFE. 64,678 of those rows are `canceled`, but a canceled obligation
  still contributes its animal to a cell's `DISTINCT animal_count`. Dropping them would change what
  the number means, so it must not be done as a performance edit.

Options that remain, none taken unilaterally: park-scope the grid so no single request aggregates
tenant-wide; or revisit the projection ban in §4 for this one aggregate — which the anchor-date
reasoning still argues against. Relaxing the budget is not an option; `tools/perf/
api-latency-policy.mjs` hard-caps it and will not let anyone raise it.
