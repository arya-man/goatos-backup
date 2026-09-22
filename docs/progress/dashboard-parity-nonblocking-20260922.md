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

## Pending

- Commit the scoped fix.
- Run local landing gate before any main promotion.
- If landed, refresh the OCI runner checkout and rerun automation to verify the
  Slack card reports browser evidence instead of stopping on parity.

## Tests

- `node --check tools/dashboard-automation/run.mjs`
- `node --check tools/dashboard-automation/notify-slack.mjs`
- `node tools/dashboard-automation/run.mjs --self-test`

Result: green.

## Known Failures

- No current known focused-test failure for this scoped change.

## Current SHA

- Base: `origin/main` at `7772c2e92`
- Working tree: uncommitted scoped automation fix.

## Deployment State

- Not deployed.
- Not pushed to main yet.
