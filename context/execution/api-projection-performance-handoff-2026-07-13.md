# API Projection, Availability, and 1M-Scale Handoff

Date: 2026-07-13 (Asia/Kolkata)

> **Scale-envelope note (reconciled 2026-07-14).** This handoff predates and is
> narrowed by the accepted ADR
> [`docs/decisions/operational-kernel-5k-50k-scale-envelope.md`](../../docs/decisions/operational-kernel-5k-50k-scale-envelope.md).
> The current release target is the 5,000–50,000-animal envelope, with
> query-plan proof required at the ~500k obligation-row upper bound;
> one-million / 1–5M-animal deployment is the **future** certification bar, not a
> present release requirement. Read the "1M", "1M-certified", and "P0 scale gap"
> framing below as that future-scale certification target — and as correctness
> work that still matters at the current envelope — not as a claim that
> one-million-scale is a present release invariant. The certification-boundary
> statements that already say this work does not prove 1M are correct as written.

## Purpose

This handoff is for the unfinished API/performance recovery covering:

- Control Tower (CT)
- Action Center (AC)
- Protocol Adherence (PA)
- Calendar
- Preventive Care Vaccination operations/execution/shed screens
- SOP Library lookup used by Vaccination
- GCP scheduled kernel jobs and their local Docker equivalents

Do not describe this work as fully merged or fully 1M-certified. The final serving-read
architecture is substantially improved, but the remaining Calendar canonical reads and
full-tenant projection rebuilds are still P0 scale gaps.

## Current Git State

Canonical repository:

```text
/Users/ravi/mesha/goatos
```

Integration worktree:

```text
/Users/ravi/mesha/.worktrees/goatos-vaccination-projection-publish
```

Branch and recorded heads when this handoff was written:

```text
branch: integration/api-kernel-final-20260713
HEAD:   d37130bdd8f3693f53eba3013b270cebbf6aacd2
origin/main observed: 06f12161c1237ffacd810b02c1e2c52635adf9d9
status: ahead 18, behind 7, clean
```

`origin/main` was advancing repeatedly during this task. Fetch and rebase again before
testing or editing. Preserve all newer mobile, proof-video, navigation, verifier-flow,
and migration work from `main`.

Nothing from this integration batch has been pushed to `main`. Staging was not mutated.

## Work Already Present on `main` Before This Batch

The following should be preserved, not reimplemented:

- Calendar park-drive cards and mobile cards restored earlier: one all-day drive per
  park/date, sheds/vaccines/doses/drive-packet summaries, vaccine names, and counts.
- Future seeded obligations, manager/backup mappings, deferred/overdue behavior, IST and
  `as_of` corrections, mobile execution/scan pagination, Data Gap pagination/cache, and
  mobile task/auth handling from the earlier recovery batch.
- SOP Library N+1 removal:
  - server-side `code_prefix`, search, and keyset cursor;
  - one active Vaccination SOP lookup;
  - batched `latest_versions`;
  - no redundant per-row SOP detail request.
- Existing relevant commits visible in history include:

```text
40672185 perf(sop): batch latest versions into SOP Library list, kill N+1 detail fanout
e1eb2a8c fix(sop): eliminate N+1 scale anti-patterns via batching
```

Verify current `main` rather than assuming these hashes remain the branch tip.

## Completed on the Integration Branch

### 1. Projection-only serving reads

Implemented indexed read models and fail-closed contracts for:

- Vaccination execution
- Vaccination operations
- Vaccination shed summary
- CT / AC / PA Process Integrity rows and pre-aggregated summaries
- Calendar open/due Vaccination event list

The request paths do not fall back to the previous huge canonical compute-on-read query
when a required projection is missing or incompatible. They return a typed retryable 503.

Primary commits after the latest rebase:

```text
232b226c feat(vaccinationexecution): serve execution projection
1cb25d12 feat(vaccinationexecution): serve operations projection
af95502a perf(process-integrity): preaggregate summaries and reject stale reads
f92457de perf(calendar): expose projection freshness and fail stale reads
06565bbf ci(perf): require million-row projection evidence
2d24ca0a fix(projections): fail closed on incompatible snapshots
```

### 2. Serving-read latency evidence

The earlier real-Postgres, HTTP-level scale-shaped run measured:

