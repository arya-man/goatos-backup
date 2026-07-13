# PostgreSQL Integration-Test Harness Optimization Handoff (2026-07-13)

## Status and scope

**Status:** approved follow-up design; not implemented in this session.

This handoff covers only the Go integration/E2E PostgreSQL test harness under
`backend/internal/platform/pgtest` and its callers. It must not change production
database behavior, migrations, event processing, or business rules.

Before editing, inspect `git status`, the current branch/HEAD, and changes to
`backend/internal/platform/pgtest`, test packages, `Makefile`, and CI workflows.
The worktree was already shared and dirty when this handoff was written; do not
commit or overwrite unrelated changes.

## Problem confirmed in the current repository

`pgtest.StartPostgres` currently performs all of the following for every call:

1. starts a new `postgres:16.9-alpine` Docker container;
2. waits for PostgreSQL readiness;
3. extracts every committed migration's Goose `Up` section, concatenates the
   resulting SQL in filename order, and sends it through one
   `psql -v ON_ERROR_STOP=1` process; and
4. registers cleanup that removes the container and its anonymous volume.

Many integration-test functions call `StartPostgres` directly. The E2E fixture
also starts a new migrated container for every story. Consequently, test count
amplifies container startup/readiness polling and migration replay cost. The
current migration path already uses one `psql` process and one connection for
the concatenated migration SQL, so do not assume 133 CLI/process round trips or
replace it with a Goose CLI. Container startup and readiness are the expected
dominant avoidable cost, but the baseline benchmark must measure both terms
rather than asserting the split from code inspection alone. The event-driven
and concurrency-heavy tests themselves are valuable.

As of 2026-07-13, the backend contains no `t.Parallel()` calls. Top-level tests
therefore run serially within each package. The clearest hotspot is the calendar
PostgreSQL integration file: 28 tests make 28 serial `StartPostgres` calls, so
the package pays container startup/readiness/migration/teardown 28 times in
sequence. Across the backend there are 52 test files and 224 textual
`StartPostgres` matches. These counts are discovery context, not invariants.

The E2E suites have only two centralized `StartPostgres` call sites—one in each
shared fixture—but the helpers are invoked by 41 kernel stories and one HRMS
story. Centralizing the call site makes conversion easy; it does not currently
make the container lifecycle package-scoped.

As of 2026-07-13, `backend/migrations/postgres` contains **133** SQL migration
files. This is context, not a durable invariant: recount rather than hard-coding
the number in implementation or assertions because the migration set will
continue to grow.

## Decision

Retain real-PostgreSQL integration tests. Optimize isolation as follows:

- Start **one migrated PostgreSQL container per Go test package/process**.
- Apply all committed migrations once to a template database in that container.
- Give **each test its own database**, cloned from the migration-complete
  template database.
- Give each test its own `pgxpool.Pool` connected to that cloned database.
- Close the pool, terminate any leaked connections if necessary, and drop the
  cloned database from test cleanup.
- Remove the package container and its volume once, after the package exits.

Do **not** share one mutable database across tests. Do **not** replace database
isolation with transaction rollback as the general solution. Several suites
exercise multiple connections, committed state, worker/relay behavior,
`FOR UPDATE SKIP LOCKED`, idempotency, outbox claiming, projections, triggers,
and concurrency races; a single enclosing transaction would change what those
tests are proving.

### Considered alternative: truncate and reseed one package database

A middle design would keep one container and one mutable database per package,
then `TRUNCATE` every application table, reset sequences, and restore migration
seed data between test functions. The current suite has no general truncate or
schema-reset harness.

Reject that design for this change. It requires a complete, permanently updated
table/reset ordering, must distinguish canonical migration fixtures from test
rows, and can leak database-level state that table truncation does not restore:
DDL, sequences missed by enumeration, extensions, roles/grants, session or
database settings, `search_path`, triggers, and other schema objects. A clone of
the closed migrated template restores the entire database state without a
hand-maintained reset inventory. Reconsider truncate only if measured clone cost
dominates and a separately reviewed reset contract can prove equivalent
isolation.

## Required implementation shape

### 1. Preserve the simple caller contract

Prefer keeping the common call site close to:

```go
pool := pgtest.StartPostgres(t, ctx)
```

The helper may be renamed if a clearer API materially improves lifecycle
ownership, but avoid a repository-wide test rewrite merely for naming. Existing
callers should receive a clean migrated database and continue registering
test-local pool cleanup.

### 2. Package-scoped server lifecycle

Implement a concurrency-safe package-local harness that lazily starts one
container on first use. Initialization must be guarded (`sync.Once` or an
equivalent state machine), and initialization failures must be reported cleanly
to every caller rather than deadlocking later tests.

The package container must not be tied to the cleanup of the first `*testing.T`.
Use an explicit package/process cleanup mechanism. Evaluate these options and
choose the smallest reliable one:

