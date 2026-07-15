# GitHub Workflows

When hosted Actions cannot allocate jobs because of billing/platform state,
follow [Local CI Mirror](local-ci.md). The hosted workflows call the same
`make ci-local JOB=...` implementation, so local proof is command-identical.

This runbook explains the GitHub Actions workflows for project and engineering
review.

If a new workflow is added, or an existing workflow changes, update this file in
the same style: what it does, when it runs, what machine/service it uses, and
what a failure usually means.

## Current Workflows

```text
.github/workflows/ci.yml
.github/workflows/android-quality.yml
.github/workflows/pages.yml
.github/workflows/stg-pr-gate.yml
.github/workflows/stg-deploy.yml
```

This is the main CI guardrail workflow.

It runs on:

```text
push to main
pull_request
```

We usually push directly to `main`, so every pushed commit should pass this
workflow.

## What The CI Guardrail Is For

The CI guardrail makes sure a commit did not quietly break Goat OS.

The checks are:

```text
Can the backend compile?
Do tests pass?
Can admin-web lint, typecheck, pass mock-fidelity guards, build, and keep the bearer token out of the client bundle?
Are API contracts still valid?
Did we accidentally commit a huge/private file?
Did we accidentally add a new million-animal scale anti-pattern?
Can a brand-new Postgres database be built from our migrations?
Can sqlc regenerate typed DB code from that schema?
Do important SQL queries still use the expected indexes, including natural
planner proof for typed UUID-array predicates?
Do migration invariants still hold?
```

This is why even a README-only commit runs CI: GitHub does not know whether a
commit is "just docs" until it checks the repository state. Running the same
guardrail on every commit keeps `main` honest.

## Who Starts Postgres In CI?

GitHub does.

More precisely:

```text
1. We push to GitHub.
2. GitHub Actions starts a temporary Ubuntu runner.
3. Our CI job runs on that runner.
4. The CI scripts run Docker on that runner.
5. Docker starts a temporary Postgres container.
6. We apply Goat OS migrations into that temporary Postgres.
7. We run sqlc, query-plan, and migration checks.
8. When CI ends, the container is deleted.
```

The CI database is **not**:

```text
your laptop Postgres
production Postgres
staging Postgres
a shared database
```

It is a fresh disposable Postgres container created for that CI run.

Current pinned image:

```text
postgres:16.9-alpine
```

The image is controlled by:

```text
GOATOS_POSTGRES_IMAGE
GOATOS_SQLC_POSTGRES_IMAGE
```

## Workflow: `ci`

File:

```text
.github/workflows/ci.yml
```

Job:

```text
guardrails
admin-web
android
```

Runs on:

```text
ubuntu-latest
```

GitHub JavaScript actions are kept on Node 24-capable majors to avoid Node 20
runner deprecation warnings:

```text
actions/checkout@v7
actions/setup-go@v6
actions/setup-node@v6
```

### Step 1: Checkout

GitHub downloads the repository into the temporary runner.

### Step 2: Setup Go

GitHub installs the Go version declared by:

```text
backend/go.mod
```

### Step 3: Install Guardrail Tools

Commands:

```text
sudo apt-get update
sudo apt-get install -y ripgrep
rg --version
```

Purpose:

```text
Install ripgrep for boundary checks that scan secrets, direct data-source
access, forbidden logging patterns, and module boundary violations.
```

`check-boundaries.sh` fails closed when `rg` is missing. If this step fails, the
runner cannot enforce the full boundary guard; fix the package install before
trusting the CI result.

### Step 4: Agent Guardrails

Commands:

```text
bash tools/agent-hooks/check-boundaries.sh
bash tools/agent-hooks/check-contract-drift.sh
```

Purpose:

```text
Make sure agents did not edit across forbidden boundaries.
Make sure OpenAPI/JSON Schema/contracts/examples still validate.
Make sure generated OpenAPI TypeScript clients are regenerated.
Make sure admin-web does not reintroduce direct BigQuery, Google Sheets,
Apps Script, direct Sheet CSV export, or XLSX live-data access.
Block a changed SQL aggregate/projection unless it records canonical membership,
stable group identity, join-cardinality, pagination, and scope evidence and adds
the applicable fan-out/date/scope/status/page-boundary regression tests. The
aggregate-projection guard inspects committed, staged, unstaged, and untracked
work so a dirty checkout cannot report a false no-change pass.
Make sure rendered/admin-web-facing code uses Mesha visible branding instead of
old or internal product labels.
Make sure static deployment guards validate related Terraform/HCL fields within
the same block and include adversarial sibling-block self-tests.
```

