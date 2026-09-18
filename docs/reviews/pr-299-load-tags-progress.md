# PR 299 Counts Breakdown Purchased Loads Progress

Last updated: 2026-09-18 after focused checks.

## Scope
- Fix PR 299 so Counts Breakdown purchased loads show the actual current goat RFID/tag identifiers, not management-stage buckets, in the table under the chart.
- Preserve the existing bought/on-farm/stage/sex semantics.

## Done
- Reviewed live PR head `c72e2383e039729caad7a4fb7423becaad0f910b` and confirmed it only renamed the UI to Current stage.
- Created isolated worktree `/Users/raviteja/mesha/tmp/pr-299-load-tags` from `origin/pr-299`.
- Added `current_tags` to the Counts Breakdown load response, sourced from active `goat_identifiers` rows for `animal_identifier_1` and `animal_identifier_2`.
- Updated admin-web to render the actual current tag values in the Purchased loads table.
- Updated OpenAPI and regenerated `packages/api-client/src/generated/app-api.ts`.

## Pending
- Push branch `feat/counts-breakdown-purchased-loads`.

## Tests / E2E Performed
- `go test ./internal/counts/adapters/postgres -run TestCountsBreakdownLoadsReadCurrentTagAndSexPerLoadAndMatchTheSalesPurchasedRule -count=1` from `backend` — PASS.
- `go test ./internal/counts/domain -count=1` from `backend` — PASS.
- `npm --prefix apps/admin-web run typecheck` — PASS after local `npm --prefix apps/admin-web ci --no-audit --no-fund`; shell Node is v23.1.0 while package declares Node 24.x, but TypeScript completed green.
- `make api-client-generate` — PASS.
- `make api-client-check` after committing generated output — PASS.

## Known Failures
- None in focused checks after the fix.

## Before / After Metrics
- Not a latency/performance change. No metrics captured yet.

## Judge Status
- Manual review finding fixed locally; focused backend test now asserts actual `animal_identifier_1` / `animal_identifier_2` values.

## Current SHA
- PR head before fix: `c72e2383e039729caad7a4fb7423becaad0f910b`.

## Deployment State
- PR-only work. No main landing or staging deploy.