- a `TestMain` integration owned by `pgtest` callers;
- a package harness registered from each package's `TestMain`; or
- a process-level cleanup registry with a documented fallback for interrupted
  runs.

Do not depend only on `os.Exit` defers; Go does not run them. Container names
must remain unique across concurrently running Go packages and separate `go
test` processes. Do not rely only on `time.Now().UnixNano()`: include the process
ID plus a cryptographically random suffix (or equivalent collision-resistant
process identity) in container, template, and cloned-database names.

There are currently 31 Go packages with `StartPostgres` callers. Commands such
as `go test ./...` can run multiple package test binaries concurrently, so the
live concurrency axis is package processes, not `t.Parallel()` within a package.
Today that already permits roughly one PostgreSQL container per active package;
package-scoped reuse does not increase that theoretical count, but it keeps each
container alive for the package's full test duration and may retain a template
plus a live clone longer. Benchmark peak container count, Docker memory, disk,
and wall time on the actual CI runner. If measured pressure is unsafe, cap the
relevant Go commands with an evidence-based `go test -p N`; do not guess a cap or
serialize the entire suite by default.

### 3. Migration-complete template database

Within the shared container:

1. create a dedicated template database;
2. reuse the existing `extractGooseUp` + filename-ordered concat-and-pipe path
   to apply all migration `Up` SQL to it exactly once;
3. close the migration connection completely before serving any clone request;
4. mark the template as disallowing connections after migration and never use
   it as an application/test pool target;
5. clone a uniquely named database for each test with
   `CREATE DATABASE ... TEMPLATE ...`.

The no-template-sessions rule is a hard invariant, not a best-effort cleanup.
PostgreSQL cannot clone a source database if another session is connected to it
when copying begins. Migration is the template's only connection phase. After
that phase, all clone/drop operations must execute through a neutral admin
connection; no long-lived pool may ever target the template database.

Do not reuse the container's current `POSTGRES_DB=goatos` database as both the
clone template and a live test database. Create a separately named internal
template database (for example, a collision-resistant package/process name with
an internal `_template` suffix), refactor the existing `psql` helper to target
that database during migration, close that connection, and then disallow future
connections to the template. Per-test clones receive their own names; the
maintenance/admin connection remains attached to `postgres`.

Database names are identifiers, not query parameters. Generate them internally
from safe characters and quote identifiers defensively. Do not derive raw SQL
identifiers directly from arbitrary test names.

The admin connection used to create/drop databases must connect to a neutral
database such as `postgres`, not to the database being dropped.

The current suite has no concurrent clone requests within a package because it
does not use `t.Parallel()`. A clone mutex/semaphore is therefore not required
for the first serial conversion. Before enabling intra-package parallel tests,
add a package-level one-slot clone guard whose critical section contains only
`CREATE DATABASE ... TEMPLATE ...`; test execution can remain parallel after
each clone exists.

### 4. Per-test cleanup under the current serial model

Each test receives an independent database even though current top-level tests
run serially. Cleanup must finish before the next test reuses package capacity:

1. close the test pool;
2. terminate remaining sessions for that test database, with bounded waiting;
3. drop the test database; and
4. surface cleanup failures without masking the original test failure.

Use collision-resistant database names. Multiple connections and goroutines
inside one test remain fully supported; this intra-test concurrency is distinct
from `t.Parallel()` across top-level tests.

### 5. Conditional gate before enabling `t.Parallel()`

Under the current serial package model, one test pool is active at a time, so
connection exhaustion across test pools is not a present blocker. Do not expand
this optimization's first phase by enabling `t.Parallel()`.

If a later change enables intra-package parallel tests, it must first add an
explicit connection budget. One shared container would then serve every
parallel test pool, and inheriting `pgxpool`'s host-derived default could exceed
PostgreSQL's `max_connections`.

- Parse the per-test pool config and set a small default `MaxConns` (start with
  4), `MinConns = 0`, and bounded idle/lifetime settings suitable for tests.
- Allow an explicit, reviewed per-test override for a test that proves it needs
  more concurrent sessions. Do not silently raise every pool for one test.
- Start the package container with an explicit `max_connections` value and
  reserve headroom for the admin connection, cleanup/termination work, and
  PostgreSQL-owned slots.
- Bound simultaneously active test databases/pools when the worst-case formula
  would exceed the server budget:

  ```text
  admin_and_cleanup_headroom + active_test_pools * per_pool_max_conns
      <= usable_server_connections
  ```

- Fail fast with a clear harness error when an override cannot fit the budget;
  do not let the suite degrade into intermittent `too many clients` failures.

At that future gate, benchmark with the chosen connection limit and parallelism.
Raising server `max_connections` can supplement the pool cap, but it is not a
substitute for bounding each pool and total active-pool concurrency.

### 6. Explicit escape hatch

