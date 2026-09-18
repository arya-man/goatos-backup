# PR 307 Farm Born Review Fix

## Scope

Fix the PR 307 review finding where Sales > Farm born counted revenue and deal dates from non-closed sales deals.

## Done

- Review finding identified: `farm_born_sales_repository.go` joined tagged allocations to every sales deal status.
- Patched `farm_born_sales_repository.go` so farm-born deal attribution only attaches `Deal Closed` deals.
- Added a postgres regression fixture for a sold animal tagged to an `Advance Paid` deal whose deal date is inside the window but actual exit date is outside it.

## Pending

- Push to the PR branch after proof is green.

## Tests / E2E Performed

- Before fix: `go test ./internal/procurement/... ./internal/adminui/app` passed.
- Before fix: `npm --prefix apps/admin-web test -- admin-route-page-contract.test.mjs` passed, 819 tests.
- After fix: `cd backend && go test ./internal/procurement/adapters/postgres -run 'TestFarmBorn' -count=1` passed.
- After fix: `cd backend && go test ./internal/procurement/... ./internal/adminui/app` passed.
- After fix: `npm --prefix apps/admin-web test -- admin-route-page-contract.test.mjs` passed, 819 tests.

## Known Failures

- None known for the patched scope.

## Current SHA

- `review/pr-307` at `b0580e9d7e3508adee9896dc1fb7901f26e90232`.

## Deployment State

- No merge, no main push, no staging deploy.
