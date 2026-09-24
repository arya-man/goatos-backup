# PR 397 review fixes

Scope: load stock depletion from sales; completed-create replay before mutable catalog validation; Mother gender valuation override; Android animal-tagging eligibility. PR branch push only.
Base SHA: 8de67cd35c3de4d2605454b780c1d0101c24927f. Current candidate: working tree on this base.

## Done
- Load FIFO includes sales as a separate movement stream; consumed kg/days and burn rate remain feeding-only. Arrival-day overrun behavior is preserved.
- Stock cache revision includes sale depletions, including reopen deletion.
- Completed sale replays return the original result before catalog lookup, without repeating stock checks or writes.
- Mother valuation uses the existing normalized stage to select female pricing.
- Android tag eligibility uses stamped line kinds, with Sheep/Goat fallback for old cached rows.

## Exact checks performed
- `cd backend && go test -p 1 ./internal/sales/... ./internal/feeddirection/adapters/postgres ./internal/adminui/app` passed. Default DB tests are opt-in/skipped in this command.
- Explicit DB tests used `GOATOS_RUN_POSTGRES_TESTS=1` and `GOATOS_PGTEST_ADMIN_DSN` with disposable OCI test databases. Maintained OCI clone and STG were not mutated.
- `go test -v ./internal/feeddirection/adapters/postgres -run 'TestAFeedSaleReduces|TestStockLoads' -count=1` passed (152.501s): basic sale/cache/reopen and all five existing StockLoads test functions including FIFO, delivery handoff, milk, card parity, scope/pagination and sheet correction.
- Expanded `go test -v ./internal/feeddirection/adapters/postgres -run '^TestAFeedSaleReducesTheBalanceWithoutTouchingTheBurnRate$' -count=1` passed (113.173s).
- `go test -v ./internal/sales/adapters/postgres -run 'TestMotherValuationUsesFemaleRateDespiteRecordedMaleSex' -count=1` passed (128.364s).
- `node --experimental-strip-types --test features/procurement/sale-lines.test.mjs`: 4/4 passed.
- `make postgres-bind-contract-guard aggregate-projection-guard operational-read-model-contract-guard`: passed, including self-tests.
- `git diff --check`: passed.
- Android: `JAVA_HOME=/opt/homebrew/opt/openjdk@21 ANDROID_HOME=/Users/raviteja/Library/Android/sdk ./gradlew :app:testProdDebugUnitTest --tests sg.mesha.goatos.viewmodel.FeedSaleLinePresentationTest --max-workers=1`: BUILD SUCCESSFUL in 6m2s; XML confirms 6 tests, zero failures/errors/skips. This is prodDebug coverage, not the stgRelease full-CI lane.

## Before/after evidence
- Original load SQL ignored sales; fixture now proves 5,000 kg -> 3,000 kg after a 2,000 kg sale, consumption stays zero, reopening restores 5,000 kg.
- Expanded fixture: sale of 5,500 kg before next 1,000 kg delivery, then 100 kg feeding: old load -500 kg/zero eaten; new load 900 kg/100 eaten; both card and latest-load runway report 400 kg and 4 days at 100 kg/day.
- Mother with recorded male sex uses 24,000 female valuation; Buck uses 30,000 male valuation in the executed full query.
- Completed replay tests cover unavailable catalog and changed payload returning original deal with zero new writes/catalog/stock reads.
- No latency benchmark or speed improvement claim. DB durations above include migration/template setup.

## Failures and resolution
- First Go run caught duplicated stage normalization; fixed by reusing `s.stage_norm` and rerun passed.
- An expanded test run hit laptop disk exhaustion during linking; space recovered independently. Cleanup attempt deleted zero files. Reruns passed.

## Reviews / pending / deployment
- Round 1: independent stock and sales backend reviewers found no actionable bugs; requested stronger fixtures were added and executed.
- Broader frontend/mobile review found Tag animals exposed for feed/other sales; fixed with predicate regression coverage.
- Round 2: independent stock and sales backend reviews found no actionable bugs.
- Final Android diff independently reviewed: no actionable bugs; implementing reviewer also rechecked frontend/mobile adjacent contracts with no further findings.
- Pending at commit preparation: PR branch push/readback. Functional fixes and review cycles are complete.
- Full `make ci-local`, browser/device E2E and deployed smoke not run for this follow-up.
- Deployment state: not started. No main merge or staging deployment requested or performed.

- Full guardrail run initially found gofmt differences in two existing PR-touched health/vaccination files; formatting only was applied and the run restarted. Android emitted a pre-existing Gradle journal warning but completed successfully.

- Data-map hash guard flagged analytics.go because cache revision changed. Verified `stockItemsSQL`, `feedSoldCTESQL`, and `feedPurchaseStockKgSQL` are byte-for-byte unchanged from PR base; the already-derived saved stock calculation requires no SQL or example-number change. Refreshed only the analytics source hash via `gen-data-map.mjs --rehash-derived`; `make mesha-data-map-guard` passed. No new live-data verification claimed.

## Final guardrail boundary
- All `guardrails` targets through `telemetry-guard-ratchet-v2` passed across the original run and targeted continuation after fixing formatting, source hash and projection annotation. Repeated unchanged prefix targets were not needed for the continuation.
- Final `local-gcp-kernel-parity-guard` could not run: `check-local-gcp-kernel-parity.sh: line 58: docker: command not found`. Guard script, self-test and compose config have zero diff against origin/main. Docker was not installed, consistent with the laptop rule. This is an environment prerequisite, not a sales regression; do not describe full guardrails or full CI as green.
- Final diff check passed. No remaining actionable bugs in the repeated independent reviews. No full-CI/main promotion receipt claimed.
