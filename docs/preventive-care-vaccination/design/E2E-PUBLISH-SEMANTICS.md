# E2E — what publishing a new vaccination plan version actually does

**Status:** in progress. Findings below are empirical, run against a clone of `goatos-stg`
restored onto the OCI dev VM. Every claim states how it was produced.

**Harness:** [`tools/e2e/oci-db.sh`](../../../tools/e2e/oci-db.sh),
[`tools/e2e/case-01-publish-new-version.sh`](../../../tools/e2e/case-01-publish-new-version.sh)

---

## Environment

| | |
|---|---|
| Database | clone of `goatos-stg` on the OCI dev VM, reached over an SSH tunnel at `127.0.0.1:15432` |
| Reset | Postgres **template** `goatos_base` frozen from that clone; each case runs `CREATE DATABASE … TEMPLATE goatos_base` — **9 seconds**, verified |
| Generation | the real `GenerationService`, invoked through `backend/cmd/generate-vaccination-obligations` — the same code path as the `protocol.version.published` handler (`internal/vaccination/app/generation_handler.go:175`) |
| Never touched | Cloud SQL. The harness only ever connects to `127.0.0.1:15432`. |

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
create version as DRAFT  →  attach rules  →  flip to PUBLISHED  →  retire the previous one
```

which is exactly what the redesigned console models with "Start a new version → edit → publish".

---

## Finding 2 — publishing does not supersede the previous version's open work

**Read from source first.** The publish path is
`ProtocolPublishedHandler → GenerateForVersionWithRun → generateForVersionWithRun`
(`internal/vaccination/app/generation_handler.go:175`, `generation.go:777`). Scanning that
function for any cancellation of prior versions returns **nothing**.

The only supersede path is `CancelOpenVaccinationObligationsForGoatExceptVersions`
(`internal/obligation/adapters/postgres/repository.go:1640`), and its single non-test caller is
inside `generateForGoat` (`generation.go:1930`) — the **per-goat** path, reached from
`goat.created` and from stage/location/health/reproductive rechecks. Not from publish.

The CLI's own header says the same thing in operational terms
(`cmd/generate-vaccination-obligations/main.go:1-7`):

> "The :8080 API only generates obligations event-driven (on goat.created); **after publishing a
> new version, the existing cohort has no obligations until generation is run.**"

**Confirmed empirically.** Mid-run, with V2 retired and V3 published and generating:

```
V3 6f01b4ad   scheduled 4,149   deferred 24     ← new work appearing
V2 7d5c2ccc   scheduled 6,852   deferred 56     ← old work still open
```

Both versions hold open obligations for the same cohort at the same time.

*(Final counts and the per-goat overlap query complete when the run finishes — this section will
be updated with the settled numbers rather than the mid-run snapshot.)*

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
Against the OCI VM over an SSH tunnel this fails immediately:

```
generate effective cohort: obligation: cancel by idempotency key: timeout: context deadline exceeded
```

The harness sets `GOATOS_PG_QUERY_TIMEOUT=120s` for remote runs. Fine on a LAN; a trap for
anyone pointing a local binary at the OCI dev DB.

---

## Cases still to run

| # | Case | What it proves |
|---|---|---|
| 01 | publish a changed version over a live cohort | in progress — above |
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
