# Dashboard Automation Setup Progress - 2026-09-21

Scope: implement the first guarded repo-side slice for post-main and daily dashboard automation.

Done:

- Added automation config with desktop/mobile viewports, known failure strings, OCI free-tier limits,
  and agent budget policy.
- Added route inventory discovery that compares admin-web filesystem routes with the smoke inventory.
- Added dry-run/production-smoke runner that writes redacted receipts and fails closed on missing auth.
- Added OCI free-tier/headroom preflight. Non-dry runs now require explicit Always Free classification.
- Added guardrail target and self-test so new routes cannot silently skip smoke coverage.
- Added runbook for OCI enablement and Anthropic/agent handling.
- Rebased onto current origin/main and repaired the newly enforced PostgreSQL bind-contract guard by
  wrapping remaining task/toxin dynamic SQL call sites with `sqlbind.MustBind`; the shrink-only
  baseline now ratchets that file down.

Pending:

- Install the OCI host timer/service.
- Wire host-specific read-only dashboard auth.
- Add host adapter for actual agent API calls with measured token/cost accounting.
- Run three report-only lifecycle receipts before enabling unattended notification.

Latest proof:

- `make dashboard-automation-guard`: PASS.
- `node tools/dashboard-automation/run-dashboard-automation.mjs --mode production-smoke --dry-run`: PASS.
- `GOATOS_DASHBOARD_OCI_FREE_CLASSIFICATION='Paid' node tools/dashboard-automation/preflight-oci-free.mjs --require-oci`: expected FAIL, proving paid/non-free config is rejected.
- `node --test apps/admin-web/scripts/smoke-visual-route-coverage.test.mjs`: PASS.
- `node tools/ci/check-guardrail-registration.mjs`: PASS.
- `make postgres-bind-contract-guard`: PASS.
- `cd backend && go test ./internal/tasks/adapters/postgres ./internal/toxin/adapters/postgres`: PASS.
- `cd apps/goatos-android && LINT_PRINT_STACKTRACE=true ./gradlew :app:lintStgRelease --no-daemon --console=plain --no-configuration-cache --max-workers=1 -Dkotlin.compiler.execution.strategy=in-process -Dkotlin.daemon.enabled=false -Pkotlin.compiler.execution.strategy=in-process`: PASS.

Judge state: requested Codex judge tasks could not run because the account hit the Codex usage limit
before they started. The repo-side guardrails remain local and deterministic; agent review is optional
and budget-capped in config, not required for normal smoke execution.

Promotion state: repo changes are local until `make land-main` passes after final review.
