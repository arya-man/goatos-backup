# PR 267 Landing Progress

Scope: land PR 267 (`fix/admin-web-in-place-feedback`) to `main`.

## Scope

- Remove the admin-web Sales pipeline/evidence surfaces requested on 2026-09-15.
- Keep Market Survey config writes in place instead of redirecting to the top of `/sales/config`.
- Make SOP publish close the editor and return to the module library with a visible published-version banner.
- Revalidate every module SOP route served by the sidebar after SOP mutations.

## Done

- Reviewed current PR head `a9250a014`.
- No blocking review finding found in the PR pass.
- Verified the Sales guard, proof-media egress guard, and backend admin UI package tests.
- Verified targeted Market Config and SOP publish tests directly.
- Added this landing progress note and committed it on top of the PR branch.
- First `make land-main` attempt reached local CI but failed on `agent: ai-doctor` because `.repowise` index files were missing.
- Rebuilt the local `.repowise` index with `make ai-rebuild-repowise`.
- Re-ran `bash tools/agent-hooks/ai-doctor.sh` and it passed.
- Second `make land-main` attempt passed the local CI gate and pushed the certified SHA to `main`.

## Pending

- None for main landing.

## Tests And Guards Performed Before Landing

- `node --test --experimental-strip-types apps/admin-web/features/procurement/market-config.test.mjs apps/admin-web/features/sops/publish-closes-editor.test.mjs` — passed, 7/7.
- `node tools/agent-hooks/check-sales-pages.mjs` — passed.
- `ADMIN_WEB_PROOF_MEDIA_EGRESS_BASE=origin/main node tools/agent-hooks/check-admin-web-proof-media-egress.mjs` — passed, 13 files checked.
- `go test ./internal/adminui/app` from `backend` — passed.
- `git diff --check origin/main...HEAD` — passed.
- First landing attempt: `make land-main` — failed at `agent: ai-doctor`; all shown backend/admin-web/android build and test steps passed, but no valid push receipt was produced because local CI was red.
- Repair: `make ai-rebuild-repowise` — passed.
- Repair verification: `bash tools/agent-hooks/ai-doctor.sh` — passed.
- Second landing attempt: `make land-main` — passed. Local CI selected `common,backend,admin-web,android`, recorded a green all receipt for `eaea3e0e3f17`, pushed `HEAD -> main`, and verified `origin/main` is `eaea3e0e3f17`.

Known local limitations before landing:

- `npm --prefix apps/admin-web run typecheck` could not run in the isolated worktree before dependency install because `tsc` was not present in local `node_modules`.
- Running the package `test` script with explicit files triggered the full admin-web suite because of package script expansion; two unrelated existing tests failed there (`features/responsive-viewport-guard.test.mjs`, `features/verification-review/review-events.test.mjs`). The PR-specific tests were rerun directly and passed.

## Current State

- Candidate SHA before first landing gate: `b30d0d5e7`.
- Candidate SHA after recording ai-doctor repair: `eaea3e0e3f17`.
- Landed `main` SHA: `eaea3e0e3f17`.
- Deployment state: not deployed; this entry is for main landing only.
