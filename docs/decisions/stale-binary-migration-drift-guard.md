# ADR: Stale-binary migration-drift guard (fail fast in both directions)

Status: ACCEPTED — implemented in `internal/platform/migrationguard`, wired into
`internal/bootstrap.NewAPI` (`cmd/api`). Applies to every backend binary/agent
touching `cmd/api` startup or Postgres readiness.

## Problem

A long-lived `go run ./cmd/api` process (started from one worktree, per
`tools/dev/run-local-stack-supervised.sh`) kept running while migrations
`000187`/`000188` dropped tables it still queried
(`vaccination_shed_projection_rows` and friends — the U7 projection cleanup,
see `docs/decisions/operational-kernel-5k-50k-scale-envelope.md`). The
database moved forward; the running binary did not. Every affected screen
returned 500/503 with no signal that the real cause was a stale binary, not a
code bug. Nothing compared the binary's migration ceiling against the
database's applied level, either at startup or while the process kept
running.

AGENTS.md already documents the *other* direction as a rule: "Do not start
local, staging, or production app code against a database that is behind
that build's migrations." That was pure human/process discipline — no code
enforced it either.

## Decision

`internal/platform/migrationguard.Check(dbVersion, binaryVersion)` is a small,
pure, unit-tested comparison: it returns a nil error only when the two
versions match, and a distinct, actionable error for **either** direction of
drift.

- **DB ahead of binary** (`DBAhead`) — the incident above. Error: "database
  migrated to NNN but this binary only knows MMM — rebuild the backend."
- **Binary ahead of DB** (`BinaryAhead`, includes a never-migrated database)
  — the AGENTS.md-documented direction, now enforced in code for the first
  time. Error: "database is at migration MMM but this binary requires NNN —
  apply pending migrations first (go run ./cmd/migrate)."

`binaryVersion` comes from `migrationguard.BinaryVersion()`, which reads the
highest version among `backend/migrations/postgres/*.sql` files **embedded
at build time** (`backend/migrations/embed.go`, `go:embed`). This is
necessary, not cosmetic: `backend/Dockerfile` never `COPY`s
`backend/migrations` into the production image, so a filesystem read would
silently see nothing there. `dbVersion` comes from
`migrationguard.AppliedVersion(ctx, pool)`, which reads the max
`goatos_schema_migrations.version` and normalizes it to the same bare
`"000188"` shape `BinaryVersion` returns — the column actually stores
`cmd/migrate`'s full filename stem (e.g.
`"000188_drop_process_integrity_projection_summaries"`), not the bare
number; a live end-to-end smoke test (`go run ./cmd/migrate` then
`go run ./cmd/api` against a throwaway Postgres) caught this before it
shipped. `AppliedVersion` returns `""`, not an error, when the table doesn't
exist yet — a never-migrated database.

### Wiring

- `internal/bootstrap.NewAPI` calls `Check` once, right after the pool
  connects and before any other setup. Any drift is a fatal startup error
  (logged via the `observability.New` instance logger passed into
  `NewAPI`, never `slog.New` directly — see `docs/decisions/observability.md`)
  and `cmd/api` exits non-zero. Equal versions proceed silently.
- `GET /readyz` re-runs `AppliedVersion` + `Check` on **every call**, not
  just at startup — this is the actual incident scenario, where the DB drifts
  ahead *while the process keeps running*. A launcher that reuses an
  already-healthy process (`tools/dev/run-local-stack-supervised.sh`'s
  `start_api`, which short-circuits when `/readyz` is already 2xx) will now
  see it go unhealthy instead of silently continuing to serve a stale
  binary.
- `GET /version` (new, public like the other health routes — see
  `httpmiddleware.isPublicHealthRoute`) reports `build_sha`,
  `binary_migration_version`, `db_migration_version`, and a
  `migration_drift` boolean + reason, so an operator or a bare `curl` can see
  staleness instantly without a token, even for a process that started
  before drift occurred and hasn't restarted.
- `internal/platform/buildinfo.SHA` carries the git commit, stamped via
  `-ldflags -X .../buildinfo.SHA=<sha>` in `backend/Dockerfile`'s `api` build
  step, with a `GOATOS_BUILD_SHA` runtime-env fallback
  (`buildinfo.Current()`) for anyone who wants to set it without a rebuild.
  **Deliberately not yet wired**: no CI/CD workflow or deploy script passes
  `--build-arg GIT_SHA=...` today, so a Docker-built image reports
  `build_sha: "unknown"` until that follow-up lands (flagged separately;
  touching `.github/workflows/stg-deploy.yml` /
  `tools/deploy/stg-clouddeploy-release.sh` is a deploy-pipeline change with
  its own review, not bundled into this guard).

### Why both directions are enforced now, not just the incident direction

The task authorizing this guard explicitly allows enforcing `BinaryAhead` too
("if no existing guard, a clear error is fine too"), and no runtime code
previously enforced it — only the AGENTS.md sentence did. Cloud Deploy
already applies migrations before updating Cloud Run services/jobs for every
release (`docs/runbooks/cloud-deploy-staging.md`), so in the normal deploy
path this direction should never trigger; when it does, it means the
sequencing broke (e.g. a failed migrate job that a release script proceeded
past anyway) — exactly the kind of silent failure this guard exists to
surface loudly instead of serving 500s against missing tables/columns.

## Consequences

- A binary can no longer start, nor stay marked ready, against a database it
  disagrees with in either direction. `equal` is the only silent-success
  case.
- `/version` makes staleness observable without waiting for a request to
  500 or for someone to notice `/readyz` degrade.
- The production image still does not carry a verified build SHA end to end
  until CI/CD is updated to pass `--build-arg GIT_SHA`; until then,
  `build_sha` reads `"unknown"` (or whatever `GOATOS_BUILD_SHA` an operator
  sets by hand) — a known, documented gap, not a silent one.
- `run-local-stack-supervised.sh` itself was intentionally NOT modified by
  this change (a different session had uncommitted local edits to it in a
  different worktree; the guard was designed against the committed `main`
  copy of that script). Its `start_api`'s reuse-if-healthy check now behaves
  correctly against the sharpened `/readyz`, without needing script changes.
