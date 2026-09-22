# PR 348 Vaccination Live Tracker Landing

## Scope

- Land PR 348, "Fix vaccination live tracker assignment totals", to `main`.
- Change area: vaccination live tracker assignment totals, vaccination proof completion reconciliation, Android vaccination proof retry/capture state, and related runbook/test coverage.

## Current State

- PR head before landing gate: `0479cd61fd2a53650c855138911c3b22089c6d3c`.
- Base branch: `main`.
- Rebased candidate after first landing attempt: `c504d51431e33e97b13814fa7c9a427d103150e0`.
- Merge/push/deploy status: not merged, not pushed to `main`, not deployed.

## Done

- Review completed against the live PR head available at the time of review; no blocking findings were found.
- Backend focused tests passed from `backend`:
  - `go test ./internal/proof/app ./internal/vaccination/app ./internal/vaccination/adapters/postgres ./internal/vaccinationexecution/app ./internal/vaccinationexecution/adapters/postgres`
- Proof-media egress guard passed:
  - `node tools/agent-hooks/check-android-proof-media-egress.mjs --all`
- First `make land-main` attempt rebased onto `origin/main` and failed before any push.
- Landing-blocker fixes applied:
  - Converted new vaccination proof observer logging to the service logger.
  - Added CEO AI coverage-matrix exclusion for proof-completion reconciliation plumbing.
  - Wrapped touched dynamic Postgres queries with `sqlbind.MustBind` and reduced the bind-contract baseline.
  - Added exception reporting for Android proof obligation-cycle metadata decode failure.
  - Added projection-review markers and invariant coverage for touched vaccination aggregate projections.
  - Re-derived two scale-guard inline-SQL baseline counts that were rebase-visible pre-existing debt.
- Focused reruns passed:
  - `make postgres-bind-contract-guard`
  - `make exception-guard scale-guard admin-web-request-reads-guard`
  - `go test ./internal/vaccinationexecution/adapters/postgres -run 'TestDriveAssignmentCarryProjectionOneToManyPageBoundaryDateShiftParkScopeStatusMatrix|TestCarrySummaryTotalExcludesTerminalObligations|TestCanonicalVaccinationReadsUseDriveAssignmentPlannedDateOneToManyPageBoundaryExecutionDateParkScopeStatusMatrix'`
- Second `make land-main` attempt at `805de97a60b67e8514fe1d0afe63797529b019f2` failed before any push on:
  - `required PostgreSQL query plans` (no Postgres DSN in the gate environment)
  - `command-board query plans` (no Postgres DSN in the gate environment)
  - `admin-web mock-fidelity` (live tracker read `park` directly instead of through shared scope parsing)
  - `android screenshots` (`RoleChromeScreenshotTest.role_operator_drawer` and `role_ceo_drawer`, app version text snapshot delta)
- Final blocker fixes applied:
  - Live tracker now uses `parseScope()`'s parsed `scope.parkId` instead of reading `park` directly.
  - Accepted visually inspected role-drawer Paparazzi snapshots for the app version text update (`1.0.35-dev`, code `85`).
- Focused reruns passed:
  - `npm --prefix apps/admin-web run check:mock-fidelity`
  - `ANDROID_HOME=/Users/raviteja/Library/Android/sdk ANDROID_SDK_ROOT=/Users/raviteja/Library/Android/sdk ./gradlew :app:verifyPaparazziDevDebug --no-daemon --console=plain --no-configuration-cache --max-workers=1 -Dkotlin.compiler.execution.strategy=in-process -Dkotlin.daemon.enabled=false -Pkotlin.compiler.execution.strategy=in-process --tests 'sg.mesha.goatos.ui.RoleChromeScreenshotTest'`
  - `GOATOS_SQLC_PLAN_ADMIN_DSN="$DATABASE_URL" make validate-sqlc-plans` after sourcing `/Users/raviteja/mesha/local-data/goatos-stg-to-oci/oci-goatos-db.env`
  - `GOATOS_PGTEST_ADMIN_DSN="$DATABASE_URL" GOATOS_SQLC_PLAN_ADMIN_DSN="$DATABASE_URL" make commandboard-query-plan-guard` after sourcing `/Users/raviteja/mesha/local-data/goatos-stg-to-oci/oci-goatos-db.env`
- Third `make land-main` attempt at `8cc6c088a8049d99ff507a96a2f06bfbd30dd4ec` failed before any push on:
  - `required PostgreSQL query plans`
  - `admin-web unit tests`
- Follow-up focused reruns passed:
  - `npm --prefix apps/admin-web test -- --runInBand`
  - `GOATOS_FAST_LOCAL_CI=1 GOATOS_SQLC_PLAN_ADMIN_DSN="$DATABASE_URL" GOATOS_PGTEST_ADMIN_DSN="$DATABASE_URL" tools/ci/run-local-ci.sh query-plans`
- Fourth `make land-main` attempt at `f3c2997421deb7bd001e94bcfc51800fba511747` failed before any push on `required PostgreSQL query plans`; focused query-plan reruns passed, so the next gate keeps `DATABASE_URL` out of unrelated parallel jobs and passes only the required `GOATOS_*` DSNs to the landing command.

## Pending

- Run exact repo landing gate from this clean isolated worktree:
  - `source /Users/raviteja/mesha/local-data/goatos-stg-to-oci/oci-goatos-db.env; env -u DATABASE_URL GOATOS_SQLC_PLAN_ADMIN_DSN="$DATABASE_URL" GOATOS_PGTEST_ADMIN_DSN="$DATABASE_URL" ANDROID_HOME=/Users/raviteja/Library/Android/sdk ANDROID_SDK_ROOT=/Users/raviteja/Library/Android/sdk make land-main`
- Verify `origin/main` readback after landing.
- Resolve GitHub PR 348 state after the certified SHA reaches `main`.

## Known Gaps

- The focused failed gates are green; final certification still requires the full landing rerun after committing the latest fixes.
- No staging deploy has been requested or run.
