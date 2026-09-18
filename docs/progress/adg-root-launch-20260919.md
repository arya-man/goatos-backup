# ADG Root Launch Redirect Progress - 2026-09-19

## Scope

- Fix `https://dashboard.mesha.sg/` launching through multiple ADG Analytics redirects.
- Preserve ADG query filters when root redirects to `/weighing/analytics`.
- Use the same DB-served weighing window settings that ADG Analytics uses.

## Done

- Verified live staging/public API was already deployed past the earlier loading-boundary fixes: `/version` reported `dcbe8750bcbb`, migration `000357#1`.
- Reproduced root launch in Chrome: `/` landed via `/weighing/analytics?scope_mode=company` and then canonicalized to dated `wt_from` / `wt_to`.
- Updated root landing to compute the ADG landing window before redirecting.
- Added `landing-href.mjs` so both undated and already-dated root URLs preserve the full ADG query: `sex`, `origin`, `weighing`, `tab`, pagination and table filters, plus `wt_from` / `wt_to`.
- Added root-route coverage for both no-window and already-windowed root URLs.

## Proof So Far

- `node --test apps/admin-web/features/root-route/admin-root-route.test.mjs`: PASS.
- `node --test apps/admin-web/features/weighing/weights-window.test.mjs`: PASS.
- `npm run typecheck` from `apps/admin-web`: PASS.
- `git diff --check` for touched files: PASS.
- First `make land-main` attempt failed before push on `agent: boundaries` because `page.tsx`
  deep-imported `weightsWindowSettings` from the Weighing feature internals. The helper is now
  exported through `@/features/weighing`, and the route imports it from the public entrypoint.

## Pending

- Commit this progress note and code.
- Rerun exact landing gate from this clean worktree with `make land-main`.
- Push to `origin/main` only if `make land-main` produces a green exact-SHA receipt.

## State

- Base SHA: `7ab4b8c20b4b04f1a19946bb5110091d6176c09a` (`origin/main` at worktree creation).
- Deployment state: no new deploy from this fix yet.
- Push state: not pushed yet.
