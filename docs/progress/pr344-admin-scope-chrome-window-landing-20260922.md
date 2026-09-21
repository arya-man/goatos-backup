# PR 344 admin scope chrome landing progress

## Scope

- Fix admin-web top-bar park scope rewrites so page-local query filters do not
  corrupt date windows or repeated params.
- Hide false/duplicative global park chrome on no-scope, local-scope, SOP,
  config, and fixed-detail routes.
- Add review guidance and guard tests for this bug family.

## Done

- PR #344 created and pushed on `fix/admin-scope-chrome-window`.
- Parallel agent audits reviewed query-preservation and false-park-scope route
  families; findings were folded into the PR.
- Guard tests cover complete `from`/`to` preservation, one-sided half-window
  cleanup, repeated query-param preservation, module SOP suppression, nested
  route-family suppression, and known local/no-park pages.
- Admin-web scoped agent guidance now requires this review lens for future
  shell/top-bar scope work.

## Proof So Far

- `npm --prefix apps/admin-web ci` completed setup earlier in this worktree;
  Node 23 emitted the expected warning because the package requests Node 24.
- `cd apps/admin-web && node --test --experimental-strip-types features/sops/publish-closes-editor.test.mjs features/verification-review/local-drawer-navigation.test.mjs features/procurement/sales-format.test.mjs` passed: 33 tests, 33 pass.
- `git diff --check` passed.

## Known Gaps

- Browser before/after screenshots are not captured yet. The shared local stack
  refuses feature branches by design, and the isolated stack could not start
  because local Postgres at `127.0.0.1:5433` refused connections.
- No staging deploy has run.

## Landing State

- Candidate before landing receipt: `47f3d1f2f`.
- First `make land-main` attempt was interrupted before completion after the
  local-CI classifier incorrectly selected Android for an admin-web/test-only
  diff.
- Classifier root cause: `apps/admin-web/features/verification-review/*.test.mjs`
  matched the broad verification proof-media fanout. Runtime verification-review
  source still fans out to Android; `.test.mjs` files now stay admin-web-only.
- Second `make land-main` attempt correctly selected `common,admin-web`, then
  failed before push on three guard issues:
  - `sales-pages-guard` still expected the retired
    `PAGES_OWNING_PARK_SCOPE` shell constant.
  - `android screenshot scope self-test` used `printf | grep -q` under
    `pipefail`, producing false negatives when `grep -q` exited early.
  - `screenshot remediation guard self-test` had the same pipefail matcher
    issue.
- Focused repairs are applied locally. Direct reruns are green for
  `check-sales-pages.mjs`, `check-android-screenshot-scope.test.sh`,
  `check-screenshot-remediation.test.sh`, `ci-scope.mjs --self-test`,
  the focused admin-web unit tests, and `git diff --check`.
- Next step: commit/push the guard repairs, then rerun `make land-main`.