The admin-web visible-branding guard is case-insensitive for rendered app,
component, feature, and server-facing error text so lowercase variants such as
`vgoat`, `goat os`, or `goat-os` cannot slip through. Internal identifiers such
as `GOATOS_*`, `@goatos/api-client`, `GoatOSApiError`, and the backend SOP form
schema ID `goatos.sop-form.v1` remain allowed.

The admin-web direct data-source pattern has a local self-test:

```text
bash tools/agent-hooks/check-boundaries.sh --self-test
```

If this fails, it usually means:

```text
contract drift
missing regenerated example/client artifact
boundary rule violation
admin-web direct data-source access instead of Goat OS backend API access
admin-web rendered/user-facing code using old or internal product labels instead
of Mesha visible branding
```

### Step 4b: Scale Anti-Pattern Guard

Command:

```text
make scale-guard
```

Purpose:

```text
Statically block the million-animal scale anti-patterns catalogued in
docs/decisions/scale-anti-patterns.md from re-entering main. Runs the
zero-dependency Go analyzer in tools/scale-guard over backend/internal/**.
```

It blocks these query shapes on request/worker paths: compute-on-read god-CTEs,
N+1 loops (a DB call inside a for/range), OFFSET pagination, whole-tenant
projection delete+reinsert, and non-sargable `lower(col) LIKE '%..%'`. It fails
only on NEW offenders — pre-existing debt is tracked in
`tools/scale-guard/baseline.txt` (a burn-down list; delete a line when the code
is fixed). A genuinely-bounded case may carry an inline
`// scale-guard:ignore: <reason>`.

If this fails, it usually means:

```text
a new read/list/dashboard query reconstructs state per request instead of
reading a projection; a DB call was placed inside a loop; OFFSET was used
instead of keyset pagination; a projection was rebuilt with a whole-tenant
delete+reinsert; or an unindexable lower()+LIKE search was added.
```

Note: this guard checks query SHAPE, not runtime cost. It does not replace the
plan/latency gates (Step 8), which today run at ~1k rows — a green run means
"correct shape", not "1M-proven".

### Step 4b: Mobile list-fetch anti-pattern guard

Command:

```text
make mobile-guard          # what CI runs (diff-scoped)
make mobile-guard-audit    # whole-tree backlog
```

Purpose:

```text
Block the mobile/web "fetch a whole list to render a screen" anti-patterns
catalogued in docs/decisions/mobile-data-fetch-anti-patterns.md. A phone shows
~10 rows; a screen must never pull 50/200/1000. Runs the Node analyzer
tools/agent-hooks/check-mobile-list-fetch.mjs over apps/goatos-android/**.
```

It blocks: a `limit = N` / `*_LIMIT` / `*_PAGE_LIMIT` / `*_PAGE_SIZE` above ~20
rows; `buildMonthDays`/`buildWeekDays` parsing event dates (the calendar overview
must render from backend day-markers/dots, not by fetching+parsing events); and
O(n^2) date scans (`.find { ... parseLocalDate }`).

It is **diff-scoped in CI** (`MOBILE_GUARD_BASE`, default `origin/main`): it only
scans mobile `.kt` files changed vs the base, so **a commit with no mobile code
passes instantly** — the guardrails job checks out with `fetch-depth: 0` so the
diff base is available. `make mobile-guard-audit` scans the whole tree to show the
current backlog. A genuinely-bounded case may carry an inline
`// mobile-guard:ignore: <reason>`.

### Step 4b: Admin-web full-table request-read guard

Command:

```text
make admin-web-request-reads-guard          # what CI runs (diff-scoped)
make admin-web-request-reads-guard-audit    # whole-tree backlog
```

Purpose:

```text
Block the Next.js SSR full-table request-read anti-pattern in apps/admin-web
catalogued in docs/decisions/scale-anti-patterns.md — a server data helper that
drains a paginated backend endpoint cursor-by-cursor into one big array to
compute a KPI on the request path (the searchAllGoats full-herd walk removed in
810bc1b3). Runs tools/agent-hooks/check-admin-web-request-reads.mjs.
```

It blocks the `cursor-drain-loop` shape: a `for`/`while` whose body both
accumulates (`.push(...)` / `.concat`) and advances a cursor from `next_cursor`.
The fix is a projection/summary endpoint that returns pre-aggregated counts. It is
**diff-scoped** (a commit with no admin-web TS passes instantly), skips
`'use client'` modules and test/mock/seed files, and honors an inline
`// scale-guard:ignore: <reason>`. The `Omit<Params, "limit" | "cursor">`
signature alone is NOT flagged — that is also the shape of a legitimate summary
reader.

