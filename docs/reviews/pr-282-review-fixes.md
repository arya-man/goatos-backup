# PR 282 review fixes

Scope: resolve the three findings against PR head bd444960769ca4e06508cfd9940a75ba4ce23f4b. Push to the PR only; no main/staging promotion requested.

## Done
- A pen-feed comparison with either sheet missing reports a skipped rule with a reason.
- Rules checked counts only completed reads, excluding failed and skipped detectors.
- Empty tables distinguish incomplete checks, severity filtering, disabled rules, and completed clean checks.
- Event and overflow row identities include the park, preserving uniqueness in all-parks views.
- Regression cases cover each missing sheet, reader failure, a clean comparison, cross-park movements/overflow keys, and frontend empty-state decisions.

## Validation and pending
- Focused Go suite: `go test ./internal/alerts/... ./internal/adminui/app ./internal/permissions` passed.
- Final frontend suite: `node --test --experimental-strip-types apps/admin-web/features/alerts/*.test.mjs apps/admin-web/lib/admin-ui-contract.test.mjs apps/admin-web/scripts/smoke-visual-route-coverage.test.mjs` — 27 passed, including production TSX renders and old-backend copy compatibility.
- Race checks: `go test -race ./internal/alerts/... ./internal/adminui/app ./internal/permissions` passed.
- Node 24 frontend typecheck and ESLint for the changed frontend files passed.
- `make admin-web-phone-viewport-guard` passed: 35 self-test cases, 338 files, zero new findings.
- `git diff --check` passed.
- Pending at commit time: PR branch push and remote SHA verification. All focused code checks are green.
- Real API latency and full live-route laptop/mobile E2E are not certified. After installing dependencies, `responsive:guard` stopped because GOATOS_API_BASE_URL, GOATOS_BEARER_TOKEN and GOATOS_TENANT_ID were not configured for this isolated checkout. Existing running stacks belong to other tasks and do not run this PR. No performance or full-route visual certification claim is made.
- Before/after: missing sheets formerly returned one checked rule with no alerts; now that rule is skipped. Cross-park movement rows formerly shared keys; now each park has a distinct stable key. Degraded and filtered-empty tables formerly claimed all checks ran clean; now show contextual copy.
- Judge status: local self-review and focused regression checks; no independent judge requested.
- Current base SHA: bd444960769ca4e06508cfd9940a75ba4ce23f4b. Final commit to be recorded in the handoff.
- Deployment state: no merge, main push, or deployment performed.
