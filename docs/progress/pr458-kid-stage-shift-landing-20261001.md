# PR 458 Kid Stage Shift Landing Progress

## Scope

Land PR #458 (`feat/kid-stage-shift-tasks`) to `main`, then deploy STG backend/admin-web/mobile and verify public STG plus mobile distribution.

## Current SHA

- Candidate: `44dd46ff38aa1817f7fa7a779d59ee3f59761fb3`
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

## Pending

- Rerun full `make land-main` with the OCI query-plan DSNs scoped in the environment.
- Verify local and remote `main` SHA after landing.
- Run guarded STG deploy:
  - `GOATOS_REPO=/Users/raviteja/mesha/goatos-wt-pr458-review /Users/raviteja/bin/goatos-stg-deploy backend-web-mobile`
- Verify STG public API health/readiness, deployed SHA/build evidence, and mobile distribution.

## Known Failures

- First `make land-main` attempt failed before push on infrastructure only: `127.0.0.1:15432` refused the query-plan connection.
- A broad accidental admin-web test run outside the focused target failed on missing local dependencies (`typescript`, `@grafana/faro-core`); this is not counted as PR evidence.

## Deployment State

- Main: not landed yet.
- STG: not started.
- Mobile: not distributed for this SHA yet.
