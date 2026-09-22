# Dashboard Automation Parity Nonblocking - 2026-09-22

## Scope

Fix dashboard automation so production/read-only browser smoke does not wait for
STG-to-OCI parity. STG is live and continuously moving, so exact OCI parity must
be a data-trust signal, not a prerequisite that prevents Playwright/browser
evidence.

## Done

- Set `tools/dashboard-automation/config.json` business data parity to
  explicit-only by default.
- Updated `tools/dashboard-automation/run.mjs` so `production-smoke` skips the
  latest parity receipt and live business parity gates before browser smoke.
- Updated `post-main-certification` to still collect read-only browser evidence
  when parity is degraded.
- Updated Slack next-action copy so `Browser: not_run` no longer points at
  STG-to-OCI parity repair.
- Updated `tools/agent-hooks/check-dashboard-automation-guard.mjs` so local CI
  enforces the new nonblocking parity policy instead of the old strict policy.
- Verified the PR code on OCI: STG-to-OCI parity no longer blocks
  `production-smoke`; browser smoke ran and surfaced real follow-up failures.
- Scoped `production-smoke` to the live dashboard browser sweep by default;
  API latency, Lighthouse, Grafana, and vaccination lifecycle checks remain
  available behind `GOATOS_DASHBOARD_CERTIFICATION_EXTRAS=1` for OCI smoke and
  stay default-on for post-main certification.
- Fixed the Weighing FCR KPI unit contrast issue exposed by the OCI browser
  accessibility pass.

## Pending

- Rerun focused checks and full local CI after the production-smoke scoping fix.
- Push the updated PR branch.
- Refresh the OCI PR worktree and rerun the real production-smoke receipt.

## Tests

- `node --check tools/dashboard-automation/run.mjs`
- `node --check tools/dashboard-automation/notify-slack.mjs`
- `node tools/dashboard-automation/run.mjs --self-test`
- `node tools/agent-hooks/check-dashboard-automation-guard.mjs --self-test`
- `node tools/agent-hooks/check-dashboard-automation-guard.mjs`

Result: focused checks green.

## Known Failures

- Full `bash tools/ci/run-local-ci.sh` first failed only
  `dashboard-automation-guard` because the guard still encoded the old
  strict-parity rule. The guard has been updated and rerun focused green.

## Current SHA

- Base: `origin/main` at `7772c2e92`
- PR branch: `codex/dashboard-parity-nonblocking-pr`
- Latest local commit: `c3c50c568`

## Deployment State

- Not deployed.
- Not pushed to main.
- PR opened: https://github.com/vgoats/goatos/pull/350
