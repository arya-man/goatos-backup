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

## Pending

- Run focused backend health tests after the final edit.
- Commit the fix and progress note.
- Rerun focused backend health tests after guard fixes. Done: passed.
- Rerun the two previously failing guards against the amended candidate. Done: passed.
- Commit the guard fixes. Done: amended into `333d7c96a`.
- Rebase or recreate candidate on current `origin/main`.
- Rerun the required local landing receipt (`make land-main`) from a clean worktree.
- Push to `main` only after the landing receipt passes.

## Evidence

- Review-only focused test before fix: `go test ./internal/health/...` from `backend` passed at PR SHA `8c3aa11f3895a1e4fb967e82a3fa7891610ecd7a`.
- Fix-focused test: `cd backend && go test ./internal/health/...` passed after `gofmt`.
- Failed landing attempt: `make land-main` at candidate `05e6f57d7f8ba67895a6a13035c41c7e86f82a39`; no push occurred.
- Guard-fix focused test: `cd backend && go test ./internal/health/...` passed after binding updates.
- Guard-fix checks: `node tools/agent-hooks/check-leadership-assistant-coverage.mjs` passed; `make postgres-bind-contract-guard` passed.

## State

- Current base before fix: PR 365 SHA `8c3aa11f3895a1e4fb967e82a3fa7891610ecd7a`.
- Deployment: none.
- Main push: not done.
