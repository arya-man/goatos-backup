# Health Config Editor Main Landing Progress - 2026-09-17

## Scope

- Admin-web Health Config only.
- Make Edit open a full-screen editor route/state instead of burying the editor below the catalog.
- Preserve filtered catalog URL when returning from the editor.
- Avoid catalog API fanout while an editor `hc_version` route is selected.
- Recover cleanly from stale `hc_version` values.
- Keep direct-opened editor URLs safe: Back must not navigate to unrelated browser history.

## Done

- Split Health Config list and editor route states.
- Editor route fetches only the selected protocol detail.
- Catalog route fetches only the bounded catalog page.
- Added a Back-to-list control with a one-time, history-entry-bound nonce so only the exact catalog-opened editor entry may use `router.back()`.
- Direct opens, reloads, stale markers, and non-matching entries fall through to the safe filtered list href.
- Removed the unreachable stale-version alert from the catalog render branch.
- Preserved list filters/cursor when opening editor and when returning to list.

## Pending

- Rerun exact local landing gate from this clean worktree.
- Push to `main` only after `make land-main` passes.
- No staging or mobile deployment requested in this turn.

## Tests / Evidence

- Clean landing worktree focused validation passed:
  - `cd apps/admin-web && node --test features/health/health-config-editor-navigation.test.mjs`
  - `cd apps/admin-web && npx eslint features/health/health-config.tsx features/health/health-config-editor.tsx`
  - `cd apps/admin-web && npm run typecheck`
  - `git diff --check -- apps/admin-web/features/health/health-config.tsx apps/admin-web/features/health/health-config-editor.tsx apps/admin-web/features/health/health-config-editor-navigation.test.mjs docs/runbooks/health-config-editor-main-progress-2026-09-17.md`
- After the first landing failure, direct blocker checks passed:
  - `make ai-doctor`
  - `bash tools/agent-hooks/check-boundaries.sh`
- First eslint attempt in the clean worktree failed before dependency install because `eslint-config-next` was missing; `npm ci` completed and the rerun passed.
- Earlier screenshot artifacts were visually checked under `artifacts/health-config-editor-screenshots/`, but they are not part of this scoped main patch.

## Known Failures

- First `make land-main` attempt on candidate `e9c0d0aba1ab6fc63371c2c7601b31f446ba7d79` failed before push:
  - `agent: ai-doctor` because this fresh worktree lacked `.repowise` setup.
  - `agent: boundaries` because internal marker names used the legacy `goatos` prefix.
- Marker names were changed to Mesha-neutral keys.
- `make ai-setup` generated the local `.repowise` / `.code-review-graph` indexes; generated editor/MCP files were kept out of the commit.
- The direct `make ai-doctor` and boundary guard reruns passed after those fixes.

## Before / After Metrics

- Before: selected editor URL fetched catalog plus detail, and the editor rendered below the catalog.
- After: selected editor URL fetches detail only; catalog is not fetched behind editor mode.
- Quantitative API latency gate pending local landing checks.

## Judge Status

- Independent reviewer agent re-reviewed the nonce-based Back behavior and found no remaining Back-marker bug.
- Residual safe edge noted: a click before `useEffect` binds the nonce falls through to the list href instead of scroll-preserving history.

## Current SHA / Deployment

- Clean worktree base before commit: `76ab6e2de747e44f0308988b1dc7e5c549b2c540` (`origin/main` at worktree creation).
- Commit SHA: pending after final amend.
- Deployment: not started.
