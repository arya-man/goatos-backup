# PR 374 migration isolation fixes

Scope: fix review findings in migration 000394; push only to PR 374.
Base SHA: 821bd1b2549bc4e033f8905d7342cf3cbc81704f.

Done: restrict leaver revocation to the resolved tenant and inactive member;
persist exact grant, pending-grant and designation identities and prior values
for rollback. Preserve intentional access revocations and cleaning reductions.

Before: unchanged SQL reproduced cross-tenant leaver revocation and rollback
of an unrelated same-name manager. After: full-schema PostgreSQL regression PASS; unrelated tenant unchanged, renamed
original restored, later same-name hire and new grant preserved.
Tests: prior focused permissions/workforce/adminui Go tests and frontend tests passed.
Tests: TestOperatorRetirementTenantIsolationAndExactRollback PASS with native
PostgreSQL 16 via GOATOS_PGTEST_ADMIN_DSN. Original Up fails on cross-tenant
revocation; original Down fails to restore renamed original. Final fixed SQL
rerun PASS. Focused Go suites rerun PASS. Bind-contract, seed-migration,
git-identity and diff checks PASS.
Implementation SHA: 6f8e7b12a27a18aa299588cba51b22c45107cac0.
Pending: PR push and remote SHA readback.
Known failures: initial fixture omitted required valid_from; fixed and rerun.
AI Doctor initially lacked local indexes. CRG built successfully. Repowise
full and fresh fast indexing were stopped after extended local analysis.
`REPOWISE_SETUP=0 make ai-doctor` PASS using the supported optional-tool
exclusion; Repowise analysis is NOT certified. No code gate was disabled.
validate-migrations reports existing unrelated migration findings; no 000394 finding.
 responsive guard lacks authenticated target configuration; no
laptop/phone rendering proof. No UI changes in this follow-up. Full ci-local not run.
Judge status: self-review complete for the two migration findings. Deployment state: none; no main promotion.

Exact checks (from backend unless stated):
- `GOATOS_RUN_POSTGRES_TESTS=1 GOATOS_PGTEST_ADMIN_DSN=<disposable-local-admin-dsn> go test ./migrations/postgres -run TestOperatorRetirementTenantIsolationAndExactRollback -count=1 -v`
- `go test ./internal/permissions/... ./internal/workforce/app ./internal/adminui/app`
- Root: `node --test apps/admin-web/features/feed/feed-follow-up-dates.test.mjs apps/admin-web/features/leadership-tasks/tasks-phone-viewport.test.mjs`
- Root: `make postgres-bind-contract-guard seed-migration-guard git-identity-guard`
- Root: `git diff --check`

No performance claim: this is a bounded migration correctness repair. No latency
endpoint, payload, frontend or running-service behavior was changed by this follow-up.

Additional check: `go test ./migrations/postgres` PASS (database cases are
opt-in; the new regression was separately run with PostgreSQL enabled).
