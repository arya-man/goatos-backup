# PR 299/300/301/302 Club Landing Progress

## Scope

- Club PR 299, PR 300, PR 301, and PR 302 together.
- Land the integrated result to `main`, close the PRs, then deploy Goat OS staging with backend, admin-web, and mobile distribution.

## Done

- Created isolated worktree `/Users/raviteja/mesha/.landing-worktrees/club-pr299-pr302` from `origin/main`.
- Fetched PR heads:
  - PR 299: `f0b28ef637526f0e14b868549e8916a35e06d2e6`
  - PR 300: `0a6436e87f3902e440801b478be946a5397e22ae`
  - PR 301: `8da1a8b7f18ce1c0ea4962dd24c252cb4906e709`
  - PR 302: `5bddbb60f7b012d9bfc7d4b9b7b2cd7bad5715cb`
- Merged all four PR heads into `club/pr299-pr302` with no conflicts.
- Focused backend, admin-web, and Android checks passed.
- First full `make land-main` receipt ran to completion but failed before push on three gates: `org-boundary-guard`, `agent: ai-doctor`, and `admin-web phone viewport`.
- Fixed the guard issues:
  - converted the new counts sort helpers from `sort.SliceStable` added lines to `slices.SortStableFunc`;
  - changed `.pa-grid` from `min-width: 620px` to `min-width: 38.75rem`;
  - ran `make ai-setup`, after which `ai-doctor` passed.
- Focused reruns of the three failed gates passed.

## Pending

- Resolve conflicts, regenerate generated clients if needed, and commit the integrated candidate.
- Run the required local landing receipt after final rebase.
- Push certified `main`, confirm local/remote SHA, close PRs, and run STG deployment.

## Exact Tests / E2E Performed

- `go test ./internal/counts/adapters/postgres ./internal/feeddirection/adapters/postgres ./internal/penroutines/...` from `backend` passed.
- `node --test --experimental-strip-types features/feed/feed-shed-feed-charts.test.mjs features/pen-routines/pen-routines.test.mjs` from `apps/admin-web` passed.
- `ANDROID_HOME=/Users/raviteja/Library/Android/sdk ANDROID_SDK_ROOT=/Users/raviteja/Library/Android/sdk ./gradlew :app:testStgDebugUnitTest --tests 'sg.mesha.goatos.viewmodel.PenRoutineDetailViewModelTest'` from `apps/goatos-android` passed.
- First `make land-main` selected `make ci-local-screenshots`; heavy lanes passed, including `go test ./...`, required PostgreSQL query plans, command-board query plans, admin-web lint/typecheck/unit/build, Android compile/unit/lint, Android screenshots, and Android benchmark compile. Receipt was red only because of the three gates listed under Done.
- After fixes, `node tools/agent-hooks/check-org-boundary.mjs && node tools/agent-hooks/check-admin-web-phone-viewport.mjs && bash tools/agent-hooks/ai-doctor.sh` passed.
- `go test ./internal/counts/adapters/postgres -run 'TestCountsBreakdownLoadsReadCurrentTagAndSexPerLoadAndMatchTheSalesPurchasedRule|TestCountsBreakdown' -count=1` passed.

## Known Failures

- Initial Android command `./gradlew :app:testDebugUnitTest --tests 'sg.mesha.goatos.viewmodel.PenRoutineDetailViewModelTest'` failed because the task name is ambiguous across `dev`, `prod`, and `stg` flavors.
- Retried `:app:testStgDebugUnitTest` without SDK environment failed because the fresh worktree has no `local.properties`; rerun with `ANDROID_HOME` and `ANDROID_SDK_ROOT` passed.

## Before / After Metrics

- Not applicable yet. No performance claim has been made for this club.

## Judge Status

- Focused guards green. First full landing receipt red on named guard issues; second full landing receipt pending.

## Current SHA

- Club branch is at `17bc3b894` before committing this progress update.

## Deployment State

- No merge, push, main landing, or STG deploy has happened yet.