### Step 4b: Android bounded-memory guard

Command:

```text
make android-bounded-memory-guard           # what CI runs (diff-scoped)
make android-bounded-memory-guard-audit     # whole-tree backlog
```

Purpose:

```text
Block unbounded in-memory growth in the Android data layer catalogued in
docs/decisions/mobile-data-fetch-anti-patterns.md — an in-heap cache/accumulator
with no cap/TTL/eviction, or a DAO reading a whole table into memory (commits
7058fff2 + d58acac2). Runs tools/agent-hooks/check-android-bounded-memory.mjs.
```

It blocks `unbounded-inmemory-collection` (a class-field mutable map/list/set never
evicted from — use `LruCache` or a Room `JsonBlobCacheDao` with TTL + cap) and
`whole-table-read` (`SELECT * FROM t` with no WHERE and no LIMIT — filter to active
rows or bound with LIMIT). It is distinct from `mobile-guard`, which owns fetch/page
SIZE. It is **diff-scoped** (a commit with no android Kotlin passes instantly),
skips test/entity/dto files, and honors an inline `// mobile-guard:ignore: <reason>`.

If this fails, it usually means:

```text
a mobile screen fetches more than one keyset page (~20) of rows; a calendar
overview parses events to draw its dots instead of consuming day-markers; or a
list transform re-parses every event inside .find/.any (O(n^2)).
```

### Step 4b-3: Room migration-safety guard

Command:

```text
make room-migration-guard          # what CI runs (self-test + diff-scoped)
make room-migration-guard-audit    # whole-tree audit of every @Database
```

Purpose:

```text
Block the Room-migration crash anti-pattern catalogued in
docs/decisions/room-migration-safety.md — an @Entity added to an Android
@Database with no migration to CREATE its table (the roster_timetable_cache /
roster_coverage_cache defect). Fresh installs work via Room's createAllTables;
every in-place upgrade of an already-installed APK crashes on open. Runs
tools/agent-hooks/check-room-migration-safety.mjs.
```

It blocks `export-schema-off` (exportSchema must be `true` so migrations can be
schema-validated), `missing-golden-schema` / `schema-not-committed` (a git-tracked
`schemas/<db>/<version>.json` must exist for the declared version — present locally is
not enough, CI/fresh clones need it committed), `version-bump-without-migration` (a
`version` rise with no matching `Migration(N-1, N)`), `entity-without-migration` (a
table new in schema `vK+1` that `Migration(K, K+1)` does not `CREATE` — the roster
defect), and `missing-migration-test` (every @Database module must ship a
`*MigrationTest` AND a `*UpgradeCrashTest`).
It is **diff-scoped** (a commit touching no Room DB/migration/schema passes instantly)
and honors an inline `room-migration-guard:ignore: <reason>` on the `@Database`
version/exportSchema line.

If this fails, it usually means an `@Entity` was added to a `@Database` without a
migration to create its table, `exportSchema` was left off, or a version bump forgot
its migration. Add the migration + a `*MigrationTest` / `*UpgradeCrashTest`.

### Step 4b-2: Telemetry guardrail

Command:

```text
make telemetry-guard          # what CI runs (diff-scoped)
make telemetry-guard-audit    # whole-tree backlog
```

Purpose:

```text
Enforce docs/observability/TELEMETRY_GUARDRAILS.md: every new or changed
user-facing surface (Android screen/viewmodel, admin-web route) must wire
Firebase Analytics event(s), Crashlytics fatal+non-fatal logging on error
paths, and the relevant funnel/journey step. Runs the stdlib-only Python
analyzer tools/telemetry-guard/telemetry-guard.py.
```

Two surfaces, both configurable (markers, globs, block-vs-warn) in
`tools/telemetry-guard/config.json`:

```text
android (mode: block)    -> ADDED/MODIFIED apps/goatos-android/**/*Screen.kt or
                             **/*ViewModel.kt, plus ADDED files under a
                             feature-*/features/ package, must reference an
                             AnalyticsPort/AnalyticsEvents/Crashlytics-family
                             marker (file or sibling .kt in the same dir).
admin_web (mode: warn)   -> ADDED apps/admin-web/app/**/page.tsx or
                             **/*Screen.tsx must reference a faro/trackEvent/
                             pushEvent/ErrorBoundary marker. Non-blocking today
                             — most existing routes predate Faro rollout; see
                             TELEMETRY_GUARDRAILS.md §3.2/§7 for the backlog.
```

