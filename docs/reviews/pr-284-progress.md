# PR 284 Progress

## Scope

- Fix review finding on PR 284: the Sales Farm value tolerance layout checker existed but was not wired into a repeatable admin-web guard.

## Done

- Added `smoke:sales-tolerance-layout:live` to `apps/admin-web/package.json`.
- Wired the checker into `responsive:guard` after `smoke-visual-live.mjs`.
- Changed the checker import to `@playwright/test`, matching existing admin-web smoke scripts and declared dev dependencies.

## Pending

- Live browser proof requires an admin-web server at `GOATOS_ADMIN_WEB_BASE_URL` or `http://127.0.0.1:3300`.
- No merge, main landing, or staging deploy has been performed.
- Current review follow-up only updates this progress receipt to match the pushed PR head.

## Tests / E2E Performed

- Passed: `node --test --experimental-strip-types apps/admin-web/features/procurement/sales-format.test.mjs apps/admin-web/scripts/smoke-visual-route-coverage.test.mjs`.
- Passed: package-script readback confirms `responsive:guard` runs `smoke-visual-live.mjs` and then `check-sales-tolerance-layout.mjs`, and `smoke:sales-tolerance-layout:live` points at the new checker.
- Passed in the review worktree at `78df56445a23c2e165a37a8958e8ff9223deb34c`: `node --check apps/admin-web/scripts/check-sales-tolerance-layout.mjs`.
- Passed in the review worktree at `78df56445a23c2e165a37a8958e8ff9223deb34c`: `git diff --check origin/main...HEAD`.

## Known Failures

- Earlier broad `npm --prefix apps/admin-web test -- ...` invocation expanded the full test suite and hit unrelated missing-module failures in this isolated worktree.

## Metrics

- No performance metric changed; this is guard wiring for a layout regression.

## Judge Status

- Review finding fixed in source; focused verification passed; receipt updated for PR head.

## Current SHA

- PR head under review: `78df56445a23c2e165a37a8958e8ff9223deb34c`.

## Deployment State

- Not deployed.
