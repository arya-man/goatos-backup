# Animal Purchase Preview Landing Progress

Date: 2026-09-15

## Scope

- Restore animal-purchase tile previews in admin-web without reintroducing proof-media GCS egress spikes.
- Show actual image previews for photo proof slots.
- Show video thumbnails only when the backend already has a thumbnail/poster derivative; do not probe or stream original videos to create posters.
- Gate preview loading to visible/near-visible animal rows with a 320 px lookahead instead of loading every media item on the page.
- Extend the egress guard so future regressions cannot attach thumbnail/poster URLs to media tags.
- Keep Android animal-purchase cards from falling back to original MP4 URLs while still allowing backend-provided thumbnail URLs.

## Done

- Added optional `thumbnail_url` to animal-purchase media slot API contract and generated app client.
- Backend media resolver now returns photo proof route previews and video poster/thumbnail metadata only when already present and safe: image extension only, not signed URLs, not proof download routes, and not Google storage/API hosts.
- Admin-web rewrites both `media_url` and `thumbnail_url` through the same same-origin proof proxy when the backend returns proof download routes.
- Animal-purchase lightbox now attaches preview `<img>` URLs only after `IntersectionObserver` marks the animal row visible or near-visible with a 320 px lookahead band.
- Tile previews never use `<video>` or `<source>` and original video URLs remain tap/open only.
- Android DTO/viewmodel now consume `thumbnail_url`; video cards use that thumbnail when present and otherwise show no remote preview instead of binding the MP4.
- Proof-media egress guard self-test now covers thumbnail URL usage and cannot be bypassed by a nearby suppression comment.
- Spun three reviewer agents after the first implementation; fixed their findings around row-level preview budget, video thumbnail URL validation, duplicate tile keys, guard bypass, and Android thumbnail parity.

## Pending

- Run full `make land-main` after final commit and rebase before pushing/merging to main.
- Deploy backend/admin-web to staging only after the landing receipt is green.

## Tests

- `node apps/admin-web/features/procurement/animal-purchases.test.mjs` - pass.
- `node tools/agent-hooks/check-admin-web-proof-media-egress.mjs --self-test && node tools/agent-hooks/check-admin-web-proof-media-egress.mjs` - pass.
- `go test ./internal/animalpurchase/adapters/proof ./internal/animalpurchase/adapters/http ./internal/animalpurchase/app` - pass.
- `./gradlew :app:testStgDebugUnitTest --tests 'sg.mesha.goatos.viewmodel.AnimalPurchaseViewModelsTest'` - pass.
- `GOATOS_FAST_LOCAL_CI=1 tools/ci/run-local-ci.sh admin-web` - pass after lint fix; covered lint, typecheck, unit tests, proof-media egress, phone viewport, request-plan, production build, and token leak.

## Known Failures

- Isolated `npm ci` cannot run because the root lockfile is not in sync with `package.json` for Lighthouse-related dependencies.
- First full `make land-main` failed before push because `.repowise` was missing, local query-plan PostgreSQL tunnel was down on `127.0.0.1:15432`, and admin-web lint caught synchronous state in the preview fallback.
- `.repowise` was repaired with `make ai-setup`.
- Admin-web lint issue was fixed and the admin-web fast job passed.
- OCI PostgreSQL tunnel was restarted for query-plan rerun, but the remote tunnel dropped/refused connections again during query-plan checks. This still blocks the full `make land-main` receipt unless the tunnel stabilizes or Ravi explicitly authorizes bypassing that gate.

## Current SHA

- Base worktree: `origin/main` at checkout time.
- Current preview-fix commit is the worktree `HEAD`; verify with `git log -1 --oneline` before promotion.

## Deployment State

- Prior PR 271 was merged to `main` and its staging Cloud Build completed successfully.
- This preview fix has not been deployed.
