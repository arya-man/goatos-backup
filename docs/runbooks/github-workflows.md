# GitHub Workflows

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
Do important SQL queries still use the expected indexes?
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
Make sure rendered/admin-web-facing code uses Mesha visible branding instead of
old or internal product labels.
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
go test ./...
```

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
                      scale-audit fix E2E report plus the staging-certification
                      boundary for the 1M gate
```

Runs on:

```text
push to main touching apps/goatos-android/**, backend/tests/e2e/**,
  backend/internal/**, scale/perf/E2E report inputs, the nav-graph/gallery
  generator scripts, AGENTS.md, or this workflow file
a daily cron at 03:00 UTC (the screenshot gallery and E2E report are meant
  to stay fresh even with no code change that day)
workflow_dispatch (manual run from the Actions tab)
```

Five report jobs plus one publisher:

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
                     renders context/execution/scale-audit-fix-e2e-report-
                     2026-07-11.md into a Pages category so local E2E proof
                     and certification boundaries are visible in GitHub
publish              downloads all report artifacts, assembles _site/ with a
                     linking index page, and deploys via actions/deploy-pages
```

**One-time repo setting required**: Settings -> Pages -> Build and
deployment -> Source must be set to **"GitHub Actions"** (not "Deploy from a
branch"). Until that is set, the report-building jobs still succeed and
their artifacts are downloadable from the run's Summary/Artifacts panel, but
the `publish` job fails at the `actions/deploy-pages` step because there is no
configured Pages environment to deploy into.

A failure in `mobile-screenshots` almost always means a Paparazzi golden
mismatch (a real visual regression) or an Android SDK/AGP version drift on
the runner — check the uploaded `screenshot-gallery` artifact and the
`recordPaparazziDevDebug` log first. A failure in `e2e-report` means one of
the kernel-story assertions broke — read the failing `story.Assert`
message, it is written to explain the business expectation in plain English,
not just the SQL/Go that checked it. A failure in `scale-audit-e2e-report`
usually means the committed markdown report moved or was deleted without
updating the Pages category.

## stg-pr-gate.yml

Runs only for pull requests whose base branch is `stg`.

The first job fails closed unless the PR is exactly:

```text
vgoats/goatos main -> stg
```

This workflow does not run for feature branches into `main`.

Jobs:

```text
route                verifies this is a main -> stg PR
backend-db-api       runs guardrails, scale guard, go test ./..., sqlc,
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
1. Build and push backend:<git-sha-12>
2. Build and push migrate:<git-sha-12>
3. Build and push admin-web:<git-sha-12>
4. Update goatos-stg-migrate and execute it with --wait
5. Update goatos-api-stg
6. Update scheduled backend Cloud Run jobs
7. Update goatos-admin-web-stg
8. Smoke https://goatos-api-stg-awtrpmn4za-el.a.run.app/livez -> 204
9. Smoke https://goatos-api-stg-awtrpmn4za-el.a.run.app/readyz -> 204
10. Smoke https://stg.dashboard.mesha.sg/login
```

The workflow skips a push only when the head commit message contains:

```text
[skip stg deploy]
```

Use that only for branch bootstrapping or workflow-only seeding. Normal
`main -> stg` PR merges must deploy automatically.

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
