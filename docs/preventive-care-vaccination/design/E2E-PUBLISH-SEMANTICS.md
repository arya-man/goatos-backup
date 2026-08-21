# E2E — what publishing a new vaccination plan version actually does

**Status:** in progress. Findings below are empirical, run against a clone of `goatos-stg`
restored onto a disposable staging clone. Every claim states how it was produced.

**Harness:** [`tools/e2e/e2e-db.sh`](../../../tools/e2e/e2e-db.sh),
[`tools/e2e/case-01-publish-new-version.sh`](../../../tools/e2e/case-01-publish-new-version.sh)

---

## Environment

| | |
|---|---|
| Database | a disposable clone of the staging database, reached on a local forwarded port |
| Reset | Postgres **template** `goatos_base` frozen from that clone; each case runs `CREATE DATABASE … TEMPLATE goatos_base` — **9 seconds**, verified |
| Generation | the real `GenerationService`, invoked through `backend/cmd/generate-vaccination-obligations` — the same code path as the `protocol.version.published` handler (`internal/vaccination/app/generation_handler.go:175`) |
| Never touched | Cloud SQL. The harness only ever connects to a local forwarded port. |

### Baseline (identical for every case)

```
protocol V1  bb18319d  retired    148 completed,      0 open
protocol V2  7d5c2ccc  published  5,644 completed · 3,049 canceled · 56 deferred · 6,852 scheduled
```

15,749 obligations total. Both terminal and open work are present in volume, which is what
makes the "does publishing disturb finished work?" question answerable.

---

## Finding 1 — the version lifecycle is enforced by the database, not just the app

Three separate guardrails fired while building the case. All three are correct and worth
knowing about, because they mean a bad config change cannot be smuggled in by SQL.

**1a. Only one live plan per scope at a time.**

```
ERROR: conflicting key value violates exclusion constraint "protocol_versions_published_no_overlap"
```

`EXCLUDE USING gist (tenant_id, protocol_id, scope_type, COALESCE(scope_id,…),
daterange(effective_from, effective_to) WITH &&) WHERE (status = 'published')`
— migration `000001_…baseline.sql:7126`.

Partial on `status='published'`, so retiring the old version frees the range. You physically
cannot have two overlapping published plans for the same scope.

**1b. A published version is immutable.**

```
ERROR: published protocol version 7d5c2ccc… is immutable; create a new version for config changes
```

`ensure_published_protocol_version_is_immutable()` — `…baseline.sql:746-767`. Once published,
`protocol_id`, `scope`, `version`, `version_label`, `effective_from`, `effective_to`, `rule_dsl`,
`proof_policy`, `sop_version_id`, `drafted_by`, `published_by`, `published_at` are all frozen.
The **only** permitted transition is `status → retired`.

Note the consequence: retiring cannot also set `effective_to`. The date range is released by
the status change alone, because the exclusion constraint is partial.

**1c. Rules may only be attached to a draft.**

```
ERROR: protocol version … is published, not draft; published config is immutable
```

`ensure_protocol_child_version_is_draft()`. So the real lifecycle is:

```
create version as DRAFT  →  attach rules  →  PUBLISH TRANSACTION { retire previous, publish draft }
```

which is exactly what the redesigned console models with "Start a new version → edit → publish".

---

## Finding 2 — publishing leaves the old version's open work behind, orphaned

**CONFIRMED. Severity: data integrity, not user-facing.** Case 01, settled numbers:

```
BEFORE                                    AFTER publish V3 + generation
V2  scheduled 6,852  deferred 56          V2  scheduled 6,852  deferred 56   ← unchanged
                                          V3  scheduled 7,209  deferred 62   ← newly generated
    completed 5,644  canceled 3,049           completed 5,644  canceled 3,049 ← byte-identical
```

Generation reported `generated=7271 deferred=62 failed_goats=0 suppressed_trusted=4155`.

**6,908 obligations remain `scheduled`/`deferred` against the retired V2 and are never
superseded.** 6,232 goat+dose pairs exist twice. A single goat, `et_tt_revac`:

