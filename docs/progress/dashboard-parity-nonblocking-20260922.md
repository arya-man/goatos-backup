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

## Pending

- Commit the scoped fix.
- Run local landing gate before any main promotion.
- If landed, refresh the OCI runner checkout and rerun automation to verify the
  Slack card reports browser evidence instead of stopping on parity.

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
- Latest local commit before guard update: `c356ff8cc`

## Deployment State

- Not deployed.
- Not pushed to main.
- PR opened: https://github.com/vgoats/goatos/pull/350
