# Go and PostgreSQL Engineering Quality

This is the backend implementation and review baseline for Goat OS. It applies
to `backend/**/*.go`, `backend/internal/**/adapters/postgres/**`,
`backend/migrations/postgres/**`, and `backend/sqlc.yaml`.

Shared operational read models must follow
`docs/architecture/operational-read-model-contract.md`. Backend changes that
feed Calendar, Control Tower, Action Center, Protocol Adherence, Workflows,
admin-web detail pages, Android execution/proof screens, reporting, or a new
vertical/module must declare canonical write owner, row/summary grain, stable
scope identity, bucket disjointness/overlap, whole-result summary behavior, and
every consumer contract before implementation is complete.

The repository stack is intentionally narrow:

```text
Go 1.25.12 module and builder toolchain
net/http + chi transport
pgx/v5 + pgxpool
sqlc v1.29.0 generated query packages
goose-style plain SQL migrations
PostgreSQL 16.9
OpenTelemetry + slog
```

Context7 may be used to locate current library material, but it is only a
documentation index. Resolve every rule against the primary Go, pgx, sqlc, or
PostgreSQL documentation linked in [Primary sources](#primary-sources).

## Security baseline

Fail closed on reachable vulnerabilities. Run `govulncheck ./...`; do not hide
a reachable finding behind a permanent allowlist. A temporary exception needs
an owner, exploitability analysis, expiry date, and linked remediation issue.

The 2026-07-20 audit upgraded the module and both Docker builders to Go 1.25.12,
pgx to v5.9.2, otelpgx to v0.11.1, and the coherent OpenTelemetry graph to
v1.43.0 / contrib v0.68.0. The pinned `golang.org/x/vuln` v1.6.0 scan reports
zero reachable and zero imported-package vulnerabilities. Advisories in an
unused module-graph path are reported but are not equivalent to a reachable
finding; they remain upgrade inputs and must be re-evaluated by every scan.

Keep dependency upgrades reviewable: update the minimum Go version, build
image, direct dependencies, and transitive graph deliberately; run `go mod
tidy`, `go mod verify`, `govulncheck`, unit/race tests, sqlc checks, migrations,
and PostgreSQL query plans. Do not accept a blind `go get -u ./...` diff.
The required gate runs `govulncheck` from pinned `golang.org/x/vuln` v1.6.0;
never make a required CI result depend on an unpinned `@latest` install.

## Go request and worker rules

- Pass `context.Context` explicitly as the first argument through handler,
  app/service, port, and adapter calls. Never store it in a long-lived struct or
  replace a request/worker context with `context.Background()` inside the call
  chain.
- Call every `CancelFunc` on every path. Bound database, network, storage, and
  worker-stage work with the caller's deadline or a shorter child deadline.
- A goroutine must have an owner, cancellation path, bounded concurrency, and
  observed result. Prefer the existing worker supervisor or `errgroup`; never
  start fire-and-forget goroutines from handlers or repository methods.
- Wrap errors with operation context and `%w`; use `errors.Is` / `errors.As` for
  classification. Do not branch on error strings, log-and-swallow, or log the
  same error at every layer.
- Keep handlers as transport adapters and domain/app packages vendor-free.
  PostgreSQL, HTTP, cloud, telemetry, and queue types terminate at adapters and
  bootstrap wiring.
- Tests for parsers, cursors, idempotency fingerprints, and other untrusted
  structured inputs must include malformed/boundary cases. Add a Go fuzz target
  when a compact parser has meaningful input space that examples cannot cover.

## pgx and transaction rules

- Use the shared, bounded, traced `pgxpool.Pool`; do not create per-request
  connections or private pools. Startup must parse configuration, set a maximum
  pool size, establish a bounded connection, and ping before readiness. Close
  the pool during process shutdown and keep pool saturation metrics visible.
- Every query and command receives the operation context. Repository query
  timeouts are upper bounds, not a reason to discard an earlier caller
  deadline.
- A pgx `Begin` context only controls the `BEGIN` command; cancellation does not
  auto-rollback the transaction. Prefer `pgx.BeginFunc` / `BeginTxFunc` where
  they fit. Otherwise defer a rollback immediately, check every statement, and
  explicitly commit. A transaction must finish on every path.
- Manual `Query` iteration must close rows and check `rows.Err()` after the
  loop. Prefer sqlc-generated methods or pgx `CollectRows` / `ForEachRow` where
  they simplify ownership; those helpers close rows automatically.
- Keep transaction scope small and free of remote calls. State transition,
  idempotency record, owned read-model update, audit row, and outbox write commit
  atomically when the operation contract requires them.
- Parameterize values. Dynamic identifiers or SQL fragments must come from a
  closed, code-owned allowlist; never interpolate request, token, RFID, sort, or
  filter input into SQL text.

## PostgreSQL query and concurrency rules

- Tenant and applicable park/shed scope are mandatory predicates, not filters
  added after retrieval. RBAC and database scope must agree.
- Hot lists use deterministic keyset ordering and a bounded limit. Workers use
  bounded batches, a stable cursor/high-water mark, and forward-progress tests.
  Avoid OFFSET pagination and unbounded `SELECT`/`UPDATE`/`DELETE`.
- `FOR UPDATE SKIP LOCKED` is for queue-like multi-consumer claims only. It gives
  an inconsistent view and is not valid for user-facing truth. Pair it with a
  deterministic `ORDER BY`, bounded `LIMIT`, state predicate, and retry budget.
- Under PostgreSQL `READ COMMITTED`, separate statements may observe different
  snapshots. Express compare-and-transition atomically, lock/revalidate the
  decision rows, or use a stronger isolation level with whole-transaction retry
  for serialization failures.
- Prove hot query shapes against representative cardinality and current
  statistics. `EXPLAIN` on toy data is not evidence. Use `EXPLAIN (ANALYZE,
  BUFFERS)` only in a disposable database; wrap data-changing analysis in
  `BEGIN` / `ROLLBACK`.
- Keep indexes aligned with equality scope, range, and ordering predicates.
  Index presence alone is not proof; the required query-plan gate must exercise
  the production statement shape.

## Migration and sqlc rules

- Never edit generated `sqlc` Go or schema snapshot files. Change the canonical
  migration/query, regenerate with the pinned sqlc, then prove a clean diff.
- Run `sqlc vet` and `sqlc diff` on ordinary backend CI. Keep the existing
  Postgres-backed `make sqlc-check` for canonical migration-to-schema replay.
  `sqlc verify` requires sqlc Cloud state/token and must not be introduced
  silently; adopt it only through an explicit organization/security decision.
- Keep migrations sequential, reversible where the data contract permits, and
  replayable from an empty PostgreSQL 16 database. Never mutate an already
  applied migration.
- Treat `000001_goatos_clean_slate_baseline.sql` as immutable once any shared
  environment has applied it. If a column, index, constraint, seed contract, or
  backfill was missed, add the next numbered forward migration instead of
  editing the baseline. A changed applied file creates checksum drift and can
  stop STG/production migration jobs before the real repair migration runs.
- When repairing a schema gap found in STG, verify both facts before closing
  the incident: the new migration version is recorded in
  `public.goatos_schema_migrations`, and the live table shape/data matches the
  new contract. Do not rely on a clean-slate local replay alone.
- Add columns nullable/default-safe, backfill in resumable bounded chunks, then
  constrain in a later step. Set a bounded `lock_timeout` for production DDL.
- Build hot-table indexes with `CREATE INDEX CONCURRENTLY` in a goose
  `NO TRANSACTION` migration. Detect and repair invalid indexes after a failed
  build. Add large-table CHECK/FK constraints `NOT VALID`, then validate
  separately.
- Do not combine schema expansion, bulk backfill, constraint validation, and
  destructive cleanup into one lock-holding migration. Deployment must tolerate
  old and new binaries during the expand/migrate/contract window.

## Required verification

Run narrow tests first, then the affected release gates. These checks are safe
for local/disposable environments and must never target production data.

```bash
cd backend
go mod verify
go vet ./...
GOATOS_RUN_POSTGRES_TESTS=0 go test ./...
govulncheck ./...

# Required concurrency packages on each backend change; full ./... on a
# scheduled/explicit security job until CI capacity proves it is cheap enough.
GOATOS_RUN_POSTGRES_TESTS=0 go test -race \
  ./internal/platform/worker \
  ./internal/platform/postgres \
  ./internal/kernelstages \
  ./internal/domainconsumer/app \
  ./internal/outbox/adapters/postgres

sqlc vet -f sqlc.yaml
sqlc diff -f sqlc.yaml
```

From the repository root, retain the existing database gates:

```bash
make validate-hot-index-migrations
make validate-sqlc-plans
GOATOS_RUN_POSTGRES_TESTS=1 make sqlc-check
GOATOS_RUN_POSTGRES_TESTS=1 make validate-migrations
GOATOS_RUN_POSTGRES_TESTS=1 make ci-local JOB=backend
```

CI must also fail when any changed non-generated Go file is not `gofmt` clean.
Use a diff-scoped formatting check initially because the repository currently
contains pre-existing formatting drift; remove that debt rather than declaring
the whole-tree failure green.

## Primary sources

- Go: [release policy and current releases](https://go.dev/doc/devel/release),
  [security best practices](https://go.dev/doc/security/best-practices),
  [`context` package contract](https://pkg.go.dev/context),
  [race detector](https://go.dev/doc/articles/race_detector), and
  [Go vulnerability management](https://go.dev/doc/security/vuln/).
- pgx: [`pgx/v5` package and transaction contract](https://pkg.go.dev/github.com/jackc/pgx/v5)
  and [`pgxpool` lifecycle](https://pkg.go.dev/github.com/jackc/pgx/v5/pgxpool).
- sqlc: [CI/CD checks (`diff`, `vet`, `verify`)](https://docs.sqlc.dev/en/stable/howto/ci-cd.html)
  and [Go with pgx](https://docs.sqlc.dev/en/stable/guides/using-go-and-pgx.html).
- PostgreSQL 16: [concurrent indexes](https://www.postgresql.org/docs/16/sql-createindex.html),
  [`ALTER TABLE` and `NOT VALID`](https://www.postgresql.org/docs/16/sql-altertable.html),
  [EXPLAIN](https://www.postgresql.org/docs/16/using-explain.html),
  [transaction isolation](https://www.postgresql.org/docs/16/transaction-iso.html),
  [explicit locking](https://www.postgresql.org/docs/16/explicit-locking.html),
  and [`SELECT ... SKIP LOCKED`](https://www.postgresql.org/docs/16/sql-select.html).