It is **diff-scoped in CI** (`TELEMETRY_GUARD_BASE`, default `origin/main`,
set alongside `MOBILE_GUARD_BASE`): it only scans files changed vs the base,
so **a commit touching neither Android nor admin-web passes instantly** — the
guardrails job checks out with `fetch-depth: 0` so the diff base is available.
`make telemetry-guard-audit` scans the whole tree to show the current
backlog. Escape hatch: `// telemetry:exempt <reason>` anywhere in the file.

If this fails, it usually means:

```text
a new/changed Android screen or viewmodel has no AnalyticsPort/AnalyticsEvents
call and no telemetry:exempt comment, in that file or a sibling in the same
feature directory.
```

### Step 4c: Contract-integrity guardrails

Six machine guards enforce previously prose-only AGENTS.md "Do:" rules whose
violations shipped as real ledger bugs. Each is a zero-dependency Node guard with
an embedded `--self-test`. Five use a `*-baseline.txt` ratchet for existing debt;
the aggregate/projection evidence guard is diff-scoped and has no grandfathered
waiver.

```text
idempotency-writes-guard     -> mutating write paths must not rely on the
                                insufficient `ON CONFLICT DO UPDATE SET
                                idempotency_key = EXCLUDED.idempotency_key`
                                alone (idempotency write-path contract).
atomic-readmodel-sync-guard  -> Publish*/Finalize* that upsert an owned derived
                                read model must have a rollback regression test
                                (atomic state-transition + read-model sync).
config-validate-guard        -> authored config out-of-range must reject, not be
                                silently clamped/defaulted (validate-or-reject).
india-date-guard             -> UTC must never define a Goat OS business day;
                                day/date buckets convert to Asia/Kolkata first
                                (FIXCHK-002 class).
offline-first-guard          -> Android screen-facing read repositories must be
                                Room-backed offline-first, not network-only
                                api.xxx() pass-throughs (C35-001/019, MOB-007).
aggregate-projection-guard   -> changed JOIN + aggregate projections must state
                                membership/key/cardinality/page/scope proof and
                                add applicable fan-out, date-shift, hierarchy,
                                status-matrix, and page-boundary tests.
```

Each runs in `make guardrails` (so `make ci-local JOB=guardrails` and the `ci`
guardrails job run them). A genuinely-bounded case carries an inline
`<guard>:ignore: <reason>`; otherwise fix the code or, for pre-existing debt,
add it to that guard's baseline with a burn-down note.

### Step 5: Large File Guard

Purpose:

```text
Prevent accidental commits of private workbooks, media, dumps, or other huge files.
```

The guard checks Git-tracked files only. It intentionally ignores untracked
runtime artifacts created during CI, such as `node_modules`, while still
catching large files committed to the repository.

If this fails, inspect the listed tracked file. Do not commit raw private RFID
workbooks, private source rows, media, or local data dumps.

### Step 6: Go Tests

Command:

```text
cd backend
GOATOS_REQUIRE_DOCKER=1 go test ./...
```

`GOATOS_REQUIRE_DOCKER=1` (set by `run-local-ci.sh` on this step) makes
`pgtest.SkipIfNoDocker` FAIL instead of skip when docker is missing, so the
required Postgres/E2E integration gate can never false-green by silently
skipping — a runner without docker turns the build red rather than green.

Purpose:

```text
Compile all backend packages.
Run unit and integration tests.
Exercise Docker-backed Postgres tests where available.
```

If this fails, the backend code or tests are broken.

### Step 7: Install Pinned sqlc

Command:

```text
go install github.com/sqlc-dev/sqlc/cmd/sqlc@"$(cat tools/sqlc/sqlc.version)"
```

Purpose:

```text
Use the exact sqlc version expected by the repo.
Avoid "works on my machine" generated-code drift.
```

If this fails, GitHub could not install the pinned sqlc version.

### Step 8: SQL And Migration Validation

Commands:

```text
make sqlc-check
make validate-sqlc-plans
make validate-migrations
git diff --check
```

Purpose:

```text
Start temporary Postgres.
Apply all migrations.
Dump schema for sqlc.
Regenerate sqlc code.
Assert generated code matches committed code.
Validate important SQL query plans.
Validate hand-written hot-path plans such as outbox claim, auth grant lookup,
identity lookup/timeline, obligation due-window and scope rollups, target/open
obligation lookups, inventory FEFO/ledger, vaccination generation keyset, the
SOP verify fan-out by task, vaccination verification queue, and feed review/
shed-history reads.
Exercise migration invariants.
Reject whitespace errors.
```

