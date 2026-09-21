# Dashboard automation degraded production smoke fix — 2026-09-22

## Scope

- Fix the dashboard automation runner so production read-only browser smoke is not hidden behind STG-to-OCI parity failures.
- Keep post-main certification strict: no preview Playwright certification without fresh `READBACK_PASS` STG-to-OCI parity.
- Keep self-healing PR creation limited to code-patchable product failures, not parity/auth/env/precondition failures.
- Make Slack explicitly say whether Playwright/browser smoke ran, ran degraded, or did not run.

## Done

- `tools/dashboard-automation/run.mjs`
  - Added runtime policy split:
    - `production-smoke`: may run read-only Playwright in `run_degraded` mode when only parity prerequisites are red.
    - `post-main-certification`: remains strict and blocks preview Playwright without fresh parity.
  - Added `degraded` receipt state when production browser smoke passes but parity prerequisites fail.
  - Skips agent-review and self-heal PR budget for degraded parity-only production smoke.
  - Self-heal now runs only when final receipt status is `fail`, not for `degraded`.
- `tools/dashboard-automation/notify-slack.mjs`
  - Added first-class `degraded` Slack notification.
  - Adds `Browser smoke` and `Parity gate` fields.
  - Explicitly says when browser smoke did not run versus ran degraded.
- `tools/agent-hooks/check-dashboard-automation-guard.mjs`
  - Added static guard coverage so future edits cannot remove degraded production smoke behavior or Slack clarity.
- `tools/dashboard-automation/config.json`
  - Added `degraded` to Slack post policy.

## Tests / evidence

- `node --check tools/dashboard-automation/run.mjs`
- `node --check tools/dashboard-automation/notify-slack.mjs`
- `node --check tools/agent-hooks/check-dashboard-automation-guard.mjs`
- `node tools/dashboard-automation/run.mjs --self-test`
- `node tools/dashboard-automation/notify-slack.mjs --self-test`
- `node tools/dashboard-automation/self-heal-pr.mjs --self-test`
- `node tools/agent-hooks/check-dashboard-automation-guard.mjs`
- Synthetic degraded receipt:
  - self-heal skipped PR creation for parity-only degraded failure.
  - Slack dry-run rendered `Browser smoke = ran_degraded` and `Parity gate = fail`.
- `make dashboard-automation-self-test dashboard-automation-guard`
- `git diff --check`

## Judge status

- Judge task: `01a0c599-713f-7633-a751-664b24d170cc`
- Result: sign-off, no blockers.
- Judge verified:
  - Production smoke can run degraded when only parity prerequisites fail.
  - Post-main certification remains strict.
  - Slack distinguishes `ran_degraded`, `ran_certified`, and `not_run`.
  - Self-heal PR creation remains constrained to code-patchable UI/API/latency failures.
  - August 1-to-date bug-pattern coverage remains guarded.

## Pending

- Run full `make land-main` after this final doc update and push only if the landing gate passes.

## Current SHA before landing

- Base `origin/main`: `d82c9de104fab5ef5e798f36a20cf642d410a2cb`
