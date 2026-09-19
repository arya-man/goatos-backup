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
- Added this progress note and rebased the landing candidate onto `origin/main` `88325fa54ceefaab336f3461133300a50d55ee4b`.

## Tests and checks performed

- `go test ./internal/growthdirector/adapters/postgres ./internal/growthdirector/app ./internal/growthdirector/domain ./internal/weighing/app`
- `node --test --experimental-strip-types apps/admin-web/features/weighing/weights-assumptions.contract.test.mjs`
- `git diff --check`
- `make land-main` attempted after the progress note commit; local CI failed before any main push.

All checks above passed on PR head `c203c192b7f15918139954377991e16fedd8014e`.

## Pending

- Fix the local CI blockers from the first `make land-main` attempt, then rerun the repo landing receipt with `make land-main`.
- Verify local HEAD and `origin/main` match the landed SHA.
- Deploy staging from a clean checkout at `origin/main`.
- Smoke staging `/livez` and `/readyz`.

## Known failures or blockers

- First `make land-main` attempt failed local CI at candidate `bb26c7d675db17411148e4109a365a9680e0fff6`.
- Failing steps: `org-boundary-guard`, `leadership-assistant-coverage-guard`, `ceo-ai-page-contract-drift-guard`, `agent: boundaries`, `exception-guard`, `exception-guard (whole-tree ratchet)`, `operational-location-guard`, `migration-duplicate-versions-guard`, `admin-web interaction patterns`, and `admin-web mock-fidelity`.
- In progress fix set: neutral FCR table-window naming, public admin-web weighing exports, returned-row action without route revalidation, backend copy producers for the Weights Assumptions drawer, explicit CEO AI coverage classifications, parser error exemption, operational-location SQL guard rewrite, and migration renumbering to `000363`-`000365`.
- Second fast-check pass cleared backend; remaining blockers were comment/literal guard wording in common/admin-web.
- Staging deployment has not started yet.

## State

- Current rebased landing candidate: `bb26c7d675db17411148e4109a365a9680e0fff6`.
- Main state: not updated for PR 320 yet.
- Staging state: not deployed for PR 320 yet.
