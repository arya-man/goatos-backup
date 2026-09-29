# PR 455 verify proof layout landing

Date: 2026-09-29

## Scope

Land PR #455: Android verifier places the total-feed entry under the feed-weight photo, and admin-web verifier drawer shows the whole proof photo instead of a cropped slice.

## Done

- Reviewed PR #455 against the current verification/proof-media architecture.
- Verified Android focused compile and unit gate:
  `./gradlew :feature:feature-verify:testDebugUnitTest :feature:feature-verify:compileDebugKotlin`.

## Pending

- Final rebase onto current `origin/main`.
- Exact repo landing receipt with `make land-main`.
- Merge/readback after the landing receipt passes.

## Known Failures

- Broad admin-web test attempt in this isolated worktree failed because local dependencies were missing (`typescript`, `@grafana/faro-core`). This was not treated as a landing receipt.

## Current State

- PR head before landing gate: `cbce4dbf756f25a62d0257f15e068cf15b54dae7`.
- Deployment state: not deployed.
