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

## Second review follow-up (2026-09-16)

Scope: fix bounded event reads, RFID identification, and coverage of today's live-stock check. Base reviewed SHA: `2868d94265f9e288e95d788edc8d068df4d92d5a`. Destination: PR 282 only; no main or staging promotion.

Done:
- Each event SQL reader selects at most 25 source records before tag/pen enrichment, with a same-snapshot total for the overflow summary. Date predicates use IST half-open timestamp ranges. Shifting impact aggregation is restricted to selected movements.
- Multiple enabled rules of the same kind reuse one request-local read, including its failure. No cross-request cache or invalidation change.
- Animal and mother tags select only active animal_identifier_1/animal_identifier_2, with primary identifier precedence. Internal display IDs and BLE identifiers never substitute for RFID.
- Both parks' default current-day latency cases require feed_low_stock in rules_run. Empty successful stock checks pass; skipped, disabled, missing and degraded stock checks fail. Assertion regression tests run in the existing CI latency-policy target.

Exact validation:
- `go test -race ./internal/alerts/... ./internal/adminui/app ./internal/permissions` passed.
- `GOATOS_ALERTS_TEST_DSN=<local OCI connection> go test ./internal/alerts/adapters/postgres -run TestEventReadersPostgres -v` passed. Session-local temporary tables only; no application table mutations. Covered all seven kinds, 30 matches -> 25 returned plus total 30, RFID precedence/fallback, BLE exclusion, IST midnight and park isolation. A separate 50,000-record temporary fixture returned 25 records and total 50,000 in 88 ms (single synthetic read, not a live-page latency certification).
- `node --experimental-strip-types --test apps/admin-web/features/alerts/*.test.mjs apps/admin-web/lib/admin-ui-contract.test.mjs apps/admin-web/scripts/smoke-visual-route-coverage.test.mjs`: 27 passed.
- `make api-latency-policy-test`: 76 passed.
- Initial executable-gate test caught import placement before the shebang; corrected and the full policy suite rerun green. Initial frontend invocation omitted the type-stripping flag; corrected, then all 27 checks passed.
- Before/after deterministic evidence: two same-kind rules previously made two reads, now one; SQL previously returned all 30 fixture events, now returns 25 with total 30; bounded 25-row domain input with total 50,000 renders 49,975 more.

Pending / limits:
- Full live API latency, populated production query plans, and laptop/mobile E2E are not certified by this follow-up. The isolated checkout has no configured API base URL/actor; invoking the live gate reports GOATOS_API_BASE_URL required. No wall-clock speedup or release readiness is claimed.
- Judge status: self-review and regression tests; no independent judge requested.
- Full `make guardrails` reached the unchanged local-gcp-kernel-parity self-test and failed because `docker` is not installed (`check-local-gcp-kernel-parity.sh:58`). No Docker installation was attempted; OCI is the local database path. This is not a green full-CI receipt.
- `make ai-doctor` passed after building this checkout's local indexes. `git diff --cached --check` passed. Final commit and remote SHA are reported in the PR handoff.
- Deployment state: no merge, main push, or deployment.
