# PR 280 review follow-up

Scope: correct successful proof-delivery recovery/fallback cohorts and replace the stale pinned deployment runner.

Current tested implementation SHA: e292eab2171c79fccb5afbd3770f6ed4616ad1a3.
PR base: 902145d9871afc325ef8d5f559f5bf2ff9b05497.
Delivery target: PR 280 branch fix/grafana-complete-data-20260916 only.
Deployment state: artifact build only; no main landing or service deployment.
Judge status: primary reviewer reproduced and fixed both findings; no independent judge requested.

## Done

- Delivery requires capture, upload-start and upload-complete. Processing telemetry no longer excludes successfully uploaded recovery/original files.
- Rebuilt runner in Cloud Build 43dc4728-e78a-4740-a9d2-c9fb59ca9071; compared every packaged source file with the installed image. Receipt contains 16 packaged files plus the Dockerfile hash.
- Both pins use sha256:f9a9cc58dbb3eb07acf8e4f6b8fa3c6bf38d179176509e2bc4af047d5ff4bee4.
- Receipt verification is enforced by the existing Grafana CI guard and before Cloud Deploy release mutations. Adversarial tests reject changed/missing/extra sources, stale pins and mismatched image files.

## Exact validation

- Failing-before: GOATOS_RUN_POSTGRES_TESTS=1 with the approved OCI admin DSN, go test ./cmd/analytics-rollup -run TestProofDeliveryRecoveryAndOriginalFallbackPostgres -count=1 -v -timeout=5m. Original SQL returned 0 completions/4 drop-offs/p50 0 instead of 2/2/2000.
- After fix: same isolated PostgreSQL harness, go test ./cmd/analytics-rollup -count=1 -v -timeout=8m. All 39 top-level tests passed, zero skips, 104.832 seconds. Includes replay, pending/out-of-order uploads, crash cohorts and provider failures.
- Regression result: 2 completions, 2 genuine drop-offs, p50 2000 ms. Synthetic 46,000-event/11,500-proof fixture completed in 2300 ms; not a product API latency claim.
- make grafana-durability-guard passed, including runner receipt and adversarial tests.
- node --test tools/deploy/grafana-dashboard-queries.test.mjs with read-only OCI SQL: all 14 passed, no skips.
- make ai-doctor passed after building the isolated checkout's CRG/Repowise indexes.
- Shell syntax and git diff whitespace checks passed.

## Known failures and remaining release proof

Initial runner-test setup error was corrected and rerun green. Initial AI Doctor missing-index failures resolved. No known failing focused check remains.
Full landing CI and live Grafana/Firebase/browser/device E2E were not run for this follow-up. They remain release certification work; this request authorizes a PR push, not landing or staging rollout.
