# PR 458 Kid Stage Shift Landing Progress

## Scope

Land PR #458 (`feat/kid-stage-shift-tasks`) to `main`, then deploy STG backend/admin-web/mobile and verify public STG plus mobile distribution.

## Current SHA

- Candidate: `470371c4ca45de86544bbb6e7360cb8e84e8f79e`
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

## Pending

- Run `make land-main` after this progress doc is committed or otherwise included in the candidate.
- Verify local and remote `main` SHA after landing.
- Run guarded STG deploy:
  - `GOATOS_REPO=/Users/raviteja/mesha/goatos-wt-pr458-review /Users/raviteja/bin/goatos-stg-deploy backend-web-mobile`
- Verify STG public API health/readiness, deployed SHA/build evidence, and mobile distribution.

## Known Failures

- None currently attributed to PR #458.
- A broad accidental admin-web test run outside the focused target failed on missing local dependencies (`typescript`, `@grafana/faro-core`); this is not counted as PR evidence.

## Deployment State

- Main: not landed yet.
- STG: not started.
- Mobile: not distributed for this SHA yet.
