# PR 290 Landing Progress

Last updated: 2026-09-17 after first landing-gate retry prep.

## Scope

Land PR 290, `fix(feed): 7-day forecast lists only feeds the farm is still feeding`, to `main`.

## Done

- Reviewed the PR diff against `origin/main` in an isolated worktree.
- Confirmed PR 290 is open, not draft, mergeable, and based on `origin/main` `4acd668fb48c750bbeb267f667d436a502eac25d`.
- Verified the fetched PR head and isolated worktree HEAD both point to `c7685eb2a99d224d072d47b6885ae98b96bbfa35`.
- Ran the focused feed forecast integration tests:
  - `go test ./internal/feeddirection/adapters/postgres -run 'TestStockForecast(ReportsTheKgShortfallBelowAWeeksNeed|DropsStoppedFeedsOneToManyStatusBucketsParkScopeNoPageBoundary|OneToManyStatusBucketsParkScopeAndNoPageBoundary)$' -count=1`
  - `go test ./internal/feeddirection/adapters/postgres -run 'TestStockForecastDropsStoppedFeedsOneToManyStatusBucketsParkScopeNoPageBoundary$' -count=5`
- First `make land-main` selected `common,backend,query-plans`; backend and query-plan legs passed, including `go test ./...`, required PostgreSQL query plans, and command-board query plans.
- First `make land-main` failed before push only on `agent: ai-doctor` because this isolated worktree was missing local `.repowise` index files.
- Ran `make ai-setup`; `ai-doctor` now passes with `.repowise` current enough for `d4cb81c94bc6661524685469848850864b3a7fbc`.
- Removed setup-generated editor files from the isolated worktree so only source progress documentation remains tracked.

## Pending

- Re-run `make land-main` from this isolated worktree after committing this progress note update.
- Verify the final `HEAD`, `origin/main`, and remote `main` SHA all match the landed SHA.

## Known Failures

- First landing attempt failed on missing local `.repowise` index only; fixed by `make ai-setup`.

## Judge Status

- Review found no code findings.
- Full landing certification is pending the second `make land-main` run.

## Current SHA

- PR head before this progress note: `c7685eb2a99d224d072d47b6885ae98b96bbfa35`.
- Current candidate before second landing run: `d4cb81c94bc6661524685469848850864b3a7fbc`.
- Base main before landing: `4acd668fb48c750bbeb267f667d436a502eac25d`.

## Deployment State

- No staging or mobile deploy requested or performed.