Keep an explicit helper or option for a fully dedicated container when a test
genuinely validates:

- migration application/failure or startup behavior;
- database-level settings, extensions, roles, or grants that cannot be isolated
  in a cloned database;
- container restart/crash recovery; or
- any behavior proven to be unsafe under template cloning.

Do not classify ordinary event-driven, outbox, projection, trigger, advisory
lock, or `SKIP LOCKED` tests as dedicated-container tests merely because they use
concurrency. They should work with separate connections inside one isolated
cloned database.

## Implementation sequence

1. Benchmark and record the current harness on a small representative package
   and on one broad integration package: cold and warm-cache wall time,
   container starts, migration applications, peak concurrent containers, Docker
   memory, and disk use.
2. Add focused tests for the new harness lifecycle and database isolation.
3. Implement package-scoped container initialization and the migrated template,
   preserving the current extract/concatenate/single-`psql` migration semantics.
4. Implement per-test cloning, pool creation, cleanup, and unique naming.
5. Convert one representative serial package first. Prove isolation, failure
   cleanup, repeatability, and intra-test multi-connection behavior before
   changing all callers.
6. Convert the two centralized E2E fixture call sites, then migrate remaining
   `pgtest.StartPostgres` callers mechanically, preserving
   test semantics and fixtures.
7. Keep or introduce dedicated-container use only for documented exceptions.
8. Re-run the benchmark and record before/after evidence in the PR/handoff.

Do not add `t.Parallel()` as part of this optimization. Parallelizing tests is a
separate follow-up requiring the clone guard and connection-budget gate above.

## Tests the harness change itself must have

- Two tests can insert the same primary keys without seeing each other's rows.
- A committed write in one test database is invisible in another.
- Multiple concurrent connections within one test observe committed data and
  preserve lock/race semantics.
- The template rejects ordinary connections after migration.
- A failed test still releases its pool and database.
- Leaked connections are bounded/terminated during cleanup.
- A migration failure prevents clones from being served.
- Separate Go package processes do not collide on container or database names.
- A repeated test run leaves no anonymous Docker volumes or stale containers.

Conditional tests required only before introducing `t.Parallel()`:

- Parallel tests receive different database names and pools.
- Concurrent first callers start only one package container.
- Concurrent clone requests complete deterministically without template-session
  or template-copy failures.
- Peak parallel execution stays within the configured server connection budget,
  and an oversized pool override fails clearly.

## Verification gates

During iteration, run the focused harness tests and the converted package. The
final pre-push proof must not rely solely on tests reported by another agent.
Run against the final rebased commit:

1. the new harness regression tests;
2. every Go package whose tests or setup were changed;
3. representative event/concurrency suites covering outbox processing,
   idempotency, `SKIP LOCKED`, projection freshness, and cross-tenant isolation;
4. migration validation, build, scale, query-plan, and repository guardrail
   gates required by the current Makefile/CI; and
5. the broader CI integration suite.

Compare before/after results. Acceptance requires:

- materially fewer Docker container starts and migration applications;
- no loss of per-test database isolation;
- no new flakes under repeated serial package runs and the existing concurrent
  `go test ./...` package-process model;
- no template-source connection errors;
- no hidden test skips;
- no orphaned containers, databases, or volumes; and
- no weakening/removal of real-PostgreSQL event/concurrency coverage merely to
  make the suite faster.

If intra-package parallelism is later enabled, extend acceptance to repeated
parallel runs with no `too many clients`, clone-ordering, or cleanup failures.

### Go test cache boundary

The main `make test` and staging backend command currently use `go test ./...`
without `-count=1`, so successful unchanged package results are cache-eligible.
The optimization primarily benefits cold/fresh runs, changed packages, cache
misses, and commands that force execution. Do not report a warm cached no-op as
a harness benchmark.

The staging E2E and HRMS smoke commands explicitly use `-count=1`; those always
exercise the harness. The Pages E2E jobs and the checked-in local E2E run scripts
currently omit `-count=1`, so they are cache-eligible and must not be described
as universally forced runs. Record the exact command and cache mode beside every
before/after timing.

## Non-goals

- Replacing PostgreSQL tests with mocks.
- Reducing coverage because the suite is slow.
- Sharing seeded mutable state between tests.
- Rewriting migrations or production database code.
- Combining this harness optimization with unrelated defect-ledger, frontend,
  mobile, business-rule, or cloud work.

## First commands for the next session

```bash
cd /Users/ravi/mesha/goatos
git status --short
git rev-parse HEAD
rg -n "pgtest\.StartPostgres|StartPostgres\(" backend --glob '*_test.go'
find backend/migrations/postgres -name '*.sql' | wc -l
sed -n '1,220p' backend/internal/platform/pgtest/pgtest.go
```

Then inspect the current Makefile and CI targets before choosing exact
verification commands; do not copy stale gate names from this handoff.