The no-Sort plan guard rejects actual `Sort` / `Incremental Sort` plan nodes;
partitioned timeline reads may still show `Merge Append` `Sort Key` metadata
while remaining index-backed and acceptable.

If this fails, look at the exact sub-command:

```text
sqlc-check failed
  schema dump or generated sqlc drift

validate-sqlc-plans failed
  query no longer uses expected index
  new sqlc query lacks plan expectation
  hot read query introduced an avoidable Sort
  obligation/vaccination/feed process-integrity query lost its indexed access path

validate-migrations failed
  migration does not apply cleanly
  schema invariant test failed

git diff --check failed
  whitespace or conflict marker issue
```

## Job: `admin-web`

Runs on:

```text
ubuntu-latest
```

This job makes sure the Mesha admin-web frontend still compiles and keeps server
secrets out of browser artifacts. It does not start a local backend or run the
live screenshot smoke; that visual smoke remains a local pre-push requirement
for frontend work because it needs a seeded backend/admin-web environment and
real route data.

### Step 1: Checkout

GitHub downloads the repository into the temporary runner.

### Step 2: Setup Node

GitHub installs Node 24 and enables npm caching using:

```text
apps/admin-web/package-lock.json
```

### Step 3: Install Admin-Web Dependencies

Command:

```text
npm --prefix apps/admin-web ci
```

Purpose:

```text
Install the exact dependency graph from package-lock.json, including the
generated @goatos/api-client file dependency.
```

### Step 4: Admin-Web Lint

Command:

```text
npm --prefix apps/admin-web run lint
```

Purpose:

```text
Catch TypeScript/React/Next lint failures in the admin surface.
```

### Step 5: Admin-Web Typecheck

Command:

```text
npm --prefix apps/admin-web run typecheck
```

Purpose:

```text
Catch generated-client, server-action, route, and component type drift.
```

### Step 6: Admin-Web Mock Fidelity

Command:

```text
npm --prefix apps/admin-web run check:mock-fidelity
```

Purpose:

```text
Run the admin-web IA guard, UI contract literal guard, and mock-fidelity guard
so the active screens stay aligned with the Goat OS dashboard mock and generated
backend-owned contracts.
```

This is a static guard, not full visual proof. Frontend code changes still need
the local screenshot/layout/a11y smoke plus human screenshot review before push.

### Step 7: Admin-Web Build And Token Leak Guard

Command:

```text
GOATOS_BEARER_TOKEN=sentinel-mesha-admin-token npm --prefix apps/admin-web run build
```

Purpose:

```text
Build the Next app and run the post-build token leak guard. The sentinel token
must not appear in client/static output.
```

If this fails, inspect whether the frontend no longer builds or whether a
server-only bearer token leaked into browser code/static assets. This job is not
visual QA; frontend code changes still need the local screenshot/layout/a11y
smoke plus human screenshot review before push.

## Job: `android`

Runs on:

```text
ubuntu-latest
```

This job makes sure the Goat OS Android app (`apps/goatos-android`) still
compiles and passes its JVM unit tests on every PR and every push to `main` —
not only when an Android file changes. It closes ledger finding **C35-008**
(Android changes could previously merge without a required compile/unit gate in
`ci.yml`; the compile+unit gate only lived in the path-triggered
`android-quality.yml`). This job is required on all PRs so a backend or contract
change that breaks the Android client is caught here too.

### Step 1: Checkout

GitHub downloads the repository into the temporary runner.

### Step 2: Setup Java

GitHub installs Temurin JDK 21 (which runs Gradle/AGP; app bytecode
compatibility stays Java/Kotlin 17):

```text
actions/setup-java@v4  (distribution: temurin, java-version: 21)
```

The `ubuntu-latest` runner image ships the Android SDK and presets
`ANDROID_HOME`/`ANDROID_SDK_ROOT`, which `tools/ci/run-local-ci.sh`'s `android`
job consumes. No USB device or emulator is required for compile + unit.

### Step 3: Required Android Gates

Command:

```text
make ci-local JOB=android
```

Purpose:

```text
Run the exact same android job used locally: :app compile
(compileStgReleaseKotlin) + :app unit tests (testStgReleaseUnitTest).
```

If this fails, inspect the Gradle output for a Kotlin compile break or a failing
unit test. Because the same `make ci-local JOB=android` runs locally, reproduce
and fix it with `make android-doctor` (resolves the pinned JDK/SDK) then
`make ci-local JOB=android` before re-pushing.

