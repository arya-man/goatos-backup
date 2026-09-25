# Postgres Scan Projection Guard - 2026-09-25

## Scope

Prevent scanner/projection drift where a backend Postgres row scanner grows but a sibling SQL read keeps a copied private SELECT list.

## Done

- Added `tools/agent-hooks/check-postgres-scan-projection.mjs`.
- Wired `postgres-scan-projection-guard` into `Makefile`, `tools/ci/run-local-ci.sh`, and `tools/ci/guardrail-manifest.json`.
- Fixed existing copied projection drift in Verification:
  - `itemColumnsWithLabels` now derives from `itemColumns`.
- Fixed existing copied qualified projection drift in browser push:
  - `registrationColumnsQualified` now derives from `registrationColumns`.
  - `listRegistrationsSQL` uses `sqlbind.MustBind` because it is runtime-composed.

## Proof

- `make postgres-scan-projection-guard` - pass.
- `make postgres-bind-contract-guard` - pass.
- `node tools/ci/check-guardrail-registration.mjs` - pass.
- `node tools/ci/ci-scope.mjs --self-test` - pass.
- `git diff --check` - pass.
- `GOATOS_RUN_POSTGRES_TESTS=0 go test ./internal/browserpush/adapters/postgres ./internal/verification/adapters/postgres` from `backend/` - pass.
- `make land-main` - blocked before CI once because generated untracked `tmp/` proof screenshots made the worktree dirty; cleanup pending, then rerun.
- `make land-main` - blocked by an active landing lock from `/Users/raviteja/mesha/goatos-wt-runner` pid `80660`; do not kill, retry after queue clears.
- `make land-main` - rebased the candidate onto `origin/main` then was externally terminated by SIGTERM during `ci-local`; no valid receipt or push, rerun required.

## Pending

- Commit.
- Run the repo landing gate before pushing to `main`.
- Deploy staging only after landing, if still requested.

## Current State

- Worktree: `/Users/raviteja/mesha/goatos-fix-sales-farm-value-20260925`.
- Promotion: not pushed, not landed, not deployed.
