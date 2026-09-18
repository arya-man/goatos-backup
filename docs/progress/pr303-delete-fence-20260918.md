# PR 303 Delete Fence Fix - 2026-09-18

## Scope

Fix PR 303 review finding: keyed configuration registers accepted a stale delete even though the API contract carries `row_version`.

## Done

- Added a `row_version` predicate to `keyedStore.del`.
- Added a real Postgres regression in `TestConfigurationRegistersLifecyclePostgresPaths` for a dynamic `ref:exit_reasons` row:
  stale delete returns `ErrVersionConflict`, current delete succeeds.
- Focused configuration package tests passed.
- Review follow-up: bulk sheet exports now carry `row_version` for updateable registers and import apply passes that fence into `Service.Update`, so stale downloaded sheets cannot silently overwrite newer drawer/import edits.
- Review follow-up: XLSX uploads now reject oversized workbook XML parts, oversized worksheets, and oversized shared strings before row validation, closing the compressed-workbook expansion risk.

## Pending

- None.

## Tests / E2E

- `cd backend && go test ./internal/configuration/...` - GREEN.
- `cd backend && go test ./internal/configuration/domain ./internal/configuration/app ./internal/configuration/adapters/http ./internal/permissions ./internal/adminui/app` - GREEN after the review follow-up fixes.
- `git diff --check` - GREEN.

## Known Failures

- None yet.

## Current SHA

- Base before fix: `52b2bd4928f09ac8cbd725f927ef7ebf9caf7ed6`.
- Fix commit pushed: `aa429412c`.
- Review follow-up base: `21b8485012a7d2c36738ab184a342a6ade39cf7b`.
- Review follow-up commit: branch HEAD after this progress update.

## Deployment State

- No deploy requested or performed.
