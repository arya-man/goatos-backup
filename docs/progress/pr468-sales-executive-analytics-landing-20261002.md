# PR 468 Sales Executive Analytics Landing - 2026-10-02

## Scope

- Land PR #468 (`feat/sales-executive-analytics`) to `main`.
- Feature scope: Sales executive analytics page, backend read endpoint, OpenAPI/generated client, admin-web route, permission/page contract wiring, and migration `000469_sales_executive_analytics_page_tick.sql`.
- Deployment scope: not deployed to STG or mobile from this task.

## Done

- Reviewed PR #468 after fixes; final reviewed head was `90895353943177bfa98e6120a0f43c098e7cfeb8`.
- Fixed the live visual route coverage gap for `/sales/executive-analytics?scope_mode=company`.
- Fixed invalid query parameter handling so bad values reach the backend and return `invalid_days` / `invalid_offset`.
- Rebased onto `origin/main` `a7ebddc899de956a46907c02ff4de0a2630b1eb0`.
- Renumbered the page-tick migration to `000467` after main's `000465` and `000466`.
- Added this progress note in commit `72ee6168f6195106d512a8d5dbf7002108cc6b3c`.
- Restored the OCI query-plan tunnel on `127.0.0.1:15432`.
- Repaired the sales executive analytics repository SQL so `scale-guard` sees zero new offenders:
  person-name resolution now avoids `COUNT(DISTINCT)`, latest-vendor pagination cuts the vendor
  page before resolving workforce names, and vendor totals SQL is hoisted to a named const.
- Rebased again onto `origin/main` `fe22610c30e9b33027e192f2256e22132a989079` after PR #469 landed.
- Resolved rebase conflicts by preserving `/sales/vendors` under `vendors_sales`, keeping the new
  `/sales/executive-analytics` page mapping, and combining PR #469's safe vendor SQL binding with
  PR #468's vendor edit audit trail.
- Renumbered the page-tick migration to `000469` after main's `000467_sales_designations.sql` and
  `000468_vendor_register_buyers_module.sql`.

## Proof So Far

- `make migration-duplicate-versions-guard` passed.
- `node --test apps/admin-web/features/procurement/sales-executive-analytics-route.test.mjs apps/admin-web/scripts/smoke-visual-route-coverage.test.mjs` passed.
- `go test ./internal/procurement/domain ./internal/procurement/app ./internal/procurement/adapters/http ./internal/procurement/adapters/postgres ./internal/adminui/app ./internal/permissions` passed.
- `npm --prefix apps/admin-web run check:mock-fidelity` passed.
- Earlier focused gates on the same reviewed head also passed: admin-web typecheck, targeted lint, and production build.
- Focused rerun after restoring the OCI tunnel:
  `GOATOS_CI_ONLY_STEP='required PostgreSQL query plans' tools/ci/run-local-ci.sh query-plans`
  passed.
- Focused admin-web lint rerun passed after normalizing local ignored `node_modules`.
- Focused scale guard rerun passed:
  `GOATOS_CI_ONLY_STEP='scale-guard' tools/ci/run-local-ci.sh backend`.

## Pending

- Run exact landing receipt with `make land-main` from this clean isolated worktree.
- Verify `origin/main` resolves to the certified landed SHA and PR #468 is reconciled.

## Known Failures / Caveats

- `make validate-migrations` is known to fail on historical hot-table validator debt already present on `main`; the duplicate-version migration guard for this PR is green.
- First `make land-main` attempt failed before push because the OCI query-plan tunnel was not
  listening on `127.0.0.1:15432`.
- Second `make land-main` attempt failed before push because local ignored admin-web
  `node_modules` contained a malformed nested `eslint-config-next` package; local npm install
  normalized the dependency tree, and `package-lock.json` was restored.
- Third `make land-main` attempt failed before push on `scale-guard`; the repository SQL shape was
  repaired and the focused scale guard is now green.
- No STG or mobile deployment has been requested or performed.

## Current SHA / Deployment State

- Candidate SHA before the landing note: `90895353943177bfa98e6120a0f43c098e7cfeb8`.
- Current candidate after second rebase and migration renumber: pending commit.
- Base SHA: `fe22610c30e9b33027e192f2256e22132a989079`.
- Deployment state: not deployed.
