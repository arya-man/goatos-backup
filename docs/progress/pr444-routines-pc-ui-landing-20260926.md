# PR 444 Routines / Preventive Care Landing Progress

Updated: 2026-09-26 15:32 IST

## Scope

- PR: https://github.com/vgoats/goatos/pull/444
- Branch: fix/routines-pc-ui-sweep, reviewed from isolated worktree `/Users/raviteja/mesha/goatos-wt-pr444-review`.
- Product scope: one-person routine assignment, redesigned routine drawer, Preventive Care pen-board / schedule / command-board fixes, and phone/header wording fixes.

## Done

- Reviewed the PR purpose, changed files, and recent one-month context for pen routines, vaccination execution, and admin-web Preventive Care surfaces.
- Preserved the dirty primary checkout; all review/landing work stayed in the isolated worktree.
- Rebased the PR candidate onto current `origin/main`.
- Focused backend checks passed before landing:
  - `GOATOS_RUN_POSTGRES_TESTS=0 go test ./internal/penroutines/...`
  - `GOATOS_RUN_POSTGRES_TESTS=0 go test ./internal/vaccinationexecution/app ./internal/vaccinationexecution/adapters/postgres -run 'TestShedDrilldown|TestDriveAssignments|TestPartition|Test.*Schedule|Test.*Catalog'`
  - `git diff --check origin/main...HEAD`
- First `make land-main` attempt failed at `leadership-assistant-coverage-guard`; fixed by adding `docs/ceo-ai/coverage-matrix.md` coverage for `RoleHoldersFromSQL` / `RoutineAssigneeSQL`, then reran the exact guard green.
- Second `make land-main` attempt failed at `scale-guard-plan-proof`; the changed `driveAssignmentsSQL` line now carries a narrow plan-proof exemption because the added lookup is post-group and keyed by the `shed_partitions` primary key, not a changed scan over goats or obligation tables.
- Third `make land-main` attempt failed at `agent: boundaries` because an internal browser history-state marker used legacy Goat OS wording; the marker has been renamed to a Mesha-neutral key.

## Pending

- Rerun the exact `agent: boundaries` guard, then run final `make land-main` from the clean isolated worktree after the boundary fix is committed.
- Verify the final landed SHA matches local `HEAD`, local `origin/main`, and remote `main`.
- Resolve or close PR #444 only after landing succeeds.

## Known Failures / Gaps

- The review worktree did not have Node dependencies installed, so `npm --prefix apps/admin-web run typecheck` could not be rerun there (`tsc` missing).
- No staging deployment has been started.
- No final staging/admin-web/mobile smoke has been performed.

## Current State Before Landing Gate

- Current candidate SHA before the boundary fix commit: `77fc1bc21666f7dede3c8501d71b59b7e7626272`.
- Deployment state: not deployed.
- Judge/review state: no blocking review findings found; local landing certification pending.
