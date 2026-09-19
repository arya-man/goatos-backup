# PR 320 landing and staging progress

Date: 2026-09-19

## Scope

- PR: https://github.com/vgoats/goatos/pull/320
- Branch: feat/weighing-assumptions
- Work: weighing FCR tab, sale prices as tenant data, assumptions drawer, farm value controls, and backend/admin-web guards for growth assumptions.
- Promotion request: merge to main, then deploy staging successfully.

## Done

- Reviewed PR 320 for Growth Director and weighing assumption regressions.
- Fixed failed-assumption fallback so admin-web does not silently use stale default sale prices after the tenant assumptions request fails.
- Fixed sale-ready lower/threshold validation in app/domain code.
- Fixed concurrent sale-ready line edits by enforcing the invariant inside the Postgres transaction with a transaction-scoped advisory lock.
- Pushed PR head commit `c203c192b7f15918139954377991e16fedd8014e`.

## Tests and checks performed

- `go test ./internal/growthdirector/adapters/postgres ./internal/growthdirector/app ./internal/growthdirector/domain ./internal/weighing/app`
- `node --test --experimental-strip-types apps/admin-web/features/weighing/weights-assumptions.contract.test.mjs`
- `git diff --check`

All checks above passed on PR head `c203c192b7f15918139954377991e16fedd8014e`.

## Pending

- Run the repo landing receipt with `make land-main` after this progress note commit.
- Verify local HEAD and `origin/main` match the landed SHA.
- Deploy staging from a clean checkout at `origin/main`.
- Smoke staging `/livez` and `/readyz`.

## Known failures or blockers

- GitHub reports PR 320 `mergeStateStatus=BLOCKED`; local certified landing is still required.
- No current local landing receipt has run after this progress note.
- Staging deployment has not started yet.

## State

- Current PR head before this progress note: `c203c192b7f15918139954377991e16fedd8014e`.
- Main state: not updated for PR 320 yet.
- Staging state: not deployed for PR 320 yet.
