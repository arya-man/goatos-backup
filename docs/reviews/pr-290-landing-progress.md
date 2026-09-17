# PR 290 Landing Progress

Last updated: 2026-09-17 before main landing.

## Scope

Land PR 290, `fix(feed): 7-day forecast lists only feeds the farm is still feeding`, to `main`.

## Done

- Reviewed the PR diff against `origin/main` in an isolated worktree.
- Confirmed PR 290 is open, not draft, mergeable, and based on `origin/main` `4acd668fb48c750bbeb267f667d436a502eac25d`.
- Verified the fetched PR head and isolated worktree HEAD both point to `c7685eb2a99d224d072d47b6885ae98b96bbfa35`.
- Ran the focused feed forecast integration tests:
  - `go test ./internal/feeddirection/adapters/postgres -run 'TestStockForecast(ReportsTheKgShortfallBelowAWeeksNeed|DropsStoppedFeedsOneToManyStatusBucketsParkScopeNoPageBoundary|OneToManyStatusBucketsParkScopeAndNoPageBoundary)$' -count=1`
  - `go test ./internal/feeddirection/adapters/postgres -run 'TestStockForecastDropsStoppedFeedsOneToManyStatusBucketsParkScopeNoPageBoundary$' -count=5`

## Pending

- Run `make land-main` from this isolated worktree after committing this progress note.
- Verify the final `HEAD`, `origin/main`, and remote `main` SHA all match the landed SHA.

## Known Failures

- None from focused review evidence.

## Judge Status

- Review found no code findings.
- Full landing certification is pending `make land-main`.

## Current SHA

- PR head before this progress note: `c7685eb2a99d224d072d47b6885ae98b96bbfa35`.
- Base main before landing: `4acd668fb48c750bbeb267f667d436a502eac25d`.

## Deployment State

- No staging or mobile deploy requested or performed.
