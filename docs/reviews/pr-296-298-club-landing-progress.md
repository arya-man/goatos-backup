# PR 296 + PR 298 Club Landing Progress

Date: 2026-09-18

## Scope

- Club PR 296 (`fix(vendors): short duplicate-vendor refusal, shown verbatim on the phone`) with PR 298 (`feat(feed-analytics): Status-wise view on the Consumption tab -- spend and kg per animal per pen tag`).
- Land the combined candidate to `main` only after the local landing receipt passes from a clean isolated worktree.

## Done

- Created isolated worktree at `/Users/raviteja/mesha/.landing-worktrees/club-pr296-pr298` from `origin/main`.
- Cherry-picked PR 296 and PR 298 in order with no conflicts.

## Pending

- Run focused backend/admin-web/Android checks appropriate to the combined surface.
- Run final `make land-main` after this progress note is committed and the candidate is clean/rebased on current `origin/main`.
- Confirm `origin/main` readback after the landing gate pushes.

## Tests / E2E Performed

- Pending in this candidate.

## Known Failures

- None observed in this candidate yet.
- PR 298 notes that `backend/internal/feeddirection/adapters/postgres/pen_tag_analytics_integration_test.go` needs an OCI throwaway database; do not treat that test as a local fake-db receipt.

## Before / After Metrics

- Not a latency/performance landing; no before/after API latency metric is claimed here.

## Judge Status

- PR 296 review found no code findings; real-phone/STG banner proof was not run before clubbing.
- PR 298 browser proof is taken from the PR body until revalidated locally or by `make land-main`.

## Current SHA

- Base before clubbing: `origin/main` = `6e7d3f66aff8bc53d1b64b5ffd12624351280f68`.
- Club candidate before this progress note: `aed3d104a`.

## Deployment State

- No merge, push to `main`, staging deploy, APK distribution, or final smoke has happened for this club candidate yet.