| version | due | status |
|---|---|---|
| V3 (new, 91-day interval) | 2026-11-03 | scheduled |
| V2 (retired, 182-day) | 2027-02-02 | scheduled |

For vaccines whose rules did not change, the duplicate carries an identical date —
`fmd_revac` 2026-12-30 twice, `hs_revac` 2027-02-20 twice.

### Why this is not (yet) an operator-visible bug

Every read path filters `pv.status = 'published'` (e.g.
`internal/calendar/adapters/postgres/canonical_read.go:199,384,648,1066,1810`). Measured on the
same database:

```
open obligations under PUBLISHED versions   7,271   ← what the calendar shows
open obligations under RETIRED versions     6,908   ← invisible
duplicate goat+dose pairs an operator sees      0
```

The sweeper behaves the same way — a run logs `sweep version 6f01b4ad…` only, i.e. the published
version. The orphans are never batched, never escalated, never closed.

### What it does risk

- **They never reach a terminal state.** 6,908 rows sit at `scheduled` forever, growing by
  roughly the size of the open cohort on every publish.
- **Anything that does not join on `pv.status='published'` double-counts** — analytics,
  Cube models, exports, adherence maths, ad-hoc SQL. The correctness of every such consumer
  currently depends on remembering an unwritten rule.
- **The intent already exists but is not wired to publish.**
  `CancelOpenVaccinationObligationsForGoatExceptVersions` does exactly the right thing, with the
  right predicate. It is simply never called from the publish path — only from per-goat recheck
  (`generation.go:1930`). A goat that happens to be rechecked later *does* get its stale rows
  canceled, which makes the residue non-deterministic.

### Suggested fix (not implemented)

Call the existing cancel with the newly-effective version list once per cohort after a publish
generation completes, or emit a per-goat recheck for the affected cohort. The function, its
predicate, its status events and its outbox writes already exist and are already exercised by
the per-goat path; this is a wiring gap, not new behaviour.

---

## Finding 3 — terminal work is protected by the cancel predicate

When supersede *does* run (the per-goat path), its predicate is explicit
(`repository.go:1668-1678`):

```sql
AND oi.status IN ('scheduled', 'due', 'deferred')
AND NOT (oi.protocol_version_id = ANY($3::uuid[]))
```

`completed` and `canceled` are not in that list and therefore can never be touched. In-progress
states are likewise excluded — the function's own comment says *"Terminal and in-progress work
are left untouched."*

This is the property that matters for the CEO-facing claim **"vaccinations already done are
never touched"** in the redesigned console. It is enforced in SQL, not in application code.

---

## Finding 4 — the 3-second query timeout is wrong for a remote database

`GOATOS_PG_QUERY_TIMEOUT` defaults to `3s` (`internal/platform/postgres/postgres.go:30`).
Against a remote clone over a forwarded port this fails immediately:

```
generate effective cohort: obligation: cancel by idempotency key: timeout: context deadline exceeded
```

The harness sets `GOATOS_PG_QUERY_TIMEOUT=120s` for remote runs. Fine on a LAN; a trap for
anyone pointing a local binary at the remote clone.

---

## Cases still to run

| # | Case | What it proves |
|---|---|---|
| 01 | publish a changed version over a live cohort | **done** — findings 1-4 above |
| 02 | goat created **with** DOB | birth-age trigger produces the right first-dose date |
| 03 | goat created **without** DOB | null-DOB path (`post_arrival`), no crash, no silent skip |
| 04 | sheet / bulk import | same obligations as single entry |
| 05 | procurement arrival | warm-up wait, wave 1 / wave 2 spacing |
| 06 | drive in flight during publish | running work is untouched |
| 07 | completed dose then publish | history not rewritten |
| 08 | interval shortened vs lengthened | dates move the correct direction |
| 09 | vaccine switched off | future work stops, past work retained |
| 10 | re-run generation twice | idempotency key holds, no duplicates |
| 11 | live/killed spacing conflict | the gap floor pushes the date, never earlier |
| 12 | calendar rendering | the dates a CEO sees match the obligation rows |
