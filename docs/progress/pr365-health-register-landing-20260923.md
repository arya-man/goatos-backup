# PR 365 Health Register Landing Progress - 2026-09-23

## Scope

- Review finding fix for PR 365: avoid pool starvation when health observation serving reads the published authored diagnosis register.
- Preserve the PR behavior: live observations use the tenant published register when present and fall back to seeded registers when no published register exists.

## Done

- Created isolated worktree `/Users/raviteja/mesha/goatos-review-pr-365` from PR 365.
- Reviewed PR 365 against recent Health Config/register changes.
- Identified that `SubmitObservation` held a write transaction while `evaluate` called `PublishedRegister` through the pool, allowing pool-sized concurrent observations to deadlock/wait for a second connection.
- Changed diagnosis serving to read the published register on the same transaction connection and pass the authored document into evaluation.
- Ran focused health backend tests after the fix.
- First `make land-main` attempt selected `common,backend` and failed before push on `leadership-assistant-coverage-guard` and `postgres-bind-contract-guard`; backend Go tests and other backend gates shown in the receipt passed.
- Added a typed CEO AI coverage exclusion for the two diagnosis-register serving/config helpers and bound the transaction-local register SQL with `sqlbind.MustBind`.
- Bound the existing queue-location dynamic query in the same touched file and removed the now-stale postgres bind-contract baseline entry for `diagnosis_repository.go`.
- Rebased the candidate onto current `origin/main`, reran the required clean-worktree landing receipt, and pushed to `main`.

## Pending

- None for PR 365 landing.

## Evidence

- Review-only focused test before fix: `go test ./internal/health/...` from `backend` passed at PR SHA `8c3aa11f3895a1e4fb967e82a3fa7891610ecd7a`.
- Fix-focused test: `cd backend && go test ./internal/health/...` passed after `gofmt`.
- Failed landing attempt: `make land-main` at candidate `05e6f57d7f8ba67895a6a13035c41c7e86f82a39`; no push occurred.
- Guard-fix focused test: `cd backend && go test ./internal/health/...` passed after binding updates.
- Guard-fix checks: `node tools/agent-hooks/check-leadership-assistant-coverage.mjs` passed; `make postgres-bind-contract-guard` passed.
- Final landing receipt: `make land-main` passed with `common,backend,admin-web,android`.
- Final push readback: `72201cb02..d00825522 HEAD -> main`.
- Receipt doc correction landing receipt: `make land-main` passed with docs-only scope.
- Receipt doc correction push readback: `d00825522..6e6dbcc71 HEAD -> main`.

## State

- Current base before fix: PR 365 SHA `8c3aa11f3895a1e4fb967e82a3fa7891610ecd7a`.
- Landed SHA: `d008255228dd265f8f60c3a728a789bf95612e78`.
- Receipt doc correction SHA: `6e6dbcc710cb153bc8c3757d613ace5412bb43e5`.
- Deployment: none.
- Main push: done.
