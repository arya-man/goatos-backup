# PR 379 Growth Shifting Landing Progress

- Scope: land PR 379 to `main`; backend counts/identity shifting growth rules only. No staging deploy requested.
- PR head before landing: `dc795486f7c6c23ee5ace3c16a6dbaafcb9f7bb3`.
- Current `origin/main` before landing: `60e183df0a88b6e719d2dcd17b80cf4314c3163c`.
- Done: reviewed PR 379; no blocking findings found. Focused tests passed:
  - `cd backend && go test ./internal/counts/domain ./internal/counts/adapters/http ./internal/counts/adapters/postgres`
- First `make land-main` attempt at `cddd3618b766261ae0fbc40bd1362806249c3bba` failed before push:
  - `dashboard-automation-guard`: clean-main coverage/lane ledger drift from already-landed commits.
  - `go test ./... (Postgres disabled)`: failed in the parallel landing run, but immediate rerun `cd backend && GOATOS_RUN_POSTGRES_TESTS=0 go test ./...` passed.
  - `android config-cache guard self-test`: failed in the parallel landing run, but immediate rerun `tools/ci/check-gradle-config-cache.test.sh` passed.
- Repair after first attempt: ran `node tools/dashboard-automation/sync-coverage.mjs --write`; it parked new `origin/main` commits as `needs-assertion` / `needs-lane` metadata and did not add executable smoke assertions. `node tools/agent-hooks/check-dashboard-automation-guard.mjs` then passed.
- Second `make land-main` attempt at `03c4da0cd8a6e0dc75bb3a7ba6699452c196bb97` was interrupted before push after user review of the forced Android scope. The Android run was selected because `tools/agent-hooks/postgres-bind-contract-baseline.json` matched the broad `tools/agent-hooks/` force-full prefix, even though no Android source was touched.
- CI scope repair: add a narrow force-full exception for `tools/agent-hooks/postgres-bind-contract-baseline.json`, map that baseline to backend, and pin dashboard metadata receipts to common-only. Keep executable `tools/agent-hooks/*` guard scripts force-full.
- Scope proof after repair:
  - `node tools/ci/ci-scope.mjs --self-test`
  - `node tools/ci/ci-scope.mjs --base origin/main --head HEAD --format json` selected only `common,backend`; `adminWeb=false`, `android=false`, `full=false`.
- Landing receipt after repair: `make land-main` passed at `9bae53f1373f5779f46714803fa6367bddfe63f3`, scoped to `common,backend`, and pushed that SHA to `main`.
- Known failures: none from focused review tests. First landing attempt exposed pre-existing/forced-full-scope gate drift; stable dashboard guard drift is repaired by parked metadata, and the unrelated Android selection is repaired by CI scope mapping.
- Deployment state: pushed to `main` at `9bae53f1373f5779f46714803fa6367bddfe63f3`; not deployed to staging.
