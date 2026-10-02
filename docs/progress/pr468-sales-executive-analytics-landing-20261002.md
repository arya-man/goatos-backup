# PR 468 Sales Executive Analytics Landing - 2026-10-02

## Scope

- Land PR #468 (`feat/sales-executive-analytics`) to `main`.
- Feature scope: Sales executive analytics page, backend read endpoint, OpenAPI/generated client, admin-web route, permission/page contract wiring, and migration `000467_sales_executive_analytics_page_tick.sql`.
- Deployment scope: not deployed to STG or mobile from this task.

## Done

- Reviewed PR #468 after fixes; final reviewed head was `90895353943177bfa98e6120a0f43c098e7cfeb8`.
- Fixed the live visual route coverage gap for `/sales/executive-analytics?scope_mode=company`.
- Fixed invalid query parameter handling so bad values reach the backend and return `invalid_days` / `invalid_offset`.
- Rebased onto `origin/main` `a7ebddc899de956a46907c02ff4de0a2630b1eb0`.
- Renumbered the page-tick migration to `000467` after main's `000465` and `000466`.

## Proof So Far

- `make migration-duplicate-versions-guard` passed.
- `node --test apps/admin-web/features/procurement/sales-executive-analytics-route.test.mjs apps/admin-web/scripts/smoke-visual-route-coverage.test.mjs` passed.
- `go test ./internal/procurement/domain ./internal/procurement/app ./internal/procurement/adapters/http ./internal/procurement/adapters/postgres ./internal/adminui/app ./internal/permissions` passed.
- `npm --prefix apps/admin-web run check:mock-fidelity` passed.
- Earlier focused gates on the same reviewed head also passed: admin-web typecheck, targeted lint, and production build.

## Pending

- Commit this landing progress note.
- Run exact landing receipt with `make land-main` from this clean isolated worktree.
- Verify `origin/main` resolves to the certified landed SHA and PR #468 is reconciled.

## Known Failures / Caveats

- `make validate-migrations` is known to fail on historical hot-table validator debt already present on `main`; the duplicate-version migration guard for this PR is green.
- No STG or mobile deployment has been requested or performed.

## Current SHA / Deployment State

- Candidate SHA before the landing note: `90895353943177bfa98e6120a0f43c098e7cfeb8`.
- Base SHA: `a7ebddc899de956a46907c02ff4de0a2630b1eb0`.
- Deployment state: not deployed.
