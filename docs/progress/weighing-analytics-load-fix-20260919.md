# Weighing Analytics Load Fix - 2026-09-19

## Scope

- Fix `/weighing/analytics?scope_mode=company` showing the route-level error page:
  `This page couldn't load`.
- Base branch: `fix/weighing-analytics-load-20260919` from `origin/main`.
- Keep the dirty primary checkout untouched.
- Verify against local code tests and OCI-backed local browser route before PR.

## Problem Statement

- A recent fix did not resolve the direct ADG Analytics landing route.
- Screenshot evidence shows the direct company-scope URL without `wt_from`/`wt_to`
  falling into the route error boundary instead of rendering ADG Analytics or an
  in-page load error.
- The route has an extra pre-render canonical-window step before the page body.
  Any uncaught error there, or any default-tab render error after it, can bypass
  the in-page `WeightsAnalyticsLoadError` card and show the generic route error.

## Done

- Created isolated worktree: `/Users/raviteja/mesha/goatos-weighing-analytics-load-fix`.
- Confirmed `origin/man` is not a remote branch; `origin/main` is the remote head.
- Ran focused static tests before edits:
  `cd apps/admin-web && node --test --experimental-strip-types features/weighing/weights-window.test.mjs features/root-route/admin-root-route.test.mjs`.
- Result: green, 38 tests passed.
- Started a parallel read-only review agent for recent commits/problem statement.
- Patched `/weighing/analytics` to load the admin-web bootstrap contract first,
  render an explicit contract-unavailable state instead of throwing when
  bootstrap fails, redirect unauthorized principals to the first offered page,
  and render the direct no-window ADG URL without a server-component canonical
  redirect.
- Updated `features/weighing/weights-window.test.mjs` to guard the
  bootstrap-first direct-route behavior, prevent reintroducing
  `requireAdminWebPageContract`, and prevent reintroducing the client-router
  canonical redirect on this route.
- Reproduced a browser-side route risk on the previous implementation:
  `Rendered more hooks than during the previous render` from Next's client
  Router after the direct route redirected to a dated URL.
- Removed the direct route redirect; the page renders the DB-served default
  window in place and tab links still preserve the resolved window.

## Local Evidence

- `cd apps/admin-web && node --test --experimental-strip-types features/weighing/weights-window.test.mjs features/root-route/admin-root-route.test.mjs`: green, 38 passed.
- `cd apps/admin-web && npm run typecheck`: green.
- `cd apps/admin-web && ../../node_modules/.bin/eslint features/weighing/weights-analytics.tsx 'app/(admin)/weighing/analytics/page.tsx' features/weighing/weights-window.test.mjs`: green.
- OCI-backed API probes on `127.0.0.1:18080`: `/version`,
  `/admin-web/bootstrap`, `/weighing/weighing-dates`,
  `/weighing/shed-weights`, and `/weighing/leadership/growth` returned HTTP
  200 for the local dev tenant/user.
- Browser proof against OCI-backed API/admin-web on
  `http://127.0.0.1:3319/weighing/analytics?scope_mode=company`: laptop and
  mobile passed with no page errors and no visible forbidden strings
  (`This page couldn't load`, `backend_down`, `Admin-web contract unavailable`,
  `The board could not be loaded`, `Weights could not be loaded`).
- Screenshot proof visually checked:
  `.codex-goatos-render/adg-analytics-oci-fixed-20260919204614/laptop-weighing-analytics.png`
  and
  `.codex-goatos-render/adg-analytics-oci-fixed-20260919204614/mobile-weighing-analytics.png`.
- Stock `apps/admin-web/scripts/smoke-visual-live.mjs` was not usable for this
  manual `go run` API because `/version` reports `build_sha=unknown`; the
  focused Playwright proof above used the same route and forbidden visible
  string checks.

## Pending

- Open PR.

## Current State

- Base SHA: `3bb85642a`.
- No deploy, merge, or push to main has happened.
