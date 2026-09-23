# Preserve API burst capacity — 2026-09-23
Scope: persist user-approved API max 4, min 1, concurrency 10 in Terraform, deployment restore, release verification and guards. Other services unchanged.
Base SHA: 1e40f3870 (origin/main at worktree creation).
Done: live service and serving revision already max 4; live revision goatos-api-stg-00503-qb7 uses unchanged build a67be34c0781. Source configuration updated in isolated worktree.
Before/after: max 2 -> 4; min 1 and concurrency 10 unchanged. Live checks after config change: livez 204/0.096s, readyz 204/0.283s, version 200/0.188s. No claim of measured throughput improvement.
Tests: node --test tools/deploy/stg-admin-web-traffic-order.test.mjs PASS 11/11; bash -n on both deploy scripts PASS; git diff --check PASS. Repo-local CI pending.
Judge status: not requested; none run.
Known failures: none in this patch yet. Authenticated browser E2E not run; scaling-only change.
Pending: validate source change and persist through repository workflow.
Deployment state: live scaling update complete; no new application build/deployment required by this source patch.
