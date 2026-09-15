# PR 267 Landing Progress

Scope: land PR 267 (`fix/admin-web-in-place-feedback`) to `main`.

## Scope

- Remove the admin-web Sales pipeline/evidence surfaces requested on 2026-09-15.
- Keep Market Survey config writes in place instead of redirecting to the top of `/sales/config`.
- Make SOP publish close the editor and return to the module library with a visible published-version banner.
- Revalidate every module SOP route served by the sidebar after SOP mutations.

## Done

- Reviewed current PR head `a9250a014`.
- No blocking review finding found in the PR pass.
- Verified the Sales guard, proof-media egress guard, and backend admin UI package tests.
- Verified targeted Market Config and SOP publish tests directly.

## Pending

- Rebase the candidate onto fresh `origin/main`.
- Run the repo landing gate with `make land-main`.
- Confirm `origin/main` contains the certified landed SHA.

## Tests And Guards Performed Before Landing

- `node --test --experimental-strip-types apps/admin-web/features/procurement/market-config.test.mjs apps/admin-web/features/sops/publish-closes-editor.test.mjs` — passed, 7/7.
- `node tools/agent-hooks/check-sales-pages.mjs` — passed.
- `ADMIN_WEB_PROOF_MEDIA_EGRESS_BASE=origin/main node tools/agent-hooks/check-admin-web-proof-media-egress.mjs` — passed, 13 files checked.
- `go test ./internal/adminui/app` from `backend` — passed.
- `git diff --check origin/main...HEAD` — passed.

Known local limitations before landing:

- `npm --prefix apps/admin-web run typecheck` could not run in the isolated worktree before dependency install because `tsc` was not present in local `node_modules`.
- Running the package `test` script with explicit files triggered the full admin-web suite because of package script expansion; two unrelated existing tests failed there (`features/responsive-viewport-guard.test.mjs`, `features/verification-review/review-events.test.mjs`). The PR-specific tests were rerun directly and passed.

## Current State

- Candidate SHA before landing gate: `a9250a014`.
- Deployment state: not deployed; this entry is for main landing only.
