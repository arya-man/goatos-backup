# Herd Signals Partition + 24h Delta Progress

## Scope
- Show the mapped animal's actual current shed partition in Herd Signals Live Monitor instead of falling back to a shed-level partition guess.
- Add a rolling 24h motion delta to the Herd Signals live contract and admin-web table.
- Keep wording honest: 24h delta is motion-counter movement units, not step count.

## Done
- Clean worktree created from `origin/main`.
- Backend now reads the mapped animal's current `goat_shed_partitions.partition_label` and composes pen display through the shared operational-location formatter.
- Backend/API now exposes `motion_delta_24h` as a rolling 24h motion-counter delta, explicitly not step count.
- Admin-web Live Monitor, drawer/history detail, CSV export, local API type, OpenAPI, and checked-in generated client include `motion_delta_24h`.
- Judge review completed; P2 hot-path issue fixed by fetching 24h deltas only for returned/exported rows, not the full risk-comparison cohort. P3 display-only sort concern accepted and test wording clarified.
- Full local landing receipt passed and the change was pushed to `main`.

## Pending
- None.

## Tests / E2E Performed
- `go test ./internal/herdsignals/app ./internal/herdsignals/adapters/postgres` from `/backend`: PASS.
- `go test ./internal/herdsignals/...` from `/backend`: PASS.
- `node --test apps/admin-web/features/herd-signals/herd-signals-live-table-controls.test.mjs`: PASS.
- `node --test apps/admin-web/features/herd-signals/*.test.mjs`: PASS.
- `node` OpenAPI smoke for `motion_delta_24h` and required-field indentation: PASS.
- `git diff --check`: PASS.
- `cd backend && go test ./internal/herdsignals/adapters/postgres -run 'TestGetMotionDeltas24h(OneToMany|PageBoundary|ScopeHierarchy)' -count=1`: PASS.
- `cd backend && go test ./internal/herdsignals/adapters/postgres ./internal/herdsignals/app -count=1`: PASS.
- `GOATOS_FAST_LOCAL_CI=1 tools/ci/run-local-ci.sh backend`: GREEN at `55fbbbe55`.
- `GOATOS_SQLC_PLAN_ADMIN_DSN=<set> GOATOS_PGTEST_ADMIN_DSN=<set> GOATOS_FAST_LOCAL_CI=1 tools/ci/run-local-ci.sh query-plans`: GREEN at `55fbbbe55`.
- `node tools/agent-hooks/check-leadership-assistant-coverage.mjs`: PASS at `53e208508`.
- `bash tools/agent-hooks/ai-doctor.sh`: PASS at `53e208508`.
- `git rebase origin/main`: PASS; rebased candidate is `8c3a689c2`.
- `make ai-rebuild-repowise`: PASS after final progress-doc amend.
- `GOATOS_SQLC_PLAN_ADMIN_DSN=<set> GOATOS_PGTEST_ADMIN_DSN=<set> make land-main`: GREEN at `ad88028df`; pushed `ad88028df` to `origin/main`.

## Known Failures
- Earlier `npm --prefix packages/api-client run generate` failed before dependencies were installed in the clean worktree. The local CI contract-drift step subsequently regenerated `packages/api-client/src/generated/app-api.ts`; that generated diff is committed.
- Full `make land-main` at `16cb01913` failed before push. Root causes observed: leadership coverage guard needed the new `GetMotionDeltas24h` backend function recorded in `docs/ceo-ai/coverage-matrix.md`; `ai-doctor` needed `.repowise` refreshed after the amend; OCI query-plan DB tunnel dropped during the query-plan phase. Coverage matrix was fixed and amended, `.repowise` was rebuilt, and the OCI tunnel was restarted.
- Full `make land-main` rerun after the fixes passed at `ad88028df` and pushed to `main`.

## Before / After Metrics
- Before: Live Monitor Pen column can show only shed + park when a shed has multiple active partitions; Motion Count is a raw cumulative counter with 15m/1h deltas only.
- After: Live rows prefer animal current partition display like `Yashoda 2`; table/detail/export include rolling `24h delta` movement units.

## Judge Status
- Completed. P2 fixed; P3 accepted as display-only aggregate with explicit copy/test wording.

## Current SHA
- Implementation landing SHA: `ad88028df`.
- This progress receipt may have a docs-only follow-up commit on top; use `git rev-parse origin/main` for the latest landed SHA.

## Deployment State
- Landed on `origin/main`. Staging deployment was not run in this task.
