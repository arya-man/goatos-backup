# PR 303 Delete Fence Fix - 2026-09-18

## Scope

Fix PR 303 review finding: keyed configuration registers accepted a stale delete even though the API contract carries `row_version`.

## Done

- Added a `row_version` predicate to `keyedStore.del`.
- Added a real Postgres regression in `TestConfigurationRegistersLifecyclePostgresPaths` for a dynamic `ref:exit_reasons` row:
  stale delete returns `ErrVersionConflict`, current delete succeeds.
- Focused configuration package tests passed.

## Pending

- None.

## Tests / E2E

- `cd backend && go test ./internal/configuration/...` - GREEN.

## Known Failures

- None yet.

## Current SHA

- Base before fix: `52b2bd4928f09ac8cbd725f927ef7ebf9caf7ed6`.
- Fix commit pushed: `aa429412c`.

## Deployment State

- No deploy requested or performed.