## Temporary Postgres Startup Hardening

The CI scripts use:

```text
tools/postgres-ci.sh
```

This helper exists because `pg_isready` alone can be too optimistic.

The old behavior was:

```text
Start Postgres.
Ask pg_isready if Postgres is ready.
Immediately run psql/pg_dump.
```

Sometimes GitHub's temporary Postgres was still starting or briefly shutting
down, so CI failed with messages like:

```text
FATAL: the database system is shutting down
```

The current behavior is:

```text
Start Postgres.
Wait until Postgres can run a real SELECT 1 query.
Retry transient startup/shutdown connection errors.
Retry pg_dump transient connection errors.
Print Docker Postgres logs if it still fails.
```

This keeps the guardrail strict while reducing random CI flakes.

## Local Equivalents

Before committing, run the narrowest relevant checks for the files changed.

Backend, schema, sqlc, migration, CI, and workflow changes should run the
affected build/test/validation commands locally before commit. Documentation-only
changes do not need the full database validation suite every time, but they
should still run whitespace and any relevant agent/contract guardrails.

Common full guardrail set:

```text
make test
make check
PATH="/tmp/goatos-sqlc-bin:$PATH" make sqlc-check
make validate-sqlc-plans
make validate-migrations
git diff --check
```

Admin-web CI-equivalent set:

```text
npm --prefix apps/admin-web ci
npm --prefix apps/admin-web run lint
npm --prefix apps/admin-web run typecheck
npm --prefix apps/admin-web run check:mock-fidelity
GOATOS_BEARER_TOKEN=sentinel-mesha-admin-token npm --prefix apps/admin-web run build
```

For docs-only changes:

```text
./tools/agent-hooks/check-contract-drift.sh
git diff --check
```

If the docs change agent/build behavior, also run:

```text
./tools/agent-hooks/check-boundaries.sh
```

## android-quality.yml

Runs on `pull_request` and `push to main` when `apps/goatos-android/**`,
`tools/android/**`, or the workflow file itself changes.

One job, `design-system-guard`: installs ripgrep and runs
`tools/android/check-no-hardcoded-design.sh`, which fails the build if any
`Color(0x…)` literal or inline `TextStyle(…)` shows up in app/feature UI code
— color and type must come from `MeshaColors`/`MeshaType` only. A failure here
means a change introduced a hardcoded design value instead of using (or
adding, if genuinely missing) a design-system token.

## pages.yml

Publishes CI and E2E report categories to GitHub Pages as one combined site:

```text
/screenshot-gallery/  every mobile screen, rendered fresh via Paparazzi
/nav-graph/           Compose Navigation routes + navigate() edges (Mermaid)
/e2e-report/          vaccination kernel-story E2E report (story count is read
                      from the generated report; currently 38 production-path
                      stories with direct derived-state seeding forbidden)
/e2e-hrms-report/     HRMS roster/RBAC kernel-story E2E report
/scale-audit-e2e-report/
                      scale-audit fix E2E report BOUND to current-SHA latency
                      gates; shows VERIFIED only when gates pass, UNVERIFIED
                      when gates have not run, FAILED when gates fail
```

Runs on:

```text
push to main touching apps/goatos-android/**, backend/tests/e2e/**,
  backend/internal/**, scale/perf/E2E report inputs, the nav-graph/gallery
  generator scripts, tools/ci/**, context/execution/**, AGENTS.md, or this
  workflow file
a daily cron at 03:00 UTC (the screenshot gallery and E2E report are meant
  to stay fresh even with no code change that day)
workflow_dispatch (manual run from the Actions tab)
```

Six report jobs plus one publisher:

```text
mobile-screenshots  installs a JDK + the Android SDK platform for
                     compileSdk 36, runs
                     ./gradlew :app:recordPaparazziDevDebug (fresh render,
                     not a verify-only check), then
                     tools/android/build-screenshot-gallery.py
nav-graph            tools/android/generate-nav-graph.py — no Android build
                     needed, this one is fast
e2e-report           starts Docker-based ephemeral Postgres (same pgtest
                     harness the backend integration tests use) via
                     go test ./backend/tests/e2e/... -run TestKernelStor -v
e2e-hrms-report      runs the HRMS roster/RBAC E2E harness and publishes the
                     generated story report
scale-audit-e2e-report
                     uses tools/ci/generate-scale-audit-report.py to render
                     context/execution/scale-audit-fix-e2e-report-2026-07-11.md
                     into a Pages category, binding the report to current-SHA
                     latency gate artifacts (when/if they exist). Report shows:
                     - UNVERIFIED (yellow) when no gates have run
                     - VERIFIED (green) when all gates pass
                     - FAILED (red) when any gate fails
publish              downloads all report artifacts, assembles _site/ with a
                     linking index page, and deploys via actions/deploy-pages
```

