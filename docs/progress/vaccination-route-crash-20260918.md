# Vaccination Route Crash - 2026-09-18

## Scope

- Investigate `/vaccination?scope_mode=company` generic admin-web route crash shown in Ravi's screenshot at 2026-09-18 07:32 IST.
- Check last one month commits for repeated vaccination/admin-web fixes and repair the root cause without disturbing unrelated dirty checkout work.

## Current State

- Branch: `fix/admin-sidebar-switch-lag`
- Starting SHA: `c09c95643`
- Landed SHA: `e3a5e4116dc649fe95a7d687cdb8283f3621368d`
- Screenshot failure: admin shell loads, `/vaccination?scope_mode=company` content throws generic "Something went wrong"; error reference `61156052`.
- Last-month scoped git scan found 277 commits matching vaccination/admin-web/route/contract/cache/perf/error terms under admin-web vaccination/backend contract paths.

## Done

- Confirmed the active checkout is dirty before edits.
- Began focused inspection of the vaccination route, command board, shared admin UI contract, and recent route-boundary fixes.
- Confirmed live Cloud Run error for digest `61156052`: `Admin-web page contract vaccination missing copy key inventory_progress.title` on `goatos-admin-web-stg-00463-zq8`, commit label `06bb3baf3abb`.
- Added backend page-contract copy for both `inventory_progress.title` and `section.inventory_progress.title`.
- Added frontend stale-contract fallback for `inventory_progress.title`.
- Removed unused strict `drive_steps` read from the vaccination operations route render.
- Added regression assertions for the live missing-copy key and stale option-group read.
- Strengthened the admin UI literal guard so frontend fixed `copy`, `actionFeedbackCopy`, `optionGroup`, `optionLabel`, `tableLabels`, `tablePageSizes`, and throwing `control` literal references must have a backend `AdminWebPageContract` producer or an explicit stale-contract fallback; fallback helpers such as `controlEnabled(..., false)` stay allowed.
- Follow-up review fix: tightened the guard so fixed contract references are checked from production `service.go` plus explicit frontend fallbacks, not from `*_test.go` fixtures or arbitrary backend maps; confident vaccination-route files now validate copy keys against common copy plus the vaccination route instead of accepting any route's copy key.
- Added stale-contract table fallbacks for vaccination `status-matrix`, `cohort-detail`, and `shed-events`, plus `shed-execution` `shed-events`, so older backend contracts do not crash route renders.
- Moved the verification review video play label behind backend-owned contract copy (`player.play_proof`) and added fallback/test coverage.

## Pending

- After deploy, rerun browser smoke on `https://dashboard.mesha.sg/vaccination?scope_mode=company` and fail on `Something went wrong`, `Admin-web contract unavailable`, `backend_down`, `The board could not be loaded`, and `Weights could not be loaded`.

## Verification Log

- PASS: `node --test --experimental-strip-types apps/admin-web/lib/admin-ui-contract.test.mjs apps/admin-web/features/preventive-care-vaccination/command-board-scope.test.mjs`
- PASS: `(cd backend && go test ./internal/adminui/app)`
- PASS: `npm --prefix apps/admin-web run check:ui-contract`
- PASS: `npm --prefix apps/admin-web run check:mock-fidelity`
- PASS: `node --test --experimental-strip-types apps/admin-web/scripts/check-ui-contract-literals.test.mjs apps/admin-web/lib/admin-ui-contract.test.mjs apps/admin-web/features/preventive-care-vaccination/command-board-scope.test.mjs`
- PASS: follow-up guard review fix: `cd apps/admin-web && node scripts/check-ui-contract-literals.mjs`

## Deployment State

- `make land-main` passed and pushed certified SHA `e3a5e4116dc649fe95a7d687cdb8283f3621368d`; local `HEAD` and `origin/main` matched that SHA during review.
- Live readback: `goatos-admin-web-stg-00463-zq8`, 100% traffic, commit label `06bb3baf3abb`.
