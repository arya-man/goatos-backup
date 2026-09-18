# PR 296 + PR 298 Club Landing Progress

Date: 2026-09-18

## Scope

- Club PR 296 (`fix(vendors): short duplicate-vendor refusal, shown verbatim on the phone`) with PR 298 (`feat(feed-analytics): Status-wise view on the Consumption tab -- spend and kg per animal per pen tag`).
- Land the combined candidate to `main` only after the local landing receipt passes from a clean isolated worktree.

## Done

- Created isolated worktree at `/Users/raviteja/mesha/.landing-worktrees/club-pr296-pr298` from `origin/main`.
- Cherry-picked PR 296 and PR 298 in order with no conflicts.
- Added this progress receipt and committed it as `48754b76e`.
- Focused backend, API client, admin-web, and Android vendor checks passed.
- First `make land-main` attempt ran the selected `common,backend,query-plans,admin-web,android` local CI scope at `c2a53eba2`; every reported step passed except `agent: ai-doctor`.
- Repaired the isolated worktree's local AI indexes with `make ai-setup`; the setup command finished by running `make ai-doctor`, which passed with `.repowise index current enough (c2a53eba2..., mode=fast)`.
- Rebasing onto `origin/main` changed the candidate SHA to `272b8ed00`; `make ai-doctor` correctly failed stale, then `make ai-rebuild-repowise && make ai-doctor` passed with `.repowise index current enough (272b8ed00..., mode=fast)`.

## Pending

- Commit this final repair receipt update.
- Rerun final `make land-main` from the clean, rebased candidate.
- Confirm `origin/main` readback after the landing gate pushes.

## Tests / E2E Performed

- `cd backend && go test ./internal/procurement/app ./internal/feeddirection/domain ./internal/feeddirection/app ./internal/adminui/app ./internal/feeddirection/adapters/http` passed.
- `make api-client-check` passed; generated client output stayed clean.
- First admin-web attempt `cd apps/admin-web && npm run typecheck && node --test features/feed/feed-analytics.test.mjs` failed because this fresh worktree did not yet have `tsc` installed.
- After `cd apps/admin-web && npm install`, `npm run typecheck && node --test features/feed/feed-analytics.test.mjs` passed. The test file reported 17/17 passing. `npm install` produced only local lockfile platform churn under this Node/npm version, and that generated churn was discarded.
- `cd apps/goatos-android && ANDROID_HOME=/Users/raviteja/Library/Android/sdk ./gradlew :app:testDevDebugUnitTest --tests 'sg.mesha.goatos.viewmodel.QueuedWriteFollowTest' --tests 'sg.mesha.goatos.viewmodel.VendorsPresentationTest'` passed.
- First `make land-main` attempt failed before push on exactly one local CI step: `agent: ai-doctor`. The same receipt reported PASS for backend `go test ./...` with Postgres disabled, required PostgreSQL query plans, command-board query plans, admin-web deps/lint/typecheck/unit/build, Android `:app` compile+unit+lint, and Android benchmark compile.
- `make ai-setup` passed after rebuilding `.code-review-graph` and `.repowise`; it emitted non-fatal `repowise` health persistence warnings about SQLite variable limits, then `make ai-doctor` passed.
- After rebase onto `origin/main` `1da3a688b`, `make ai-doctor` failed stale as expected; `make ai-rebuild-repowise && make ai-doctor` passed. The repowise refresh again emitted non-fatal SQLite variable-limit health persistence warnings before the final PASS.

## Known Failures

- None observed in this candidate yet.
- PR 298 notes that `backend/internal/feeddirection/adapters/postgres/pen_tag_analytics_integration_test.go` needs an OCI throwaway database; do not treat that test as a local fake-db receipt.
- Admin-web `npm install` warned that local Node is v23.1.0 while the package requests Node 24.x; the focused typecheck and test still passed in this environment.
- `make ai-setup` generated local tool artifacts and transient tracked `.claude/settings.json` hook noise; the tracked hook noise was discarded before this receipt update.

## Before / After Metrics

- Not a latency/performance landing; no before/after API latency metric is claimed here.

## Judge Status

- PR 296 review found no code findings; real-phone/STG banner proof was not run before clubbing.
- PR 298 browser proof is taken from the PR body until revalidated locally or by `make land-main`.

## Current SHA

- Base before clubbing: `origin/main` = `6e7d3f66aff8bc53d1b64b5ffd12624351280f68`.
- Club candidate before this progress note: `aed3d104a`.
- Candidate with initial progress receipt: `48754b76e`.
- Candidate with focused-check receipt and first failed landing attempt: `c2a53eba2d1453d8db37626986492d33bbc47e5b`.
- Rebased candidate after `origin/main` advanced: `272b8ed000fcfb2913c20e1753396cbde3cf815a`.

## Deployment State

- No merge, push to `main`, staging deploy, APK distribution, or final smoke has happened for this club candidate yet.
