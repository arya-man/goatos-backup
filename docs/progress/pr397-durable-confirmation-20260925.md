# PR 397 durable stock confirmation

Scope: preserve the PR meaning: farm-authored products, feed stock deducted only on close, feeding burn rates unchanged, shortage confirmation remains an explicit human choice. Recover the same refused sale after its form closes; reserve the Mixed rollup name as minor validation hardening. PR branch push only.
Base SHA: 64c0255af1cd2c333340378fee82ddab2803aa7d.
Done: traced prior commits and accepted tradeoffs; durable sync-sheet confirmation, atomic same-row correction, shared online-form confirmation, reserved-name validation and regression tests authored.
Pending: commit, push and PR SHA readback. Final diff reviewed and focused tests completed.
Before/after: previously late shortage rejection had only unchanged-payload Retry All after form disposal; now the stored question can be explicitly acknowledged against the exact original row and payload. Same idempotency key and sale fields retained. No stock, rate, valuation, or workflow business rule changed.
Accepted boundaries preserved: offline pending sales remain absent from the ledger list; existing dates and buyer register scope unchanged. Mixed is a narrow validation gap, not a reason to reject the feature design.
Tests: prior review Go suites and 28 web tests passed; current patch tests pending. No device/browser/HTTP latency proof yet.
Judge: self-review; no agent requested. Known failures and their resolutions are recorded below.
Deployment: not started. No main merge or staging promotion requested.

Validation update: sales Go suites pass. Initial mobile guard caught missing hi/kn/te resources for the new button/labels; translations added. Android focused run still executing; Gradle emitted its existing journal-cache warning.

Validation update: replaced the core-data HTTP exception test stub with the real test-only Retrofit/OkHttp dependencies so the regression exercises actual 422 error-envelope parsing. Four existing HTTP-status tests were adapted and included in the focused run. The first Android test compile failed before this correction. Core-data tests now pass; database/app tests continue. Mobile, Room, idempotency, bounded-memory, Compose-list, exception, row-action and git-identity guards pass.

Exact focused proof commands:
- Backend (backend/): `go test ./internal/sales/...` passes. PostgreSQL integration cases remain opt-in and were not executed.
- Android: JDK 21, `ANDROID_HOME=/Users/raviteja/Library/Android/sdk`, `./gradlew --max-workers=1 :core:core-data:testDebugUnitTest --tests sg.mesha.goatos.core.data.sync.SalesStockConfirmationTest --tests sg.mesha.goatos.core.data.sync.SyncEngineTelemetryTest --tests sg.mesha.goatos.core.data.sync.SyncEngineWorkflowAlreadyRecordedTest --tests sg.mesha.goatos.core.data.PcCareRepositoryDuplicateScanTest --tests sg.mesha.goatos.core.data.ExecutionRepositoryPaginationTest :core:core-database:testDebugUnitTest --tests sg.mesha.goatos.core.database.outbox.OutboxExhaustedAndObserveByIdTest :app:testProdDebugUnitTest --tests sg.mesha.goatos.viewmodel.SyncStatusViewModelTest --tests sg.mesha.goatos.viewmodel.FeedSaleLinePresentationTest --tests sg.mesha.goatos.viewmodel.QueuedWriteFollowTest`.
- Guards: `make idempotency-writes-guard android-compose-lists-guard android-bounded-memory-guard mobile-guard room-migration-guard` and `make exception-guard android-row-action-scope-guard git-identity-guard` pass.
- `git diff --check` passes.
- Repeatable sync fixture: 20 kg sale against a 10 kg stock refusal, offline enqueue then repository recreation. Original attempt plus unchanged retry both refuse (zero accepted writes); explicit acknowledgement succeeds once; repeated confirmation is rejected. Create retains its key and all entered fields, close retains its original deal and outbox identity. Room verifies stale payload and double-confirm protection. These are deterministic unit/database tests, not live stock measurements or phone E2E.

Final local result: Android BUILD SUCCESSFUL, 60 focused tests, zero failures/errors/skips. Core-data: 43; Room: 3; app: 14. Final app source compiles. Local checks above pass; no phone/browser E2E, HTTP latency test, full CI/landing receipt, merge, or deployment claimed. Candidate is the commit containing this document, based on the SHA above; remote push verification will be reported after push.
