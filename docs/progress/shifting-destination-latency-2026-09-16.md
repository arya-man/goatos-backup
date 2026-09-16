# Shifting Destination Catalog Latency - 2026-09-16

## Scope
Optimize `/app/counts/shifting/destinations` / `Repository.ShiftingDestinationCatalog` latency from the current `origin/main` code path while preserving the backend-owned destination catalog contract: tenant scoping, active parks including empty parks, active sheds, partition catalog rows including empty pens, same-park partition-alias suppression, repeated shed names under different parks, per-pen resident stages, authored configured cohort, non-clinical stage filtering, birth placement metadata, and stable ordering.

## Done
- Worktree cut from `origin/main` at `397114d1d`; clean PR branch is `fix/shifting-destination-latency-clean-20260916`.
- Initial code inspection found the hot query in `backend/internal/counts/adapters/postgres/shifting_destinations.go`.
- Current SQL uses per-row correlated laterals against `goats` for `animal_count` and `stage_agg`.
- Replaced the repeated correlated `goats` scans with a tenant-scoped `resident_agg` CTE grouped by `(tenant_id, shed_id, normalized partition label)`.
- Preserved active empty parks, active empty catalog partitions, alias suppression, tenant scope, configured pen cohort, per-pen resident stages, non-clinical filtering, and stable ordering.
- Captured direct OCI repository before/after numbers against tenant `00000000-0000-4000-8000-000000000001`, 2 parks, 120 destination rows, 25 warm samples after 5 warmups.
- Ran Chrome E2E against isolated local API `127.0.0.1:18117` backed by the same OCI database.

## Pending
- PR review/CI completion.
- No main landing or staging deploy has been attempted.

## Tests / E2E Performed
- Passed: `cd backend && go test ./internal/counts/adapters/postgres -run 'TestShiftingDestinationCatalog|TestGoatShiftingFacts' -count=1`
- Passed: `cd backend && go test ./internal/counts/app -run 'TestShiftingDestinations' -count=1`
- Passed: `cd backend && go test ./internal/counts/adapters/http -run 'Test.*Shifting.*Destination|Test.*BirthPlacement|Test.*WriteHandler.*Shift' -count=1`
- Passed before baseline: `GOATOS_LIVE_OCI_BENCH=1 GOATOS_TENANT_ID=00000000-0000-4000-8000-000000000001 go test -tags liveoci ./internal/counts/adapters/postgres -run TestLiveOCIShiftingDestinationCatalogLatency -count=1 -v` from detached `origin/main`.
- Passed after branch: same live OCI benchmark command from this branch.
- Passed Chrome E2E through installed Chrome + Playwright against `http://127.0.0.1:18117/app/counts/shifting/destinations`: HTTP 200, 2 parks, 120 destinations, 17 management stages, contract fields present, and no `backend_down`, `Admin-web contract unavailable`, `The board could not be loaded`, or `Weights could not be loaded`. Visual proof validated at `.e2e-artifacts/shifting-destinations/chrome-shifting-destinations-e2e.png`.
- Passed after clean PR review: `node tools/agent-hooks/check-operational-location.mjs`
- Passed after clean PR review: `cd backend && go test ./internal/counts/adapters/postgres ./internal/counts/app ./internal/counts/adapters/http -count=1`

## Known Failures
- None yet.

## Before / After Metrics
- Before (`origin/main` `397114d1d`, live OCI repository call): parks=2, sheds=120, n=25, min=585.00ms, p50=607.06ms, p90=679.62ms, p95=685.34ms, max=696.45ms, avg=621.26ms.
- After (branch, same live OCI repository call): parks=2, sheds=120, n=25, min=64.95ms, p50=91.22ms, p90=158.77ms, p95=166.26ms, max=176.32ms, avg=103.28ms.
- Improvement: p50 improved by 84.97%; p90 improved by 76.64%; average improved by 83.38%.
- Chrome E2E route timing after branch: n=10, min=117.60ms, p50=123.00ms, p90=208.30ms, p95=329.40ms, max=329.40ms, avg=158.49ms.

## Judge Status
- Clean PR review pass found no correctness regressions in empty parks, empty partitions, alias suppression, per-pen resident stages/head counts, configured stage vs resident fallback, tenant scope, or ordering.

## Current SHA
- `397114d1d` (`origin/main` at worktree creation).

## Deployment State
- No merge, no main push, no staging deploy.