| Path | p90 | p95 | p99 |
| --- | ---: | ---: | ---: |
| CT | 4.4 ms | 4.4 ms | 4.9 ms |
| AC list | 3.4 ms | 3.5 ms | 3.7 ms |
| AC counts | 3.1 ms | 3.5 ms | 3.7 ms |
| PA | 4.7 ms | 5.2 ms | 5.8 ms |
| Calendar open events | 28.4 ms | 31.4 ms | 42.4 ms |
| Vaccination execution | 6.9 ms | 8.6 ms | 8.6 ms |
| Vaccination operations | 2.5 ms | 2.6 ms | 3.6 ms |
| Vaccination sheds | 6.8 ms | 9.5 ms | 9.9 ms |

The hard policy remains p90 <= 300 ms, p95 <= 500 ms, p99 <= 1000 ms.

Certification boundary: this proves serving reads against scale-shaped projections. It
does **not** prove one-million-command writes, projection rebuild throughput, Pub/Sub/DLQ
recovery, or end-to-end 1M operation.

### 3. Projection freshness and historical-read correctness

Fixed:

- current reads require green/fresh compatible projections;
- Calendar requests must be covered by the projected date window;
- PI historical `as_of` reads require an exact compatible snapshot;
- stale or incompatible projections fail typed 503 instead of silently replaying raw
  source tables.

### 4. OpenAPI/generated-client drift

Committed in:

```text
e7d53937 fix(api): publish vaccination projection metadata
```

OpenAPI and generated TypeScript now expose:

- projection version;
- projected time;
- `asOf`;
- freshness status and lag;
- execution total count/cursor;
- operations cursor;
- shed summary freshness.

### 5. Periodic loading/503 root cause

The direct cause of the observed intermittent loading was found: scheduled rebuilds
changed the serving state to `rebuilding`/yellow before building a replacement, while
the APIs correctly accepted only fresh/green. Every rebuild temporarily hid a valid
last-known-good projection.

Commit:

```text
d37130bd fix(projections): preserve serving snapshots during rebuilds
```

The current branch now:

- preserves compatible last-known-good CT/AC/PA, Calendar, and Vaccination projections
  during a replacement build;
- publishes new serving metadata only after successful atomic commit;
- preserves an old serving snapshot after replacement failure until normal freshness
  policy expires it;
- serializes PI builds and all three Vaccination projection writers with tenant advisory
  locks;
- derives default live `as_of` after acquiring the lock, so a queued build cannot publish
  an already-expired timestamp;
- runs Calendar page refresh/tombstone/state publication in one Repeatable Read
  transaction;
- upserts Calendar state on first bootstrap;
- removes the duplicate Calendar rebuild from obligation-sweeper;
- removes the duplicate standalone Vaccination shed rebuild when obligation-sweeper is
  the temporary owner of shed + execution + operations.

### 6. GCP/local parity scaffolding

Implemented:

- local Postgres 16;
- official Google Pub/Sub emulator + topic/subscription/DLQ bootstrap;
- API, outbox relay, domain consumer;
- generator, obligation, Calendar reminder/escalation, Calendar/PI/Vaccination repair,
  notification workers;
- retention, idempotency cleanup, inventory reconciliation, SOP retry, and partition
  maintenance loop;
- local parity guard and smoke script.

Relevant commits:

```text
abdfa5d7 feat(dev): add local GCP kernel parity stack
d1f25b43 fix(kernel): align local scheduled-worker parity
```

Cloud Tasks has no official local emulator. Local parity intentionally uses the durable
Postgres notification/task state with the same application handlers; staging uses real
Cloud Tasks. No Temporal, Kafka, Redpanda, or Debezium dependency is required for the
current design.

## Rejected Unsafe Work — Do Not Restore It

Do not cherry-pick or reintroduce the dirty-worker implementation from:

```text
6f01cc9b feat(projections): add bounded dirty-scope worker
```

It was reverted. Although it claimed bounded shed work, one dirty shed copied every
unchanged tenant row into a new global version, counted all rows, and flipped global
pointers. That remained O(total tenant rows) per dirty shed.

A later in-place experiment was also rejected because it stamped one tenant-wide
`as_of`/freshness time over sheds rebuilt at different times. Untouched sheds could miss
scheduled -> due -> overdue transitions while the API reported green.

