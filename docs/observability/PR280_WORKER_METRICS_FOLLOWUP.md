# PR 280 worker telemetry and migration follow-up

Scope: resolve migration version collisions with main and make kernel failure/retry panels read the consolidated worker's existing OTel counters. Authorized delivery: PR 280 only. No main landing or staging/service deployment.

## Done

- Rebased the PR onto main at 397114d1d06baddb50dffc7d2c2f9df1d0497b7b, preserving both coverage-matrix additions.
- Preserved landed calendar migrations 318/319; renumbered the four unlanded analytics migrations to 320–323.
- Replaced five legacy-job log-metric queries with the existing outbox/notification counters emitted by the shared worker. Missing event-only observations remain conditional, never synthetic zeroes.
- Added a regression covering all five actual targets plus rejection of false emitter annotations. It failed before the query change and passes afterward.

## Validation

- Migration duplicate-version guard and its adversarial self-tests: PASS after renumbering; review reproduced both collisions before the fix.
- Dashboard and deployment ordering tests: 24 passed, zero skipped, including the real PostgreSQL date-boundary test.
- `GOATOS_RUN_POSTGRES_TESTS=1 go test ./cmd/analytics-rollup ./migrations/postgres ./internal/platform/kmetrics -count=1 -timeout=8m`: PASS with a disposable PostgreSQL 16 admin DSN; analytics 16.861s, migrations 25.241s, kmetrics 0.320s. These are suite durations, not product performance measurements.
- `go test ./internal/kernelstages -run 'TestAnalyticsRollup|TestConsolidatedSweeperRunEmitsActualVersionDuration' -count=1`: PASS with PostgreSQL.
- `make grafana-durability-guard`: PASS with both `GOATOS_DASHBOARD_TEST_DSN` and `GOATOS_BOOTSTRAP_TEST_DSN` supplied; database cases executed without skips.
- Artifact-only Cloud Build `34c0d6bf-37d7-4993-a530-ecd6c6515303`: SUCCESS. Ran the packaged entrypoint smoke and compared every installed file with the current source. Both runner pins and the verified receipt use `sha256:de417bd83c42d1cb3fdb479cf9ab8a23aac6ef45ec0528cdd345f23d30c42b8b`.
- `make validate-migrations` stops at existing hot-index safety diagnostics. After placing the analytics migration's NO TRANSACTION marker in both sections for the section-based validator, head and main produced byte-identical safety diagnostics and no analytics violations. Actual complete migration application was proved by the PostgreSQL suites above.
- Initial artifact submission rejected a duplicate receipt output path in the temporary build config; corrected to the repository build config and reran successfully.
- Migration inventory cross-check: analytics adds exactly 320–323. Pre-existing numbering gaps are identical to main; no new gaps or duplicates.
- `make ai-doctor`: PASS after rebuilding the isolated checkout CRG and Repowise indexes.

## Limits

Three broader reminder tests failed in the review and reproduced identically on the PR merge base (3 recipients versus expected 5). These are not attributed to this follow-up. No product latency claim; no business-route/browser/device or live deployed Grafana/Firebase certification. Independent judge: not requested. Full main landing receipt: not run.

Validation applies to the implementation in this commit after the rebase; the final Git SHA is recorded in the PR description and local delivery receipt. Deployment state: runner artifact built only, no services changed.
