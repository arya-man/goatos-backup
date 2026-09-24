# PR 397: successor sales against legacy stock

Scope: subtract sold feed at stock-family grain in cards, alerts, farm stock and saved data-map query; keep load depletion consistent without changing per-item feeding boundaries.
Base/current SHA before edits: b0e2688b97c77b9ce25f0369d14ad79e23a1f929.

Done: review reproduced 5,000 kg legacy stock minus 1,000 kg successor sale incorrectly returning 5,000 kg (expected 4,000 kg).
Pending: database regression, implementation, focused tests and guards, branch push/readback.
Tests: prior review focused Go and six Node checks passed; new regression not yet run.
Known limitations: no full CI, live browser/device E2E or deployment proof. No speed improvement claim.
Judge status: no separate judge requested or run.
Deployment: not started; PR branch push only is authorized.

## Implementation and first proof
- Family-grain sales now subtract once after each item's feeding balance is computed. Cards, alerts, farm stock and the saved data-map query share that rule.
- Load FIFO retains per-item feeding. Sales prefer their own reached item ledger, otherwise one deterministic family sibling. Later deliveries do not move earlier sales to future loads.
- Initial test fixture had an ambiguous numeric bind; fixed with explicit numeric casts. It also used external consumption on a farm view that reads locked sheets; replaced fixture with real persisted/locked sheets so the test exercises the supported common path.
- Regression against original HEAD via Go overlay: FAIL, 4,900 kg instead of 3,900 kg after 100 kg feeding and 1,000 kg successor sales. Same missing subtraction in card, confirmation, farm, alert and loads.
- First fixed regression and existing sale/FIFO/scope/pagination suite: PASS (8.386s, real PostgreSQL, no skipped DB tests).
- Bind, aggregate projection, operational read-model, data-map and latency policy guards: PASS.
- Extended test now also checks arrival stability, a subsequent exact-item sale, and reports uncached SQL timings; final run pending.

## Final validation
- `GOATOS_RUN_POSTGRES_TESTS=1 GOATOS_PGTEST_ADMIN_DSN=<disposable local PostgreSQL> go test ./internal/feeddirection/adapters/postgres -run 'TestSuccessorFeedSale|TestAFeedSale|TestAFeedNobody|TestStock' -count=1 -v`: 17 top-level tests passed, three existing tests failed (listed below). All database tests executed; no Docker or shared/STG database used.
- New regression passes for legacy-only stock, multiple legacy members, later successor delivery, subsequent exact-item sale, reopening/cache invalidation, pre-arrival feeding boundaries and excluded park scope. After 100 kg feeding plus 1,000 kg successor sales, the fixed balance is 3,900 kg instead of 4,900 kg. Burn rate remains 100 kg/day and days-left becomes 39 instead of 49.
- `go test ./internal/sales/... ./internal/feeddirection/...`: PASS (default opt-in database tests skipped).
- `make postgres-bind-contract-guard aggregate-projection-guard operational-read-model-contract-guard mesha-data-map-guard api-latency-policy-test`: PASS after final edits; latency policy self-tests 76/76.
- `git diff --check`: PASS.
- Uncached SQL fixture timings (20 samples; 3 purchase rows, 3 sale lines, 2 locked feed days): stock items p90/p95/p99 0.216/0.218/0.238 ms; stock loads 0.584/2.633/3.117 ms. These are small-fixture SQL checks, not production HTTP performance proof or a speedup claim.
- `node tools/perf/api-latency-gate.mjs --manifest tools/perf/hot-paths.feed-focused.json`: could not execute HTTP probes; `GOATOS_API_BASE_URL or --base-url is required`. Updated manifest documents the new regression and this evidence boundary without weakening thresholds.
- Full CI and browser/device E2E not run. No main landing receipt claimed.

## Existing failures independently reproduced before this fix
Used Go overlay to substitute `analytics.go` and `stock_loads.go` from original PR SHA b0e2688b9, keeping the identical database harness and test fixtures. Each failure reproduces unchanged:
1. `TestStockItemsIncludeExternalConsumptionFeeds/StatusBucketsLockedDirectedAndExternalSumOnce`: test expects the legacy concentrate card but runtime returns its merged family card.
2. `TestStockUhtDepletesFromMilkPreparationOnSubmit/OneToManyProofAttemptsCountOnlyTheCurrentOne`: 476.0 kg returned vs expected 478.0 kg.
3. `TestStockCardsActiveVocabularyOneToManyPageBoundaryParkScopeStatusBucketsNotBurnRate`: expected 120 kg/day vs maintained CBE override 55 kg/day.
These were not changed or hidden. Broad DB suite is not fully green; the requested successor-sale regression and relevant FIFO/family cases are green.

## Delivery
Candidate is the commit containing this document, based on b0e2688b9. Scope is seven listed changed files including this progress note. PR branch push pending at commit preparation; remote head will be read back in the task response. No merge or deployment.
