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

Local CI identified required runner receipt refresh: packaged deploy script changed. Rebuilding runner image and verifying packaged bytes before updating immutable pin; no guard bypass. Backend CI still running.

Local CI completed: backend PASS; common failed on runner source receipt and an existing unclassified documentation commit d3b523225 on main. Classified that docs-only change accurately; dashboard-automation-guard now PASS. Runner rebuild b9a99776-2f13-4561-a696-9639ecae014b is in progress. Added release max=4 regression assertion.
Live incident follow-up: old SQL fixes are intact in deployed code; authenticated ADG reload rendered real data at 07:18Z. Server request 2.794s vs 12.969s earlier. No performance-code fix claimed. Full diagnostic record is /Users/raviteja/mesha/incident-api-scaling-20260923/diagnosis.md.

Runner build SUCCESS: b9a99776-2f13-4561-a696-9639ecae014b; packaged-source verification executed inside the built image and passed. Immutable image asia-south1-docker.pkg.dev/goatos-stg/goatos/clouddeploy-stg-runner@sha256:830814dcbc6e7eee6c07c250e4cfab1d4595a69cd207c4d446bb6f6bf6293c29. Terraform/deploy/release guard all expect max 4. Deployment tests rerun PASS 11/11. Final repo-local CI/landing pending.
