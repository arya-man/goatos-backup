# PR 305 Mortality Review Fix Progress

## Scope

- Fix review finding on Counts -> Mortality historical at-risk denominators.
- Push the fix back to PR 305 branch `feat/counts-mortality`.

## Done

- Identified denominator bug: animals present during a historical window but exited after the window were excluded from `at_risk`.
- Updated `mortalityPopulationSQL` to use interval overlap: entry on or before window end and no exit before window start.
- Added regression test for an animal sold after the selected window still contributing to `at_risk`.
- Review pass 2026-09-18 found the later live-head-count change had drifted from Counts Breakdown:
  Mortality counted clinical lifecycle statuses such as `icu` as animals, while Counts Breakdown
  and Herd Analytics default to `lifecycle_status = 'alive'`.
- Updated `mortalityPopulationSQL` so Mortality `animals` uses the same strict live population as
  Counts Breakdown, and corrected the status-matrix regression to exclude `icu` from the denominator.
- Removed stale "population at risk" wording from the Mortality route/API/admin-web comments.

## Pending

- No code findings pending after post-push judges.
- Remote PR status remains merge-state `BLOCKED`; no main merge attempted in this pass.

## Tests / E2E Performed

- `go test ./internal/counts/... ./internal/adminui/... ./internal/permissions/...` from `backend`: PASS.
- `npm run typecheck -- --pretty false` from `apps/admin-web`: PASS.
- `go test ./internal/counts/domain ./internal/counts/app ./internal/counts/adapters/http` from `backend`: PASS.
- `go test ./internal/counts/adapters/postgres -run 'TestMortality|TestCountsBreakdownDefault' -count=1` from `backend`: PASS.
- `git diff --check origin/main...HEAD && git diff --check`: PASS.
- `npm run test -- smoke-visual-route-coverage.test.mjs --runInBand` from `apps/admin-web`: PASS (819 node tests, including visual route coverage guard).
- `make api-client-check` from repo root: PASS; regenerated API clients matched the checked-in files.

## Known Failures

- None yet in this fix cycle.

## Metrics / Evidence

- Before: static review found historical denominator undercount.
- After: regression `TestMortalityAtRiskIncludesAnimalsExitedAfterTheWindow` proves a goat sold after the selected window remains in `at_risk`.
- Later review: static + existing Counts Breakdown tests showed the live-head-count rule must be
  strict `lifecycle_status = 'alive'`, excluding clinical statuses such as `icu`; Mortality
  Postgres regression now pins that behavior.

## Judge Status

- Judge 1 backend Counts Mortality logic: PASS, no findings on code SHA `345ff6f42176140c6479e1440bf241044ba9d9a0`.
- Judge 2 admin-web/API contract wiring: PASS, no findings on code SHA `345ff6f42176140c6479e1440bf241044ba9d9a0`.
- Judge 3 coverage/guard adequacy: initial process finding for stale receipt and missing `make api-client-check`; `make api-client-check` now passed and this receipt is updated.
- Follow-up Judge 3 on pushed receipt SHA `431dca73e5def38e527f3e29f1b67e2536c29ad9`: found the receipt still needed to name the final pushed proof commit; this entry and Current SHA now do that.

## Current SHA

- Before first fix: `90bf02af6cf739fbaca1f9d3c07383da3fb193b1`.
- Before strict-live fix: `db8cf68c969d737b49151810a3c0df10f52e2536`.
- Strict-live code fix pushed: `345ff6f42176140c6479e1440bf241044ba9d9a0`.
- Current pushed proof head: `431dca73e5def38e527f3e29f1b67e2536c29ad9`.

## Deployment State

- No merge to main.
- No staging deployment.

## Superseded (maintainer decision 2026-09-18, same day)

- The at-risk denominator (both the original window population and the interval-overlap
  fix above) is RETIRED. Maintainer instruction: "remove that at risk altogether, it is how
  many animals there are in that section". `rate_pct` now divides the window's deaths by the
  section's LIVE head count today -- the same figure Counts Breakdown reports. Wire fields
  renamed `at_risk` -> `animals` (and `kid_`/`adult_` variants).
- `TestMortalityAtRiskIncludesAnimalsExitedAfterTheWindow` is replaced by
  `TestMortalityAnimalsIsTodaysHeadCountNotAnAtRiskPopulation`, which pins the new rule with
  the same fixture plus an animal born after the window (in the head count, not exposed).
- The inferred-cause bound (cases open at the moment of death) from `90bf02af6` is kept.
