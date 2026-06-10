# GitHub Workflows

This runbook explains the GitHub Actions workflows in plain language.

If a new workflow is added, or an existing workflow changes, update this file in
the same style: what it does, when it runs, what machine/service it uses, and
what a failure usually means.

## Current Workflows

```text
.github/workflows/ci.yml
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

In plain language, it asks:

```text
Can the backend compile?
Do tests pass?
Are API contracts still valid?
Did we accidentally commit a huge/private file?
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
```

Runs on:

```text
ubuntu-latest
```

### Step 1: Checkout

GitHub downloads the repository into the temporary runner.

### Step 2: Setup Go

GitHub installs the Go version declared by:

```text
backend/go.mod
```

### Step 3: Agent Guardrails

Commands:

```text
bash tools/agent-hooks/check-boundaries.sh
bash tools/agent-hooks/check-contract-drift.sh
```

Purpose:

```text
Make sure agents did not edit across forbidden boundaries.
Make sure OpenAPI/JSON Schema/contracts/examples still validate.
```

If this fails, it usually means:

```text
contract drift
missing regenerated example/client artifact
boundary rule violation
```

### Step 4: Large File Guard

Purpose:

```text
Prevent accidental commits of private workbooks, media, dumps, or other huge files.
```

If this fails, inspect the listed file. Do not commit raw private RFID workbooks,
private source rows, media, or local data dumps.

### Step 5: Go Tests

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

### Step 6: Install Pinned sqlc

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

### Step 7: SQL And Migration Validation

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
Exercise migration invariants.
Reject whitespace errors.
```

If this fails, look at the exact sub-command:

```text
sqlc-check failed
  schema dump or generated sqlc drift

validate-sqlc-plans failed
  query no longer uses expected index
  new sqlc query lacks plan expectation

validate-migrations failed
  migration does not apply cleanly
  schema invariant test failed

git diff --check failed
  whitespace or conflict marker issue
```

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

Before pushing backend or workflow changes, run the narrowest relevant checks.

Common full guardrail set:

```text
make test
make check
PATH="/tmp/goatos-sqlc-bin:$PATH" make sqlc-check
make validate-sqlc-plans
make validate-migrations
git diff --check
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

## Maintenance Rule

When adding or changing workflows:

```text
1. Update this runbook.
2. Explain the workflow in plain language.
3. List when it runs.
4. List what external systems it starts or calls.
5. List common failure meanings.
6. Keep examples copy-pasteable.
```

Do not let workflow behavior live only in `.github/workflows/*.yml`.
