# PR 305 Mortality Review Fix Progress

## Scope

- Fix review finding on Counts -> Mortality historical at-risk denominators.
- Push the fix back to PR 305 branch `feat/counts-mortality`.

## Done

- Identified denominator bug: animals present during a historical window but exited after the window were excluded from `at_risk`.
- Updated `mortalityPopulationSQL` to use interval overlap: entry on or before window end and no exit before window start.
- Added regression test for an animal sold after the selected window still contributing to `at_risk`.

## Pending

- Push branch to PR.

## Tests / E2E Performed

- `go test ./internal/counts/... ./internal/adminui/... ./internal/permissions/...` from `backend`: PASS.
- `npm run typecheck -- --pretty false` from `apps/admin-web`: PASS.

## Known Failures

- None yet in this fix cycle.

## Metrics / Evidence

- Before: static review found historical denominator undercount.
- After: regression `TestMortalityAtRiskIncludesAnimalsExitedAfterTheWindow` proves a goat sold after the selected window remains in `at_risk`.

## Judge Status

- Manual review finding fixed; focused guards passed.

## Current SHA

- Before fix: `90bf02af6cf739fbaca1f9d3c07383da3fb193b1`.

## Deployment State

- No merge to main.
- No staging deployment.
