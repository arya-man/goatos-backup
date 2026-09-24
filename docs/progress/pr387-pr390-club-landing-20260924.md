# PR 387 + PR 390 club landing progress

Date: 2026-09-24

## Scope

- Club PR 387 (`feat/sale-price-stage-sex`) and PR 390 (`fix/vaccination-grid-json-vaccine-source`) into one branch.
- Push the replacement branch, create a replacement PR, close the superseded PRs, and land the certified result to `main`.

## Done

- Created isolated worktree: `/Users/raviteja/mesha/goatos-club-pr387-pr390-20260924`.
- Started from current `origin/main` at `5448b81ea8c77ba7135fd9c19c89ee31f8273de3`.
- Replayed all PR 387 commits onto current `main`.
- Replayed PR 390 head `a05a3ffdff6f27efe88cf18cf435e3ff18be4101` on top.
- Club branch is `club/pr387-pr390`.
- Pushed `club/pr387-pr390` to origin.
- Created replacement PR: https://github.com/vgoats/goatos/pull/392.
- Closed PR 387 and PR 390 as superseded by PR 392.
- Ran `make land-main`; it blocked before main push with three hard failures.
- Fixed generated API client drift from the OpenAPI description.
- Renumbered the sale-price stage/sex migration from `000398` to `000399` because current main already has `000398_herd_signals_realtime_motion.sql`.
- Renamed the FCR effective-date fallback test to include `DateShift` so aggregate projection guard sees the date adversarial coverage.
- Focused reruns passed: `check-contract-drift.sh`, `make aggregate-projection-guard`, and `make migration-duplicate-versions-guard`.
- Final `make land-main` passed and pushed `01095633a815ba34b3dce301a48a21269bc6d086` to `main`.
- Readback after landing matched: local `HEAD`, local `origin/main`, and remote `refs/heads/main` were all `01095633a815ba34b3dce301a48a21269bc6d086`.
- GitHub PR 392 is marked merged.

## Pending

- None.

## Tests / E2E

- `make land-main` ran and failed before main push.
- Passing lanes included backend `go test ./...` with Postgres disabled, required PostgreSQL query plans, command-board query plans, admin-web lint/typecheck/unit tests, and admin-web production build.
- Failed lanes: `agent: contract-drift`, `agent: aggregate-projection`, `migration-duplicate-versions-guard`.
- Focused reruns after fixes passed for all three failed lanes.
- Final `make land-main` passed at `01095633a815ba34b3dce301a48a21269bc6d086`.
- PR 390 focused backend tests passed earlier in isolated review worktree; must be rerun or covered by final landing gate for this clubbed branch.

## Known failures

- None for the landed code. This file is being updated after the landing receipt.

## Current SHA

- Landed code SHA: `01095633a815ba34b3dce301a48a21269bc6d086`.
- Final docs-only receipt update was landed after the code landing; read current `main` for the latest documentation-only SHA.

## Deployment state

- No staging deployment started.
- Main push completed by `make land-main`.
