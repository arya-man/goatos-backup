# PR 271 Landing Progress

Date: 2026-09-15

## Scope

- Land PR 271, `fix(admin-web): Animal purchases media plays again via the proof proxy; Market survey block spacing`, to `main`.
- Deploy the landed `main` SHA to Goat OS STG using the guarded local staging launcher.

## Done

- Reviewed PR 271 against the animal-purchase proof-media playback failure.
- Verified the root cause: animal-purchase media URLs were emitted as `/app/proofs/{id}/download`; when admin-web absolutized them onto the API host, browser media tags requested the route without bearer auth and received 401, leaving the video player blank at `0:00`.
- Verified PR 271 rewrites exact backend proof download routes to same-origin `/api/proof-media/{id}` for both legacy `media_url` and SOP `media_slots[].items[].media_url`.
- Verified the existing admin-web proof-media proxy resolves the proof server-side and redirects to the signed storage URL.

## Local Proof

- `node apps/admin-web/features/procurement/animal-purchases.test.mjs` passed in isolated review worktree.
- `go test ./internal/proof/adapters/http ./internal/proof/app ./internal/proof/adapters/storage/gcs` passed from `/backend` in isolated review worktree.
- `git diff --check origin/main..origin/pr/271` passed.

## Pending

- Run `make land-main` from the clean isolated landing worktree after this progress note is committed.
- After `make land-main` lands the exact certified SHA on `origin/main`, merge/close PR 271 in GitHub according to repo policy.
- Deploy STG with `GOATOS_REPO=/path/to/clean/goatos /Users/raviteja/bin/goatos-stg-deploy backend-web-mobile`.
- Verify the deployed STG SHA/revision/traffic after Cloud Build completes.

## Known Failures

- None from the focused proof-media checks.
- One adjacent plain-node test, `apps/admin-web/features/verification-review/toxin-review-render.test.mjs`, is not directly runnable with bare `node` because it imports `.ts` modules; it was not counted as PR 271 proof.

## Current State

- PR head at review time: `e27fea7e40a5dbd0276e23fef4b917ec63722341`.
- Base `origin/main` at review time: `7150ebd3cae279ac826e6db8eb99bd66e02c4084`.
- Landing worktree: `/tmp/goatos-pr271-land.VbVAsS`.
- Deployment state: not started.