The branch history contains the experiment and its reverts. Judge the final tree, not an
intermediate commit. Before merging, squash/drop the canceling experiment commits if the
reviewer wants clean history:

```text
6f01cc9b / bce5fc52
f6d5870d / 5005f30c
a8fb168a / e965539c
```

Do not drop a revert without dropping the corresponding implementation.

## Remaining P0 Work

### P0-A. Materialize Calendar completed history and date markers

Two real client paths still execute canonical request-time joins:

- `status=completed` Calendar History list;
- `include_date_markers=true` Calendar overview/date-picker markers.

Affected code:

```text
backend/internal/calendar/adapters/postgres/repository.go
  completed_history branch in calendarListSQL
  calendarDateMarkersSQL accepted-completion branch
```

Real callers include admin-web Calendar History/date picker and Android Calendar
overview/history.

The integration manifest now measures both paths, but classifies them honestly as
`canonical_1300_nonempty_latency_only`. That is measurement, not 1M remediation.

Required implementation:

1. Add projector-owned accepted-history rows (stable event identity, tenant, business
   date, park, shed, rule/protocol/vaccine, counts and labels).
2. Add projector-owned date-marker aggregates by tenant/date and relevant scope/filter
   dimensions.
3. Project acceptance, rejection, reversal, import, and obligation status changes; do
   not depend only on the happy-path `vaccination.completed` event.
4. Replace both request-time canonical branches with indexed projection reads.
5. Add bounded date/park/shed/status indexes and `EXPLAIN` gates.
6. Keep these two explicit latency cases in `hot-paths.vaccination.json`:

```text
/calendar/vaccination/events?status=completed&limit=50
/calendar/vaccination/events?include_date_markers=true&limit=1
```

7. Move the two evidence names from the 1300-only boundary to the million-scale
   projection-backed boundary only after real 1M projection proof passes.

### P0-B. Replace full-tenant scheduled rebuilds with real bounded projectors

Current temporary ownership:

- obligation-sweeper serially rebuilds Vaccination shed -> execution -> operations;
- PI projector rebuilds all tenant PI rows;
- Calendar projector rebuilds the full configured date window.

This removes duplicate work and preserves availability, but it is not enough at 1M.
If a build takes longer than the five-minute freshness window, requests still eventually
fail closed.

The current freshness TTL and staging schedule are both exactly five minutes. Scheduler
jitter plus any nonzero build duration can therefore create a stale/503 gap even after the
LKG fix. As an immediate mitigation, make refresh cadence comfortably shorter than the
serving TTL (or derive TTL from a measured schedule + build SLO) and add a delayed-cycle
test. This mitigation does not replace the bounded incremental projector required below.

Required architecture:

1. Reuse the sound parts of the rejected queue design only:
   - durable Cloud SQL/Postgres dirty-scope queue;
   - coalescing;
   - `FOR UPDATE SKIP LOCKED` leases;
   - retry budget, DLQ state, checkpoint and source watermark.
2. Use per-shed (and Calendar park/date) shard state, not one mixed tenant timestamp:
   - `projected_at`;
   - `as_of`;
   - `dirty_through`/source watermark;
   - `next_transition_at`;
   - serving state and row counts/version.
3. Build only claimed shards, atomically flip each shard pointer/version, then bounded-
   delete that shard's old version. Never copy unchanged tenant rows.
4. Enqueue `next_transition_at <= now()` shards so time alone causes scheduled -> due ->
   overdue recomputation without replaying stable sheds every five minutes.
5. Live reads validate relevant shard states. Exact historical reads continue to require
   an immutable full snapshot; do not label mixed live shards an exact historical snapshot.
6. Use statement-level transition-table triggers or application-batch enqueue for hot
   obligation/completion/status writes. Do not run one conflicting queue UPSERT per goat
   across a million-row generation batch.
7. Invalidation coverage must include both OLD and NEW scope on moves and include:
   - goats/shed moves and lifecycle/health/stage fields;
   - obligations/status events/completions/batches;
   - SOP tasks;
   - workforce old + new location;
   - locations and operational attributes;
   - `shed_profiles` and `animal_stage_lookup`;
   - capacity and protocol/rule changes.
8. Use outbox + Pub/Sub as a low-latency wake-up, Cloud Run workers as bounded drains,
   and Scheduler as catch-up/repair. Cloud SQL queue remains correctness truth.