### Scale Audit E2E Report Certification

The scale-audit-e2e-report job generates an HTML report that binds the
scale-audit findings (local/backend E2E proof) to the results of the latency
gates (api-latency-gate and process-integrity-latency-check) for the same
commit SHA.

**Certification states:**

- **UNVERIFIED** (yellow): No latency gate artifacts found for this SHA.
  The report explains the gates have not run. The markdown report content is
  still published, but the report footer indicates the SHA is not certified.

- **VERIFIED** (green): Latency gates ran and all passed for this SHA. The
  report embeds gate results (p95 latency, check names, pass/fail status) and
  shows the report as certified.

- **FAILED** (red): Latency gates ran but at least one failed. The report
  shows which endpoints/checks failed and does not certify the SHA.

**Current behavior (July 2026):**

Today the latency gates run only during staging certification, not on every
CI push. So main branch scale-audit reports will show UNVERIFIED. This is
correct: the report is bound to the real gates, not to a static markdown file.

**Future behavior (when gates are automated):**

When latency gates are wired into CI (a future feature), the scale-audit job
will consume their outputs and the report will automatically show VERIFIED or
FAILED for each commit.

**One-time repo setting required**: Settings -> Pages -> Build and
deployment -> Source must be set to **"GitHub Actions"** (not "Deploy from a
branch"). Until that is set, the report-building jobs still succeed and
their artifacts are downloadable from the run's Summary/Artifacts panel, but
the `publish` job fails at the `actions/deploy-pages` step because there is no
configured Pages environment to deploy into.

**Common failures:**

- `scale-audit-e2e-report` fails: Check whether the markdown source file
  moved or was deleted. Verify
  `context/execution/scale-audit-fix-e2e-report-2026-07-11.md` exists, and
  `tools/ci/generate-scale-audit-report.py` exists and is executable.
- Report shows UNVERIFIED: Gates have not run for this SHA. This is expected
  on main until gates are automated in CI.
- Report shows FAILED: A latency gate exceeded its threshold. Investigate
  the specific endpoint/check that failed and optimize the code path.
- `A failure in `mobile-screenshots` almost always means a Paparazzi golden
  mismatch (a real visual regression) or an Android SDK/AGP version drift on
  the runner — check the uploaded `screenshot-gallery` artifact and the
  `recordPaparazziDevDebug` log first. A failure in `e2e-report` means one of
  the kernel-story assertions broke — read the failing `story.Assert`
  message, it is written to explain the business expectation in plain English,
  not just the SQL/Go that checked it.

## stg-pr-gate.yml

Runs only for pull requests whose base branch is `stg`.

The first job fails closed unless the PR is exactly:

```text
vgoats/goatos main -> stg
```

This workflow does not run for feature branches into `main`.
No local ref may be pushed directly to remote `stg`; `make ai-setup` installs a
pre-push block for humans and agents, and the committed agent hooks reject the
same command before shell execution.

Jobs:

```text
route                verifies this is a main -> stg PR
backend-db-api       runs guardrails, aggregate/projection evidence, scale guard,
                     go test ./..., sqlc,
                     query plans, hot-index migration validation, migration
                     validation, vaccination kernel E2E, and HRMS roster/RBAC
                     E2E against disposable Postgres
admin-web            runs npm ci, lint, typecheck, mock fidelity, and Next
                     production build with token-leak guard
android-release-apk  builds :app:assembleStgRelease, runs
                     :app:testStgReleaseUnitTest, runs
                     :app:verifyPaparazziStgRelease when the Android build
                     exposes it, verifies the APK contains AndroidManifest.xml,
                     and uploads the release APK artifact
stg-pr-gate          aggregate success marker
```

The Android gate intentionally uses the staging release variant, not
`stgDebug`, because release Compose/runtime behavior is the one that matters for
staging handoff.

Common failures:

```text
route failed
  PR is not main -> stg.

backend-db-api failed
  migrations, DB-backed APIs, sqlc/query plans, or kernel story behavior broke.

admin-web failed
  generated-client/admin surface drift, mock-fidelity drift, or build/token leak.

android-release-apk failed
  the stg release APK did not compile, release unit tests failed, release render
  checks failed, or no structurally valid APK was produced.
```

## stg-deploy.yml

Runs on:

```text
push to stg
workflow_dispatch
```

Before cloud authentication, image builds, or release creation, the workflow
queries GitHub and fails closed unless all of these are true:

```text
ref is exactly refs/heads/stg
SHA is exactly the merge commit of a closed, merged pull request
PR base is vgoats/goatos:stg
PR head is vgoats/goatos:main
```

A direct push from `HEAD`, local `stg`, `main`, or an agent branch therefore
cannot deploy. `workflow_dispatch` is not a bypass: it can only rerun the exact
current `stg` SHA that GitHub already produced by merging `main -> stg`.

Authentication uses GitHub OIDC with repo variables:

```text
GOATOS_STG_WIF_PROVIDER
GOATOS_STG_DEPLOYER_SERVICE_ACCOUNT
```

The deployer is
`goatos-github-deploy-stg@goatos-stg.iam.gserviceaccount.com`. It can write to
the staging Artifact Registry, update/execute Cloud Run, and act as the staging
runtime service accounts. There is no committed service-account key.

Deploy flow:

```text
1. Verify the exact SHA came from a merged same-repo main -> stg PR
2. Build and push backend:<git-sha-12> with --build-arg GIT_SHA=${{ github.sha }}
3. Build and push migrate:<git-sha-12> with --build-arg GIT_SHA=${{ github.sha }}
4. Build and push admin-web:<git-sha-12>
5. Create the Cloud Deploy release
6. Cloud Deploy updates migration/API/jobs/admin-web in the documented order
7. Cloud Deploy verifies image skew and smokes the API/dashboard
```

The `GIT_SHA` build-arg stamps the exact commit SHA into the backend api binary
via ldflags (`-X github.com/vgoats/goatos/backend/internal/platform/buildinfo.SHA`).
This allows the `/version` endpoint to report the real commit SHA instead of the
default `unknown` value. See `docs/decisions/stale-binary-migration-drift-guard.md`
for context. The migration image receives the same `GIT_SHA` for consistency, though
it is not currently used in the Dockerfile.migrate build.

The workflow skips a push only when the head commit message contains:

```text
[skip stg deploy]
```

Use that only for a merged `main -> stg` commit that intentionally should not
deploy. It does not authorize a direct branch push or arbitrary SHA. Normal
`main -> stg` PR merges must deploy automatically.

Local and CI guard test:

```bash
make stg-promotion-guard
make stg-promotion-guard-install
```

The first command validates command/ref rejection and committed workflow/hook
wiring. The second idempotently installs the machine-local pre-push block.

## Maintenance Rule

When adding or changing workflows:

```text
1. Update this runbook.
2. Explain the workflow clearly for project and engineering review.
3. List when it runs.
4. List what external systems it starts or calls.
5. List common failure meanings.
6. Keep examples copy-pasteable.
```

Do not let workflow behavior live only in `.github/workflows/*.yml`.

When any feature, fix, audit, or scale gate generates an E2E result, commit the
report and wire it into this Pages report site before handoff. The report must
be visible as a card/list item on the root CI reports index
(`https://vgoats.github.io/goatos/`), not only reachable through a standalone
URL. Reuse an existing category when it fits: for example, vaccination kernel
stories belong inside `/e2e-report/`, HRMS roster/RBAC stories belong inside
`/e2e-hrms-report/`, and mobile visual E2E proof belongs inside
`/screenshot-gallery/`. Do not add a second root card for a test that is part of
an existing category. Add a new Pages category and root card only when the E2E
result is a genuinely new report family; document whether that result is
local-only, staging certification, or production proof.

Every E2E detail page must match the existing report style: self-contained HTML,
title/subtitle, summary tiles, pass/fail/pending badges, sections/steps, and
readable code/evidence blocks. Each story/check must explain, in prose a
reviewer can understand without opening the code, all of:

```text
what behavior is under test
why it matters operationally or at scale
what data/setup creates the scenario
what action/trigger/API/job is executed
what assertions prove pass/fail
where the source evidence lives
what is out of scope or still uncertified
```

Do not ship one-line headings, raw markdown, a bare `<pre>` page,
screenshots-only proof, an Actions artifact, a local scratchpad, or a chat paste
as the final E2E report. Before declaring the report published, verify both the
root card and the detail page with `curl`; when GitHub Pages cache is stale, use
a `?v=<commit-sha>` cache-busting URL in the handoff evidence.
