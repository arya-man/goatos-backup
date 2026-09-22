# PR 356 Runtime Review Fix - 2026-09-23

## Scope

Fix review blockers found after PR 356:

- Published Health Config diagnosis registers were visible as live but ignored by `SubmitObservation`.
- Health Analytics pen-type breakdown followed a goat's current partition instead of the case's snapshotted partition.

## Done

- `DiagnosisService` now resolves the tenant's published authored register through the health repository at observation time.
- The published-register read is performed inside the existing `SubmitObservation` transaction, so one observation uses one DB connection and cannot pool-starve waiting on a nested register read.
- The committed embedded register remains the fallback only when no published register exists.
- Authored registers can compile into the engine's `Register` shape with the same defaults, class binding, and structural validation as embedded registers.
- The observation API now accepts optional authored `answers` and runs the published register's authored answer-to-token mapping when answers are supplied; legacy `findings` clients still work.
- Authored answers that affect diagnosis are now persisted with the run and exposed on diagnosis-run readback.
- Explicit empty authored `answers: {}` now validates through the authored form and is rejected as missing required answers; only absent `answers` keeps legacy clients on the legacy findings path.
- Explicit empty authored answers are preserved in stored/readback form data and serialize as `answers: {}`; legacy rows still omit `answers`.
- Diagnosis status contracts now include `declined` wherever runtime can return it.
- OpenAPI and the generated TypeScript API client now expose optional observation `answers`.
- Register resolver and compile failures now abort submission as backend errors before any diagnosis run is inserted.
- Health Analytics pen-type breakdown now resolves from `health_cases.shed_id` and `health_cases.partition_label`, not `goat_shed_partitions`.
- Production health case creation now snapshots `partition_label` on both manual `OpenCase` and diagnosis-confirmation case creation paths.
- Updated the analytics grain integration fixture to seed `shed_partitions.shed_type` instead of the retired `shed_profiles.shed_type` column.
- Added focused regression tests for published-register runtime selection, fallback, resolver error propagation, analytics snapshot usage, and the retired-column fixture path.

## Pending

- Push to the PR head branch.
- Run `make land-main` after final commit/rebase before merging to `main`.

## Tests

Run from `backend`:

```text
go test ./internal/bootstrap ./internal/health/diagnosis ./internal/health/domain ./internal/health/app ./internal/health/adapters/http ./internal/health/adapters/postgres
```

Result: PASS.

## Agent Review

- Backend diagnosis runtime: `No bug found`.
- HTTP/API/admin contract: `No bug found`.
- SQL/persistence/analytics: `No bug found`.

## Known Failures

- None in the focused gate above.

## Deployment State

- No merge to `main`.
- No staging deploy.
- No production/mobile deploy.

## Current SHA

- Base PR head before fix: `2914a9ab49218cdd13054fc45457790c700a1790`.
