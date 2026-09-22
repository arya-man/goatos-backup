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

## Pending

- Commit the landing-blocker fixes.
- Rerun `make aggregate-projection-guard` after the fixes are in the committed diff.
- Run exact repo landing gate from this clean isolated worktree:
  - `ANDROID_HOME=/Users/raviteja/Library/Android/sdk ANDROID_SDK_ROOT=/Users/raviteja/Library/Android/sdk make land-main`
- Verify `origin/main` readback after landing.
- Resolve GitHub PR 348 state after the certified SHA reaches `main`.

## Known Gaps

- The first landing attempt's Android screenshot stage failed in `RoleChromeScreenshotTest.role_operator_drawer` and `role_ceo_drawer`; this still needs the full landing rerun after backend/guard fixes.
- No staging deploy has been requested or run.
