# PR 458 Kid Stage Shift Landing Progress

## Scope

Land PR #458 (`feat/kid-stage-shift-tasks`) to `main`, then deploy STG backend/admin-web/mobile and verify public STG plus mobile distribution.

## Current SHA

- Candidate: `860cb28b6a225468e2390e32352b607e351edc2a`
- Branch: `review-pr-458`
- PR: https://github.com/vgoats/goatos/pull/458
- GitHub merge state before landing: `BLOCKED`

## Done

- Review completed with no findings.
- Focused backend tests passed:
  - `go test ./internal/tasks/domain ./internal/tasks/app ./internal/tasks/adapters/boardsource`
  - `go test ./internal/counts/domain`
- Focused admin-web SOP model test passed:
  - `node --test --experimental-strip-types apps/admin-web/features/sops/followup-model.test.mjs`
- Focused Android tests passed:
  - `./gradlew :core:core-network:testDebugUnitTest --tests '*GoatSearchDtoDecodeTest*' :app:testDevDebugUnitTest --tests '*WorkBoardRouteIdentityTest*'`
- Progress receipt committed:
  - `44dd46ff38aa1817f7fa7a779d59ee3f59761fb3`
- First `make land-main` attempt failed before push because the required OCI query-plan tunnel on `127.0.0.1:15432` was not listening.
- OCI tunnel restored with `/Users/raviteja/mesha/tools/local/oci-goatos-a1-dev.sh tunnel`.
- Focused failing step rerun passed:
  - `GOATOS_CI_ONLY_STEP='required PostgreSQL query plans' tools/ci/run-local-ci.sh query-plans`
- Second `make land-main` attempt failed before push because this fresh worktree did not have admin-web npm dependencies installed; `admin-web lint` could not find `eslint`.
- Installed locked admin-web dependencies:
  - `npm --prefix apps/admin-web ci`
- Focused failing step rerun passed:
  - `GOATOS_CI_ONLY_STEP='admin-web lint' tools/ci/run-local-ci.sh admin-web`
- Third `make land-main` attempt failed before push on `android-bounded-memory-guard`.
- Fixed `ShiftingViewModel.preselectKids()` to use a bounded `mapNotNull` result over the already-parsed kid list instead of an unbounded mutable accumulator.
- Focused failing step rerun passed:
  - `GOATOS_CI_ONLY_STEP='android-bounded-memory-guard' tools/ci/run-local-ci.sh android`
- Fourth `make land-main` attempt failed before push on `leadership-assistant-coverage-guard`.
- Added `docs/ceo-ai/coverage-matrix.md` coverage for the kid-stage litter shift workflow helpers as write-path plumbing behind the existing counts shifting read surfaces.
- Focused failing step rerun passed:
  - `GOATOS_CI_ONLY_STEP='leadership-assistant-coverage-guard' tools/ci/run-local-ci.sh common`
- Fifth `make land-main` attempt failed before push on `scale-guard-plan-proof`.
- Added tight `scale-guard:plan-proof-exempt` markers on the litter shift SQL constants because the reads are bounded by one litter (<= 3 kids) or a keyset-limited litter page; focused rerun passed:
  - `GOATOS_CI_ONLY_STEP='scale-guard-plan-proof' tools/ci/run-local-ci.sh backend`

## Pending

- Commit the scale-guard repair and progress note.
- Rerun full `make land-main` with the OCI query-plan DSNs scoped in the environment and admin-web dependencies installed.
- Verify local and remote `main` SHA after landing.
- Run guarded STG deploy:
  - `GOATOS_REPO=/Users/raviteja/mesha/goatos-wt-pr458-review /Users/raviteja/bin/goatos-stg-deploy backend-web-mobile`
- Verify STG public API health/readiness, deployed SHA/build evidence, and mobile distribution.

## Known Failures

- First `make land-main` attempt failed before push on infrastructure only: `127.0.0.1:15432` refused the query-plan connection.
- Second `make land-main` attempt failed before push on local dependency setup only: missing admin-web `node_modules` caused `eslint`/`tsc` to be unavailable.
- Third `make land-main` attempt failed before push on code/guard issue: unbounded mutable accumulator in `ShiftingViewModel.preselectKids()`.
- Fourth `make land-main` attempt failed before push on missing leadership assistant coverage classification for the new kid-stage litter shift workflow functions.
- Fifth `make land-main` attempt failed before push on missing large-table SQL plan proof classification for bounded litter shift reads.
- A broad accidental admin-web test run outside the focused target failed on missing local dependencies (`typescript`, `@grafana/faro-core`); this is not counted as PR evidence.

## Deployment State

- Main: not landed yet.
- STG: not started.
- Mobile: not distributed for this SHA yet.
