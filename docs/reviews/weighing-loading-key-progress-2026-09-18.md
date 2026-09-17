# Weighing Loading Key Follow-up Progress

Last updated: 2026-09-18 02:03 IST

## Scope

- Fix the review finding on the landed weighing loading skeleton: duplicate React
  keys in `/weighing/loading.tsx` caused by using `key={width}` with repeated
  `96` widths.
- Keep the existing admin shell and route-level loading-boundary behavior
  unchanged.

## Done

- Updated the skeleton tab map to include the item index in the React key.
- Added a focused static guard in `features/weighing/weights-window.test.mjs` so
  the weighing loading route keeps the index-qualified key and cannot silently
  regress to duplicate width keys.

## Local Evidence

- Focused admin-web test command passed:
  `cd apps/admin-web && node --test --experimental-strip-types components/admin-layout-loading.test.mjs features/weighing/weights-window.test.mjs`.
- Result: `35` tests passed.

## Pending

- Commit the local fix.
- Run the repo landing gate with `make land-main` from this clean worktree.
- Push to `main` only if the landing gate passes.

## Current State

- Starting SHA before this follow-up: `ea2702fd600e34042e80e8bd526c3ab2722a0873`.
- Before commit, this worktree matched `HEAD == origin/main == origin/HEAD ==
  ea2702fd600e34042e80e8bd526c3ab2722a0873`.
- Staging has not been deployed for this follow-up.
