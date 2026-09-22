# PR 346 weighing ADG landing progress

## Scope

- Review and land PR 346, `fix(weighing): an animal's gain is total movement, not a median of leg rates`.
- Changed surface: backend weighing analytics SQL and Postgres integration tests.
- No staging deployment requested.

## Done

- Reviewed the PR diff against `origin/main`.
- Static review found no remaining blocking findings after the PR's third commit made the scanned-vs-whole-pen cohort race use the same sex/origin claim predicates as `lump`.
- Created isolated review worktree at `/Users/raviteja/mesha/.worktrees/goatos-pr-346-review`.
- Confirmed PR head before landing: `8759f9c5792583ccfbc36c998d7dda7671d9738d`.
- `git diff --check origin/main...HEAD` passed.

## Tests and evidence

- Focused command without Postgres opt-in:
  - `go test -v ./internal/weighing/adapters/postgres -run 'TestAnimalGainIsTotalMovementNotAMedianOfLegRates|TestPenCohortOneToManyPageBoundaryParkScopeAndStatusMatrixFollowTheGrainWeighedLast|TestFilteredPenCohortKeepsScansWhenTheLaterWholePenWeighCannotBeClaimed|TestGrowthHeadlineEqualsTheGainChartForTheSameSex|TestWeeklyGainEqualsTheHeadlineWhenAllMovementIsOneWeek' -count=1`
  - Result: PASS with all selected Postgres tests skipped because `GOATOS_RUN_POSTGRES_TESTS=1` was not set.
- Focused command with Postgres opt-in:
  - `GOATOS_RUN_POSTGRES_TESTS=1 go test -v ./internal/weighing/adapters/postgres -run 'TestAnimalGainIsTotalMovementNotAMedianOfLegRates|TestPenCohortOneToManyPageBoundaryParkScopeAndStatusMatrixFollowTheGrainWeighedLast|TestFilteredPenCohortKeepsScansWhenTheLaterWholePenWeighCannotBeClaimed|TestGrowthHeadlineEqualsTheGainChartForTheSameSex|TestWeeklyGainEqualsTheHeadlineWhenAllMovementIsOneWeek' -count=1`
  - Result: PASS with all selected Postgres tests skipped because Docker was not available in this environment.

## Pending

- Commit this progress receipt.
- Run the exact landing receipt from the isolated worktree: `make land-main`.
- After landing, read back `HEAD`, `origin/main`, remote main, and PR state.

## Current state

- Merge/push/deploy status before `make land-main`: not landed, not pushed to main, not deployed.
