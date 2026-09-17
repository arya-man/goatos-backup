# Mobile WebView Loadwise Chart Labels - 2026-09-18

## Scope

Fix the sales load-wise mobile WebView chart rendering where missing series slots
can show visible dash/fallback marks instead of cleanly hiding the absent bar,
most visibly on the unsold-load fattening chart.

## Done

- Reproduced the code path: admin-web `GroupedColumns` renders load-wise price
  and fattening charts shown inside the Android/mobile WebView.
- Identified that null/zero series entries still emit a `.gcbar.none` element,
  leaving a visible fallback slot in some WebView/mobile captures.
- Patched empty grouped-column slots to be explicitly hidden from paint while
  preserving their layout slot, so the visible sibling bar stays aligned.
- Added focused admin-web component tests for the WebView paint guard.
- Added reusable local-CI guards for sibling bar/column primitives so all-zero
  stacked columns and simple SVG columns return their empty state instead of
  rendering blank chart scaffolding on mobile.
- Normal local admin-web startup was blocked in the original dirty checkout by
  the repo guard because that checkout was not `origin/main`, and the DB
  mutation guard also refused to prepare a non-verified local DB. No DB bypass
  was used.
- Captured and visually verified a same-DOM/same-CSS mobile fixture in the
  original checkout: vendor names are visible and the lower chart renders `21`,
  `19`, and `24` with no `----` artifact.
- Captured and visually verified before/after screenshots in the isolated
  worktree:
  - Before: `artifacts/mobile-webview-chart-labels/loadwise-chart-before.png`
  - After: `artifacts/mobile-webview-chart-labels/loadwise-chart-after.png`
- While certifying with `make land-main`, repaired local hard-gate blockers
  without changing Android build scripts: `.repowise` was rebuilt with
  `make ai-setup`, backend fast CI rerun passed, corrupt Gradle caches were
  quarantined, and the Android leg was rerun with JDK 21 plus an isolated
  `GRADLE_USER_HOME`/`TMPDIR`.

## Pending

- Amend this progress update, then run the final `make land-main` receipt from
  the isolated clean worktree.
- Real `/sales/loads` WebView/device screenshot after the change is deployed or
  a verified disposable local DB is available.

## Proof

- User screenshots show `----` over the lower load-wise chart and missing/poor
  labels in the mobile WebView view.
- Focused tests passed in the original checkout:
  `cd apps/admin-web && node --test --experimental-strip-types components/grouped-columns.test.mjs features/procurement/sales-format.test.mjs`
- Mobile fixture proof from the original checkout:
  `artifacts/mobile-webview-chart-labels/loadwise-chart-fixture-mobile-v2.png`
- Before/after proof from the landing worktree:
  `artifacts/mobile-webview-chart-labels/loadwise-chart-before.png`
  and `artifacts/mobile-webview-chart-labels/loadwise-chart-after.png`
- Focused reusable chart guard tests passed:
  `cd apps/admin-web && node --test --experimental-strip-types components/grouped-columns.test.mjs components/svg-column-bars.test.mjs components/svg-series.test.mjs features/procurement/sales-format.test.mjs`
- Android direct gate passed with JDK 21 and isolated Gradle cache/temp:
  `cd apps/goatos-android && ./gradlew :app:compileStgReleaseKotlin :app:testStgReleaseUnitTest :app:lintStgRelease --stacktrace`

## Current SHA

Isolated worktree starts from `origin/main` at
`36b98fd53058b14326ea8e134ea97fb27d8db20a`.

## Deployment State

Local only for this chart fix. No merge, push, or staging deploy yet.