9. Local Docker must use the same queue/worker code with the Pub/Sub emulator.
10. Full recompute commands remain explicit bootstrap/repair tools, not normal schedules.

### P0-C. Fix dev PI projector parity

Staging and local currently schedule Process Integrity projection refresh, but the latest
review found no PI projector job in `infra/envs/dev/cloud_run_jobs.tf`. Because CT/AC/PA
now require a fresh projection, dev will go stale/503 after manual bootstrap.

Add the dev Cloud Run PI projector job with the correct service account, tenant, Cloud SQL
target guard, timeout and five-minute schedule, or replace it with the completed bounded
incremental worker when P0-B lands.

### P0-D. Bootstrap/ETag cleanup

The separate admin-web/bootstrap ETag/cache cleanup remains outside this batch. Keep it a
separate evidence boundary; do not hide its latency inside CT/AC/PA/Vaccination results.

### P0-E. Rebase and migration reconciliation

Latest observed `origin/main` contains a new migration:

```text
000171_proof_artifacts_idempotency.sql
```

This branch has already renamed its migrations to:

```text
000172_process_integrity_projection_summaries.sql
000173_calendar_projection_freshness.sql
```

After fetching/rebasing, inspect the latest migration tail again. Renumber to the next
free versions if `main` advanced further. Never keep duplicate Goose versions.

## Verification Already Run

Green during this task:

- 23 Node performance/evidence policy tests;
- compile-only tests for PI, Calendar, Vaccination execution/operations/shed, and
  projector/sweeper commands;
- `make api-client-check` after OpenAPI regeneration;
- migration validation through the then-current migration tail;
- local GCP parity static guard;
- `git diff --check`;
- Vaccination LKG integration test.

One Calendar integration run initially failed because a test supplied an exclusive
projection `date_to` but queried it as an inclusive Calendar date. The test and projector
default-window contract were adjusted afterward. Re-run the Calendar and PI integration
tests; do not claim them green from the earlier run.

## Required Final Gate Sequence

Run from the integration worktree after rebasing current `main`:

```bash
git fetch origin main
git rebase origin/main

bash backend/tests/integration/validate-postgres-migrations.sh
bash backend/tests/integration/validate-sqlc-query-plans.sh
make api-client-check
make api-latency-policy-test
make local-gcp-kernel-parity-guard

cd backend
go test ./internal/processintegrity/adapters/postgres -count=1
go test ./internal/calendar/adapters/postgres -count=1
go test ./internal/vaccinationexecution/adapters/postgres -count=1
go test ./internal/sop/... -count=1
go test ./cmd/obligation-sweeper ./cmd/calendar-vaccination-projector -count=1
```

Then return to the repo root and run:

```bash
make api-client-check
make api-latency-policy-test
bash tools/dev/local-gcp-kernel-parity-smoke.sh
```

Run the actual HTTP latency gate against the exact final SHA and dataset. The report must
contain non-empty assertions and current-SHA evidence for all ten declared paths. Publish
the resulting styled report through the existing GitHub Pages report category required by
the Goat OS build skill.

Before merge, run CRG change detection/impact review and an independent code review focused
on:

- no request-path canonical replay;
- no per-list-item detail fanout;
- LKG correctness and stale expiry;
- projection-writer concurrency;
- Calendar first bootstrap;
- migration uniqueness;
- Android/admin-web generated-client compatibility;
- local/GCP job parity;
- honest certification boundaries.

## Merge and Restart Instructions

Only after every required gate is green and the independent review has no P0/P1 findings:

```bash
git fetch origin main
git rebase origin/main
# rerun affected gates after any rebase
git mesha-push HEAD:main
```

Verify `origin/main` contains the final commit. Then restart the local API/web/kernel from
that exact `main`, not from this worktree or a stale binary. Inspect existing Docker Compose
projects before stopping anything; do not tear down unrelated developer containers.

Do not deploy or reseed staging as part of this merge unless separately authorized.

## Required Handoff Language

Until P0-A and P0-B are complete, the accurate status is:

> CT/AC/PA and Vaccination serving reads are indexed and fast, SOP row fanout is removed,
> and scheduled rebuilds no longer hide a compatible last-known-good projection. Calendar
> completed-history/date-marker reads and projection build throughput remain outside the
> 1M certification boundary. Full end-to-end 1M CQRS closure is not yet complete.
