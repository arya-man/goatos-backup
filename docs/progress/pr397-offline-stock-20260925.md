# PR 397 offline rename and stock validation fixes

Scope: preserve queued name-only sales across product renames; read one request-local stock snapshot for create/close confirmation. PR branch push only.
Base SHA: 8f1b4da846bdfa7ce3108a8ec0e172c035e87e02. Candidate: working tree.
Done: product-name history migration, active catalog alias resolution, historical-name collision refusal, batched stock reader, focused regressions.
Pending: Go tests, real disposable PostgreSQL migration/rename/stock tests, guards, commit and PR push/readback.
Before/after: same queued sale previously rejected after a rename; now resolves the same code via retained name. Twenty distinct feeds previously caused twenty uncached stock reads; now one snapshot. No HTTP latency improvement claimed yet.
Known failures: none from current changes yet. Earlier PR broad DB fixture failures and missing live browser/device/HTTP latency proof remain documented in prior progress notes.
Tests/E2E: review baseline focused Go suites, 28 web tests and TypeScript passed; responsive guard lacked API/auth/tenant configuration. Current patch validation pending.
Judge status: self-review ongoing, no independent agent requested.
Deployment state: none. No main merge or staging deployment authorized.

First validation caught a duplicate Aliases field introduced during editing; corrected before rerun. Initial Go command also used the repository root instead of backend; rerun from the module directory. Bind and aggregate-projection guards passed.

## Final validation
- PASS: `cd backend && go test ./internal/sales/... ./internal/feeddirection/adapters/postgres ./internal/procurement/... ./internal/adminui/app` (default DB tests opt in).
- PASS with `GOATOS_RUN_POSTGRES_TESTS=1` and a disposable local PostgreSQL 16 cluster: `TestQueuedSaleSurvivesRenamesWithoutReassigningNames`, `TestFeedCloseReplayUsesPersistedStatusAndDepletesOnlyOnce`, and `TestSuccessorFeedSaleUsesLegacyFamilyStock`. These ran without skips and applied migrations including 000406. No shared/STG data changed.
- Rename coverage: two renames before first delivery, stable code/current name on persisted sale, one depletion after replay, historical-name collisions refused, archived aliases refused.
- PASS: new domain alias test and 20-feed service regression (known zero warns; unknown remains unknown). Before: 20 stock reads; after: 1.
- Repeatable local SQL-shape comparison, 20 samples per shape on the same fixture (3 purchases, 3 sale lines, 2 feed days): old 20-read shape p90/p95/p99 7.831/7.857/8.030 ms; one-snapshot shape 0.852/0.886/1.638 ms. This is a small-fixture comparison, not production HTTP latency certification.
- PASS: postgres-bind-contract, aggregate-projection, operational-read-model-contract, mesha-data-map, and latency-policy checks (76 policy tests). Data-map live column check skipped without a DB env.
- No frontend/mobile changes or wire contract changes: existing name-only queued payloads remain supported through server-side aliases.
- No full local CI, authenticated real-route browser/device E2E, or HTTP latency gate run for this patch. Earlier PR broad fixture failures remain outside this patch and are not claimed fixed.
- Self-review completed; no independent judge run. Deployment remains not started. Pending: final identity/diff checks, commit and PR-head push/readback. Candidate is the commit containing this note.
