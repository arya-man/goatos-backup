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

- **Summary-first.** The board returns KPIs, matrices, weekly, verification queue and a small first
  page of drives. Every count stays whole-scope and authoritative; only evidence **lists** moved.
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

| | before | after |
|---|---|---|
| `GET /vaccination/command` | **8611 ms / 753 KB** | **352 ms / 443 KB** |
| closed-without-dose | 2341 (eager) | 164 (lazy, page 50) |
| cohort exceptions | 436 eager, **18.3 s** when cell-scoped | 99 |
| cohort administered days | 103 (eager) | 50 |
| `shedVaccineSQL` | 1594 | 127 |
| `driveOptionsSQL` | 693 | 448 → paged out of the board |
| `cohortSQL` | 491 | 270 |

Every SQL rewrite was diffed row-for-row against the original on live data; `driveOptionsSQL`,
`cohortSQL` and the cohort-exception predicate return **byte-identical output**.

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
| API latency gate | `make api-latency-gate` | `/vaccination/command` over p90 300 / p95 500 / p99 500 ms, or over its 512 KB budget. The policy hard-caps p90 at 300ms, so this budget **cannot be relaxed** per-endpoint. |
| Command-board query-plan gate | `make commandboard-query-plan-guard` | The four plan shapes in §2: broad scan of a hot table where the cell should be a predicate, a materialised CTE re-scanned per outer row, a nested loop discarding tenant-scale rows, a large sort before the LIMIT. |
| `hot-path-inline-sql` (scale-guard) | `make scale-guard` | New multi-line SQL declared inside a function in a postgres adapter — root cause 5, the one that made the others invisible. |

The plan gate carries its **own mutation test**:
`TestCommandBoardPlanGuardRejectsThePreFixShapes` runs it against the pre-fix statement, preserved
verbatim, and fails if the guard passes it. A guardrail that cannot be shown to reject the bug it was
written for is decoration, and this repo has shipped that before.

`hot-path-inline-sql` found **975 pre-existing offences across 116 files** when introduced. They are
recorded in `tools/scale-guard/baseline.txt` as a shrink-only ratchet: new inline SQL is blocked,
existing debt is visible and burns down. Raising a baseline count is not an accepted way to land a
change.

## 7. Residual risks

- **`driveOptionsSQL` is still the board's heaviest single statement** even paged. It is bounded and
  gated, but it is the first place to look if the board drifts.
- **`unavailableSections` is a new contract field.** A client that ignores it will render a silently
  incomplete board rather than a degraded one.
- **The `cohortSQL` group-by still spills to disk** on the representative dataset. It is inside
  budget today; a materially larger cohort matrix would need an index or a narrower grain.
- **Plan-gate thresholds are fixture-relative**, so growing the fixture without revisiting the
  ceilings weakens the gate.
